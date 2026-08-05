# Risks & Open Decisions

## Top technical risks (from Codex review, 2026-07-29)

1. **Multi-passenger insertion complexity.** Mitigated in MVP by capping
   occurrences at 2 concurrent passenger parties + re-verify detour inside the
   accept transaction. Raising the cap needs a real insertion algorithm and more
   OSRM budget. Decide only with usage data.
2. **Push reliability.** Expo push is best-effort, duplicable, and tokens die
   silently. Mitigations are specced (outbox retries, idempotency keys, receipts,
   token pruning, reconcile-on-open) — but this needs deliberate testing, incl.
   killed-app and dozing-Android scenarios.
3. ~~**OSRM real memory footprint.**~~ **MEASURED 2026-07-29 — resolved, and the
   answer changes the deployment.** Built both extracts on the mini PC with
   `scripts/build-osrm.sh` (OSRM 26.7.3, MLD, car profile) and measured
   `osrm-routed` RSS after warming with 200 routes:

   | Extract | Artifacts | Serving RSS | Preprocess peak |
   | --- | --- | --- | --- |
   | `greece-latest` (whole country) | 2.0 GB | **1.15 GB** | 5.7 GB |
   | Evros + ~30 km buffer (bbox clip) | 43 MB | **39 MB** | 291 MB |

   Both return an identical Alexandroupoli→Komotini route (57.8 km / 45.4 min),
   so clipping costs nothing for in-region trips.

   DEPLOYMENT.md's 200–500 MB estimate was wrong in *both* directions: whole
   Greece is ~2.4× worse than the top of the range, a clipped prefecture ~5×
   better than the bottom. The RSS is anonymous memory, not shared page cache,
   so it is real per-container resident cost.

   **Consequence:** the region-clipped extract is not an optimization, it is the
   thing that makes the 2 GB VPS target achievable. Preprocessing peaked at
   5.7 GB, confirming it must stay off the VPS as specced.

   **Whole-stack measurement (2026-07-29), containers under load:**

   | Container | Measured | DEPLOYMENT.md estimate |
   | --- | --- | --- |
   | `api` (distroless, 24.6 MB image) | **7.2 MiB** | ~100 MB |
   | `db` (postgis:16-3.4, shared_buffers 256MB) | **261 MiB** | ~400 MB |
   | `osrm` (Evros + 30 km clip, MLD) | **32 MiB** | 200–500 MB |
   | `caddy` (not yet run; upstream figure) | ~30 MB | ~30 MB |
   | **total** | **≈ 330 MiB** | ~1 GB |

   The Go API is an order of magnitude smaller than estimated — the estimate
   appears to have assumed a runtime with a heavier baseline. The stack fits a
   1 GB VPS with room to spare, provided the OSRM extract stays region-clipped.
   Postgres dominates and is the knob to turn if the box is smaller: it is
   `shared_buffers` that sets the floor, not the workload.
4. **Cold-start liquidity** (product risk, the biggest one). Matching tech is
   worthless without drivers. Pilot plan: seed one corridor/community (e.g. one
   town ↔ one city), invite codes, personally onboard the first 20 drivers.

## Legal / privacy (needs a real check before public launch)

- **Cost-sharing vs commercial transport (Greek law).** The app must stay clearly
  non-commercial: suggested contribution ≤ actual cost share, no profit, no
  per-ride commission. Check KOK/transport rules + how BlaBlaCar operates in
  Greece as precedent. Until verified, copy stays conservative ("συνεισφορά στα
  έξοδα").
- **GDPR.** Precise location = personal data. Specced mitigations: no passive
  tracking, ephemeral live positions, 12-month retention + anonymization,
  region-level (not point-level) analytics. Still needs: privacy policy, consent
  screens, data-export/delete endpoints (GDPR arts. 15/17).
- **SMS OTP provider** choice affects cost + GDPR processor list.

## Open product decisions (decide during MVP build)

- Final name + domain (working name: carpool).
- Women-only matching toggle: MVP or fast-follow.
- Open vs invite-code registration per region at pilot start.
- Chat: in-app minimal thread (specced) vs just revealing phone numbers after
  accept (simpler, but leaks numbers). Current spec: in-app thread, phone reveal
  optional per user setting.
- Whether targeted join requests bypass the sequential-ask flow (currently: yes,
  a targeted request creates an ask for that driver directly).

## Open decisions (raised during implementation, 2026-07-29)

Recorded per the build handoff: where the specs contradicted themselves or a spec
detail turned out to be unimplementable as written, the smallest reasonable
resolution was chosen and logged here. Source: Codex plan review (high effort),
2026-07-29.

1. **The buffered functional GIST index cannot serve the gross-match query.**
   DATA-MODEL.md specs `route_templates_buffer_gist` on
   `ST_Buffer(route_geom::geography, 3000)::geometry`, and MATCHING.md filters with
   `ST_DWithin(route_geom::geography, :origin::geography, notify_radius_km*1000)`.
   Postgres can only use an index on the expression the query actually references,
   so a `ST_DWithin` over *unbuffered* geography will never touch a *buffered*
   index — the specced index would be dead weight and the gross match would
   seq-scan.
   **Resolution:** index the expression the query uses —
   `CREATE INDEX ... USING GIST ((route_geom::geography))` — and drop the buffered
   index. This also supports the per-driver variable radius directly, which a
   single fixed 3 km buffer cannot. The EXPLAIN test asserts *this* index is used.

2. **Occurrence identity keyed on `depart_earliest` is not DST-safe.**
   DATA-MODEL.md specs `UNIQUE (template_id, depart_earliest)`. `depart_earliest`
   is a concrete instant derived from a local Athens wall time; a tzdata update or
   a DST policy change moves the instant and would silently create a duplicate
   occurrence for the same calendar day.
   **Resolution:** materialize a `service_date date` (Athens local calendar date)
   on `trip_occurrences` and key identity on `UNIQUE (template_id, service_date)`.
   `depart_earliest`/`depart_latest` remain as the concrete instants.

3. **Ambiguous local wall times need an explicit policy.** Go's `time.Date` does
   not guarantee which offset it picks for skipped (spring-forward) or repeated
   (fall-back) wall times, so recurrence generation would be silently
   implementation-defined twice a year.
   **Resolution:** resolve wall times by round-trip search rather than trusting
   `time.Date`. Policy: a **nonexistent** local time skips that date and records a
   materialization error; a **repeated** local time picks the earlier (first, DST-)
   instant. Both are covered by explicit tests.

4. **Detour re-verification cannot happen inside the accept transaction.** The
   accept path must re-verify the detour against the current itinerary, but that
   needs an OSRM round-trip — holding row locks across a slow, failure-prone
   sidecar call is a liveness hazard.
   **Resolution:** optimistic concurrency instead. Read `itinerary_version`, call
   OSRM outside any transaction, then open the transaction, take locks in the
   canonical order `occurrence → ride_request → asks(pk order)`, and confirm the
   version is unchanged; on mismatch roll back and recompute, bounded retries.
   The TOCTOU window stays closed without holding locks across I/O.

5. **The outbox `idempotency_key` does not make delivery exactly-once.** A unique
   key stops the same business transaction enqueuing twice; it cannot prevent
   "provider accepted → worker crashed before marking done → sent again".
   **Resolution:** keep at-least-once as the honest contract, add lease columns
   (`locked_by`, `locked_until`) so a crashed worker's rows are reclaimable, and
   rely on client-side dedupe by idempotency key (already specced in MOBILE.md).
   Duplicate push delivery is documented as possible, not designed away.

6. **Refresh rotation logged users out after a lost response.** Single-use
   rotation is right, but the client can only persist the new token *after* the
   response arrives. An app killed in that window relaunched holding a spent
   token, which is indistinguishable from a replay, so the family was revoked and
   a blameless user had to sign in again.
   **Resolution:** the client writes a random `attempt_id` to secure storage
   *before* sending and reuses it on retry. A spent token presented with the id
   that spent it is one request arriving twice and gets the same successor back;
   any other id is still a replay and still burns the family. The successor is
   derived as `HMAC(server secret, parent digest || attempt id)` so it can be
   recomputed for the retry without ever storing a live token. A blind time-based
   grace window was considered first and rejected on Codex review: it cannot tell
   a lost response from a thief racing the legitimate client, and it hands the
   session to whoever asks second.

7. **"Revoke the whole family" could leave a live token behind.** Revocation is
   an `UPDATE` over the rows that exist when it runs, so a rotation committing
   alongside it inserted a token the revocation never saw — precisely the token a
   thief would hold.
   **Resolution:** every mutation of a family takes `pg_advisory_xact_lock` on
   the family id first, before the token row, so a revocation cannot interleave
   with a rotation. A stress test reproduces the old behaviour within two rounds.
   A family/session table with `revoked_at` (as Codex suggested) would be the
   structurally cleaner fix and is the thing to do if sessions grow features.

   **What this costs, stated plainly** (Codex, second review): the attempt id is
   stored and transmitted alongside the refresh token, so it is a second bearer
   value, not proof of device identity. Anything that captures the pair — malware,
   a device backup, a logging proxy — can present it and be handed the successor
   instead of tripping revocation. Detection is deferred rather than removed: both
   parties then hold the same successor, and the next rotation by either one is a
   replay that burns the family. Only device-bound keys (DPoP, hardware-backed)
   close this properly, and that is parked for post-MVP.

   Two smaller consequences, accepted for a single-VPS pilot: rotating
   `JWT_SECRET` invalidates outstanding retries, because the successor is derived
   from it (worst case one extra sign-in, and that rotation already invalidates
   every access token); and a rolling deploy across mixed versions would briefly
   reopen both races, so if this ever runs on more than one replica the advisory
   lock must ship everywhere before attempt ids are enabled.

8. **Toll estimates are quoted per ask, and the roster moves underneath them.**
   The suggested contribution now includes each passenger's share of the motorway
   barriers they will be in the car for (`internal/tolls`). Four limits, all
   raised by Codex review of the implementation and all accepted for the pilot
   *because the app never moves money* — the figure is a suggestion the two sides
   settle in cash:
   - The share is computed against the driver's **current** route, not the route
     the new passenger's detour would create. A detour that adds or avoids a
     barrier is not reflected until the booking is made.
   - Quotes are not rebalanced. A first passenger quoted half a barrier is not
     re-quoted when a second joins and the fair share becomes a third. The trip
     screen recomputes for everyone aboard; the ask does not.
   - The occupancy count for fuel and the roster for tolls are read in separate
     queries, so a booking confirmed between them makes the two disagree.
   - A route that retraces itself (A→B→A) projects a passenger's stops onto the
     first traversal, so a return passenger cannot be priced. The gross-match
     filter already refuses such candidates, so this is unreachable today.
   Closing the first three properly means freezing an allocation at booking and
   settling it at completion — a ledger, which the pilot deliberately does not
   have. Revisit if contributions ever become anything other than cash.

9. **The toll dataset is a copy, and copies go stale.** Prices are the operators'
   own published cash tariffs, collected through vriskoapostasi.gr by
   `scripts/harvest-tolls.py` and checked in. Electronic-pass discounts and
   permanent-resident exemptions are not modelled, so the figure is an estimate
   and is labelled as one. The refresh script refuses to write silently when a
   station moves, changes direction, disappears, or changes price by more than a
   quarter — those want a human. Re-run it when tariffs change, then run
   `go test ./internal/tolls/`, which replays 22 real routes.

10. **`tolls.Segment` projects a point onto the route however far away it is.**
    A pickup fifty kilometres off the geometry lands silently at the nearest
    point on it, and the passenger is billed for whatever barriers that implies.
    Every caller today hands it points the matcher has already accepted — the
    requester passed gross and fine matching, and booked riders are on this
    occurrence — so there is nothing to reject. Adding a distance limit means
    inventing a threshold no measurement supports, which is how the toll matcher
    got its first wrong answer. Revisit if `Segment` is ever called with points
    the matcher has not vetted.

11. **One outbox row fans out to every device a user owns, under one lease.**
    A push message sends to each live token in turn. Every send is capped at 20
    seconds (`push.NewExpo`) and the lease is two minutes, so a user with seven
    or more live devices can be handed to a second dispatcher while the first is
    still sending. The consequences are bounded — the app deduplicates on the
    idempotency key — but the attempt budget is charged twice and the message
    can be dead-lettered while it is being delivered.

    An earlier attempt to bound this with a handler deadline was withdrawn: the
    lease is taken from the injected clock, a `context` deadline is real elapsed
    time, and comparing the two reintroduces exactly the clock-domain fault the
    project banned. Closing it properly means one outbox row per token. Revisit
    when a real user has more than a handful of devices — which cannot happen
    before the app registers push tokens at all.

12. **Three narrower delivery limits, all recorded rather than built:**
    - A dead token is retired by `WHERE token = $1`, so a token re-registered
      between the failed send and the update would be disabled on stale news.
      Unreachable today: the app does not register tokens yet.
    - A device-fatal failure that is *not* `DeviceNotRegistered` would leave the
      token live and dead-letter every message for that user. Also unreachable:
      the only per-device permanent verdict Expo's client returns is
      `DeviceNotRegistered`; `MessageTooBig`, `InvalidCredentials` and HTTP 400
      are faults of the message or the account, not the device.
    - Two different effects sharing one non-blank idempotency key would see the
      second silently dropped by `ON CONFLICT DO NOTHING`. Keys are built from
      the event and its ids, so a collision means a bug in the producer; the
      queue does not check that the existing row describes the same effect.

    Payloads are also unversioned. A rename or a semantic change to a push
    payload would strand rows queued by the previous deployment. The pilot has
    one API process and a queue that drains in seconds, so a deploy is
    effectively a drain; if that ever stops being true, version the payloads.

13. **The driver sets their own fuel consumption, and the price follows it.**
    `driver_profiles.fuel_consumption_l_100km` defaults to 7.0 and is whatever
    the driver types. The suggested contribution is
    `consumption/100 × fuel_price × km × wear`, so a driver who enters 14 asks
    for roughly double. Nothing checks it against the car they actually drive,
    and the passenger has no way to tell.

    The fuel price has the mirror problem from the other side:
    `regions.fuel_price_eur` is a single number seeded per region and updated by
    hand. It goes stale, and when it does the suggestion is wrong for everybody
    in that region at once.

    Neither is urgent while the pilot is twenty drivers who know each other, and
    both matter the moment it is not. The intended fix is in PROGRESS.md under
    "Pricing that nobody can tilt"; the trigger to build it is the first
    complaint that a price looked too high.

    Both data sources for that fix have now been found and checked, in
    `docs/research/sources.md`: EEA registration data carries a real consumption
    field but only from 2021 registrations onward, and the fuel bulletins are
    parseable PDFs rather than a feed. Neither is a blocker any more; the
    remaining question is the old Greek fleet the EEA table does not cover.

    Half done. The consumption side is built: internal/vehicles resolves a
    car through the EEA dataset, then the CO2 at field V.7 of its άδεια
    κυκλοφορίας, then a conservative class floor, and records which. Nothing
    consumes it yet — driver_profiles still holds the typed number. The fuel
    price side is untouched and gated on a licence question. docs/pricing.md
    explains all of it.

    Raised in priority by the insurance finding. The ceiling is not only a
    fairness feature. A driver whose contribution stays at cost is a driver whose
    car is still in private use, which is the whole of their answer if an insurer
    ever argues otherwise. A driver-typed consumption of 14 is that argument
    breaking.

14. **Half of the prefecture's main corridor cannot be published.**
    `route_templates_geom_simple` rejects a route whose geometry is not simple,
    and the reasoning is sound: `ST_LineLocatePoint` cannot place a point on a
    self-intersecting line, and the gross match depends on it.

    Measured against real OSRM output on 3 August 2026:

    | Route | `ST_IsSimple` |
    | --- | --- |
    | Αλεξανδρούπολη → Ορεστιάδα | true |
    | Ορεστιάδα → Αλεξανδρούπολη | **false** |
    | Διδυμότειχο → Αλεξανδρούπολη | **false** |
    | Φέρες → Αλεξανδρούπολη | true |

    The same corridor passes northbound and fails southbound. That is not an
    edge case: it is the road the pilot exists to serve, and a driver who
    publishes their morning commute cannot publish the evening return.

    Not fixed, because the fix is a choice rather than a patch.
    `ST_RemoveRepeatedPoints` or a small `ST_Simplify` before storing would
    likely make these simple, at the cost of the stored geometry no longer being
    exactly what OSRM returned. The alternative is to stop depending on
    `ST_LineLocatePoint`, which is a matcher change.

    Discovered because `scripts/seed-demo.sh` could not publish the second
    route, and the handler was throwing the reason away. It now logs it.

15. **The place list stopped at the prefecture line — fixed 2026-08-04.**
    `internal/places` held 153 settlements harvested inside the Evros boundary,
    so Κομοτηνή could not be typed even though Αλεξανδρούπολη → Κομοτηνή is the
    demo seed's own example route.

    It is now every city, town, village and hamlet in Greece: 12,442 of them,
    harvested from the same OSM extract the router runs on. The router itself
    was switched from a buffered Evros extract to the whole country at the same
    time, because a place list that can name Θεσσαλονίκη in front of a router
    that cannot reach it is worse than either problem alone.

    What the jump cost, recorded because none of it was obvious beforehand:

    - Hamlets had to be included and ranked last. Leaving out 4,832 of them
      means nobody living in one can say where they are; ranking them level
      with cities makes the search complete and useless in the same breath.
    - The tie-break had to become population. It had been name length, which
      put Αλεξάνδρεια ahead of Αλεξανδρούπολη for "alexandr" — not a fact about
      either place.
    - A two-character floor had to be introduced. One character is a sample of
      the dataset rather than a search. Three would have been wrong: the
      shortest Greek place names are exactly three letters, so Κως would have
      had to be typed in full before appearing.
    - Each settlement carries its municipality, because Greece has dozens of
      Άγιος Γεώργιος.

16. **Automatic meeting points are good in villages and poor in cities.**
    2,922 landmarks are harvested from OSM — the ΚΤΕΛ, the station, the port,
    the petrol station — covering every city, nine towns in ten and a quarter
    of villages. Where a settlement has nothing tagged it gets nothing, which
    is deliberate: a synthetic "centre of village" is a coordinate wearing a
    confident name, and being findable is the only promise a meeting point
    makes.

    The failure is at the other end. Θεσσαλονίκη's three points come out as
    "Arda Tur Bulgaria", "Τ.Σ. Ν.Σ. ΣΤΑΘΜΟΣ" and "Ν.Σ. ΣΤΑΘΜΟΣ" — coach-ticket
    agencies tagged `amenity=bus_station`. Αλεξανδρούπολη gets urban bus-line
    termini rather than the ΚΤΕΛ. OSM is dense in cities and the density is
    what hurts: more things carry the tag, and few of them are the landmark
    anybody means.

    No automated ranking fixes this, because the information that "Arda Tur
    Bulgaria" is a ticket office and the ΚΤΕΛ is the bus station is not in the
    tags. It needs a person.

    Addressed 2026-08-05 rather than solved: a moderator can hide, rename or
    add a point, and anybody can suggest one for review. What is still missing
    is the person doing it. Nothing is curated yet, so Θεσσαλονίκη still shows
    the ticket agencies until somebody sits down with the list.

    Two things about the shape of it are worth keeping in mind. Reading the
    corrections can fail, and when it does the answer is an error rather than
    the raw harvested list, because falling back would put a point somebody
    removed for a reason back in front of people. And a suggestion is not a
    meeting point: it is invisible to everyone but its author until reviewed,
    and cannot be attached to a ride in the meantime.

17. **A suggested point carries no coordinate anybody chose.**
    The proposal form sends the settlement's own coordinate with the name,
    because there is no map to drop a pin on. A moderator sees "ΚΤΕΛ Φερών" and
    has to know, or find out, where that is.

    It is honest as far as it goes — the point does not claim to be anywhere
    precise — but it means the reviewer is doing work the person suggesting it
    could have done in one tap. This is the first thing the map should fix when
    there is one.

## The legal premise is thinner than the code assumes (raised 2026-08-05)

Every comment in the pricing path leans on the same sentence: άρθρο 18
ν. 1903/1990, as amended by άρθρο 36 ν. 4663/2020, fines a private driver
carrying passengers με κόμιστρο, so the formula makes full recovery
arithmetically impossible and the driver can say they did not come out ahead.

A review of the estimate work pushed back on that, and the objection is worth
recording rather than arguing with in a commit message:

- Άρθρο 18(1) concerns transport for remuneration **assigned to another carrier
  or person**. It is not a general cost-sharing safe harbour, and reading it as
  one is doing more work with it than it can carry.
- Άρθρο 1 ν.δ. 4354/1964 uses broader language — passenger fare, total hire, or
  "any other remuneration" — and whether genuine proportional expense-sharing
  falls outside it is not settled by reading the text.
- Άρθρο 12 ν. 4530/2018 makes *influencing the price* relevant to how an
  electronic intermediary is characterised. This app computes the figure both
  sides see, which is influence by any ordinary reading, even though the
  provision's express scheme is about public-use vehicles.

So: "the driver can never recover the whole fuel cost" is good evidence of
non-profit design and is **not** proof that the contribution is not κόμιστρο.
The code has always said "a risk control, not a safe harbour"; the risk is
larger than that phrasing suggests.

Before any public launch, a written Greek transport-law opinion covering:

1. The current force and reading of ν.δ. 4354/1964.
2. Whether proportional fuel-only contribution is legally distinct from
   remuneration.
3. Whether the platform computing the number amounts to influencing the price.
4. Insurance treatment of expense-sharing with strangers.
5. Tax, receipt and platform-fee treatment.

Until then, nothing in the pricing path should grow: no tolls beyond what is
already there, no depreciation, no insurance component, no percentage fee. Each
of those moves the contribution further from "what the fuel cost" and closer to
the thing the statute is about.

The planning estimate added on 2026-08-05 was written with this in mind — it
carries a "not an offer, no driver has agreed" sentence in the UI — but that is
mitigation, not an answer.

## Deferred tech (parked, with triggers to revisit)

| Deferred | Revisit when |
| --- | --- |
| H3 hex prefilter | gross-match SQL > ~100 ms p95 |
| Materialized candidate views | per-event matching > ~1 s p95 |
| NATS / broker, multi-node API | single VPS saturated |
| MapLibre + self-hosted tiles | Google Maps key/cost pain |
| Driving detection (bluetooth/activity) | post-MVP, product pull exists |
| Live location sharing | post-MVP milestone 2 |
| Device-bound refresh tokens (DPoP) | refresh-token theft is observed, or the pilot handles anything worth stealing |
| Rust rewrite of hot paths | never, unless profiling proves need |

## Process notes

- grok CLI is currently broken (grok.com WS protocol change, `session.created`
  frames unparsed) — research fallback is WebSearch until grok-cli is fixed.
- Implementation plan: build on the mini PC; parallelize with Codex offload lanes
  (disjoint file scopes, tests as contract) per Harold's codex-offload workflow.
