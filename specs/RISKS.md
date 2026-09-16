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

   **Still the biggest one, and now researched: see `specs/NEXT.md`.** Every
   European daily-commuter service solved this with an employer or municipality
   partnership and subsidised trips, not with product. The pilot plan above is
   sound, but it needs an institution attached to it before more code is written.

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

14. **Half of the prefecture's main corridor cannot be published — fixed
    2026-09-02, with a matcher limitation accepted in its place.**
    `route_templates_geom_simple` rejected a route whose geometry is not simple,
    and the reasoning was sound: `ST_LineLocatePoint` cannot place a point on a
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

    Measured again on 2 September 2026, this time against the whole-Greece
    extract: of ten routes between 2.5 and 164 km, Αλεξανδρούπολη → Μάκρη came
    back non-simple — 516 vertices, no repeated coordinate, one genuine crossing
    at (25.75149, 40.85981). `ST_RemoveRepeatedPoints` changes nothing about it.

    Fixed by migration 00031, which drops the constraint. Repairing the geometry
    instead was rejected: `ST_Node`, `ST_UnaryUnion` and `ST_LineMerge` work on
    planar linework rather than on an ordered traversal, so they can drop a
    repeated pass, reverse a direction, or return a `MultiLineString`. The
    stored line has to stay exactly the ordered sequence OSRM returned, because
    that order is what every fraction along it means.

    **What is now accepted, and is not fixed.** On a non-simple line
    `ST_LineLocatePoint` returns the earliest position of minimum distance
    rather than failing, so a pin within metres of a crossing can be placed on
    the first pass instead of the second. On Αλεξανδρούπολη → Μάκρη the two
    passes are 0.54 km apart on a 17.8 km line, which is why this was judged
    cheaper than publishing nothing — but the bound is the route length, not
    that gap. A route that revisits the same coordinate near both ends can have
    a 5 km shared section read as nearly the whole route, or an `f_o`/`f_d` pair
    read in the wrong order and dropped. Anything downstream that assumes a
    monotonic fraction — toll attribution, detour, meeting points, progress —
    inherits it.

    The real fix is occurrence-aware projection: enumerate the candidate
    segments with cumulative measures and choose the pickup/dropoff pair
    jointly, rather than taking each endpoint's nearest position independently.
    Even that cannot tell two levels of an interchange apart without edge
    identity from OSRM. Nobody has needed it yet; when a mismatch is reported on
    a route that crosses itself, this is the paragraph it belongs to.

    First discovered because `scripts/seed-demo.sh` could not publish the second
    route, and the handler was throwing the reason away. It now logs it — which
    is how the reason was in hand within a minute the second time somebody hit
    it, from the app rather than a script.

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
    add a point, and anybody can suggest one for review.

    Mostly solved 2026-08-08, and the paragraph above was wrong about why. The
    harvester did compute a careful ordering; it then threw it away when writing
    the file, because the `rank` column held the *kind* index rather than the
    position. Every bus station in a city therefore shared one rank and the app
    fell back to sorting by OSM id. Θεσσαλονίκη showed "Arda Tur Bulgaria" first
    not because ranking was impossible but because nothing kept the ranking.

    Two signals turned out to be in the name after all, contrary to the claim
    above that no automated rule could find them: whether a bus station says
    ΚΤΕΛ, which is what everybody in Greece means by the bus station, and
    whether it names *this* prefecture rather than another one — ΚΤΕΛ
    Καρδίτσας-Τρικάλων has an office in Λάρισα and is still Καρδίτσα's station.

    Measured: 93 settlements now lead with a different point, one changed which
    points it has at all, and the total is unchanged at 2900. Θεσσαλονίκη leads
    with ΚΤΕΛ Θεσσαλονίκης, Λάρισα with its own, Τρίκαλα with the station rather
    than the ticket window, Ρόδος with Κ.Τ.Ε.Λ. rather than the urban network.

    What is left genuinely needs a person, and it is a much shorter list.
    Πάτρα leads with "ΚΤΕΛ ΚΕΦΑΛΛΗΝΙΑΣ Πατρα" — a real coach office that names
    both Kefalonia and Πάτρα, so the rule cannot tell it from Πάτρα's own.
    Πειραιάς leads with "ΒΟΛΓΑ", because no ΚΤΕΛ is mapped there at all. Ιωάννινα
    and Κατερίνη have petrol stations because they have nothing better tagged.

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

18. **A clipped routing extract invents routes instead of refusing them —
    MEASURED and fixed 2026-09-17.** This is the one on this list that was
    quietly producing wrong data in the pricing path.

    OSRM does not refuse a coordinate its extract does not cover. It snaps to
    the nearest road it does have and answers `Ok`. Measured against
    `evros-buffered`, the artifact production serves:

    | Coordinate | Snapped | Result |
    | --- | --- | --- |
    | Αλεξανδρούπολη, Κομοτηνή | 0,0 km | correct |
    | Ξάνθη (edge of the clip) | 6,5 km | correct enough |
    | Θεσσαλονίκη | 173 km | fabricated |
    | Αθήνα | 313 km | fabricated |
    | Πάτρα | 408 km | fabricated |

    Θεσσαλονίκη → Αθήνα returned `Ok` with a 186,6 km route drawn through
    Thrace. Nothing downstream could tell that from a real answer: it is the
    geometry stored in `route_templates.route_geom`, the line
    `ST_LineLocatePoint` locates pickups along, and the distance the
    contribution formula is applied to. A driver publishing a journey outside
    the extract got a plausible number for a road in the wrong prefecture.

    Why it survived: the only geographic gate is `RegionAt`, which asks whether
    a point is in Greece, and the shipped region fixture is all 75 prefectures.
    "In Greece" and "in the routing extract" had silently become different
    questions the day the extract was clipped, and nothing asked the second one.

    Fixed by sending a snapping bound on every routing call —
    `geo.SnapRadiusMetres`, 20 km — chosen from the gap in the table above
    rather than picked: an order of magnitude above the worst legitimate snap
    and an order of magnitude below the best fabrication. OSRM then answers
    `NoSegment`, which had a sentinel error and no handler and so surfaced as a
    502; publishing and estimating now answer `400 outside_service_area`.

    **Consequence for deployment.** The served area is whatever the deployed
    extract covers, and that is now enforced rather than documented. Swapping
    `OSRM_EXTRACT` changes which trips the app accepts, so the extract is a
    product decision and not only a memory one. If the radius is ever widened
    past the clip's buffer, the fabrication comes back.

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
| Driving detection (bluetooth/activity) | rejected, not deferred — see "Driving detection cannot work" below |
| Live location sharing | post-MVP milestone 2 |
| Device-bound refresh tokens (DPoP) | refresh-token theft is observed, or the pilot handles anything worth stealing |
| Rust rewrite of hot paths | never, unless profiling proves need |

## Process notes

- grok CLI is currently broken (grok.com WS protocol change, `session.created`
  frames unparsed) — research fallback is WebSearch until grok-cli is fixed.
- Implementation plan: build on the mini PC; parallelize with Codex offload lanes
  (disjoint file scopes, tests as contract) per Harold's codex-offload workflow.

## The age gate is self-reported (raised 2026-08-05)

Signing up now asks for a date of birth and refuses anybody under eighteen.
Nothing verifies it. Somebody who types 1990 instead of 2010 is in, and the app
has no way to know.

This is the same position every carpooling service in the country is in — GoRide
asks and does not verify either — and the alternatives are worse than the
problem: an ID check at signup would collect identity documents from every user
for a service that handles no money, which is a far larger data-protection
liability than the one it closes.

What it does buy is a stated rule, a refusal that has to be actively lied to,
and a record. What it does not buy is knowledge.

Two related things are deliberately not built:

- **Minors travelling with an adult family member**, which GoRide's terms allow.
  Nothing in a booking can tell the app that the adult in the next seat is
  somebody's parent, and a rule the app cannot check is a rule it should not
  claim to have.
- **A per-seat age check.** A booking is made by one account for one or more
  seats. The account holder's age says nothing about who else gets in the car.

## The bio filter is a speed bump, not a wall (raised 2026-08-05)

A profile bio is now refused if it carries an email address, a link, a Greek
mobile number or a platform handle — including the obfuscated forms people
actually use (`at`/`dot`, `παπάκι`/`τελεία`, full-width digits, zero-width
characters wedged into the middle).

It will never be evasion-proof. Somebody determined to publish their number can
spell it out in Greek words, and no closed set of patterns catches that without
flagging ordinary prose about years, prices and distances. The trade is
deliberate: a filter that eats normal Greek gets switched off, and then it
protects nobody. Ordinary bios are the larger test set of the two.

What it stops is the ordinary case — somebody who does not think of a bio as
public typing their number in because it seemed helpful.

Two loose ends:

- **Only the bio is filtered.** Codex's review named the other stranger-visible
  free-text fields as the same bypass. Right now the bio is the only one, but
  a trip description or a pickup instruction would need the same check on the
  day it is added.
- **facebook_handle and instagram_handle are still public**, by design. The
  distinction is not "no off-platform contact" — it is between an identity
  somebody deliberately published and a number buried in prose where it will not
  be noticed until it is too late. That distinction is a judgement, and somebody
  may reasonably disagree with it.

## The report reason is text, not a column (raised 2026-08-08)

Reporting somebody now asks what happened and offers six categories. The chosen
category leads the reason string — `no_show: με περίμενε μισή ώρα` — rather than
being stored in its own column.

That is deliberate and it is a bet. A column is the right shape the day somebody
actually works the queue and wants to filter it, count repeat categories, or
route harassment differently from a no-show. Until then it is a migration and an
enum for nobody, and the reason has always been free text that a human reads.

What makes the bet cheap to lose: the categories are stable identifiers rather
than the Greek labels, and they lead the string. Promoting them to a column
later is a backfill with a regular expression, not an archaeology exercise.

What makes it a bet at all: nothing enforces the format. A future client could
send a bare sentence, and the backfill would find nothing to promote.

## Two retention periods the code will not enforce until somebody decides (raised 2026-08-08)

Eight of the ten periods in the privacy policy are now enforced by the janitor.
Two are not, and neither is a matter of finding the time.

### Blocks: the policy says 36 months, and enforcing it would unblock people

"Αναφορές και αποκλεισμοί — 36 μήνες" puts blocks in the same row as reports. A
report is a record of a past event and deleting it after three years is ordinary
data minimisation. A block is not a record of anything: it is a live safety
preference, and deleting it makes somebody visible again to a person who
deliberately made them invisible — silently, three years later, with no
notification and no way for the blocked-from party to know it happened.

That is a safety regression dressed as a privacy improvement, so the code does
not do it.

The recommendation is to change the policy rather than the code: blocks should
live as long as the account, like ratings, and the row in the table should say
so. If the answer is instead that blocks really must expire, then it needs to be
a deliberate product feature — telling the person, and letting them renew —
rather than a row vanishing out of a nightly job.

### Ratings: the period is anchored on something the schema does not record

"Αξιολογήσεις — όσο ο λογαριασμός είναι ενεργός, και 12 μήνες μετά τη διαγραφή."
Every other rule is age-based and can be written as one predicate. This one
needs the deletion time of the rated account, and erasure currently anonymises
the user row rather than leaving a durable tombstone with a timestamp on it.

So the rule cannot be written correctly yet. Codex put it plainly: without a
durable deletion timestamp there is no query that expresses this period.

Two ways out, and they are not equivalent:

1. Record a `deleted_at` on erasure and sweep ratings twelve months after it.
   Straightforward, but it means the erasure path leaves a dated tombstone for
   every deleted account — a small permanent record of the fact that somebody
   was here and left.
2. Delete the ratings at erasure time and drop the twelve months from the
   policy. Simpler and holds less, but loses the reason the twelve months are
   there in the first place: a rating somebody left about *another* person is
   evidence about that person, and erasing your account should probably not
   scrub your account of everything you said about everyone else.

Option 2 has a sharper version worth considering: keep the ratings the deleted
person *gave*, drop the ones they *received*. What survives is then about the
people still here, which is closer to what the twelve months were protecting.

### What is enforced today

otp codes, phone-change challenges, signup tickets, refresh tokens, messages,
push tokens, invite codes, reports, trips and ride requests.

Security logs are the eleventh item and are files rather than rows — nothing in
the database can enforce them, and the deployment's log rotation has not been
checked against the twelve months the policy claims.

## A push token can be swept between selection and delivery (raised 2026-08-08)

CodeRabbit found the window: `tokensFor` reads the live tokens, the provider
accepts the notification, and only then is `last_used_at` written. The janitor
can delete the row in between, so the write lands on nothing.

Not fixed, on these grounds. The precondition is narrow — the token has to be
two years unused *and* receive a successful delivery in the same moment the
sweep runs — and the consequence is that one device stops receiving until the
app next registers, which it does on launch. Coordinating deletion with delivery
would mean holding a lock across a network call to Expo, which is a worse trade
than the thing it prevents.

It is worth revisiting if push registration ever stops happening on every
launch, because the repair is what makes the loss temporary.

**Correction, 2026-08-11.** That repair did not exist when this was written.
`api.registerPushToken` was defined and called from nowhere, so nothing
re-registered on launch and the loss would have been permanent. The reasoning
above was sound and the fact it rested on was not. The app now registers on
every launch while signed in, which makes the argument true rather than merely
plausible.

## CodeRabbit asked for the retention decisions to be closed (2026-08-08)

Not a defect, and recorded so the next reader does not read it as one. The
review flagged the two open retention entries above and said they should be
resolved before release. That is correct and it is what they are here for: both
need a person to decide what the published policy should say, and neither is
work waiting to be done.

## Driving detection cannot work, and the reason is not privacy (raised 2026-08-11)

It was parked as post-MVP with a note that product pull exists. Designed
properly with Codex, it turns out to be parked for a better reason than
sequencing: there is no signal that tells a private car from a coach or a train.

The exit from home precedes every mode. An intercity KTEL coach holds the same
speed as a car on the same national road. The railway runs parallel to it, so
OSRM would snap a train onto the road and report driving. The operating
system's own `automotive` activity means "in a vehicle" and includes bus and
train. Having a car on the profile changes the prior, not the classification.

Three things that were not in the original note:

- Checking the road class means sending the fixes to our own OSRM, which is
  precisely the passive tracking the privacy policy says we do not do, and it
  would land in access logs as well.
- Google Play requires background location to be *core functionality*, with a
  demonstration video and a declaration. The app works without it, so review
  would likely call it convenience — putting the whole listing at risk for a
  feature that does not work.
- Android geofence transitions arrive two to three minutes late, up to six after
  a long stationary period. The prompt therefore arrives while the person is
  already driving, which is the worst possible moment to ask somebody to pick up
  a phone.

The only signal that means *car* rather than *vehicle* is the Bluetooth device
the user picks themselves ("choose your car stereo"). There is no first-party
Expo API for observing an arbitrary classic Bluetooth connection in the
background; it needs a native module, and on iOS it is largely closed outside
CarPlay entitlements.

**Decision: not deferred pending sequencing, rejected pending a signal.** Revisit
only if a user-chosen Bluetooth device becomes reachable from Expo without a
custom native module. What the feature was for — a driver not having to think
about publishing, a passenger not having to search — is what M3b and the
recurring-schedule reminder do instead, from data drivers declare themselves.

## Where the two matching directions are allowed to disagree (raised 2026-08-11)

`match.Waiting` is deliberately *not* a mirror of `grossSQL`. Codex argued for
this and it is right: the two are separate queries, and the dangerous direction
is the reverse one being stricter. A pair it misses fails silently — the driver
publishes, nobody is woken, the request expires beside a car with room — which
is indistinguishable from the bug the whole exercise fixed. A pair it admits
that the forward filter then rejects costs one indexed query.

So the contract is containment, not equality: everything `Gross` matches,
`Waiting` must return. `Waiting` may return more.

That property is tested rather than asserted by reading the two side by side.
Worth recording how weak the first version of that test was: mutating the
reverse query to trim twenty minutes off either window edge, or to demand
strictly more luggage room, passed — because no fixture sat on a boundary. The
fixtures now include both window edges and a request whose luggage exactly fills
what the trip allows, and all three mutations fail.

The seat cap is shared as a SQL fragment (`match.SeatsSpokenFor`) rather than
written twice, because drift there is not a false negative in one direction but
either a notification storm or a trip nobody can be asked about.

## Departing from Codex on the ask cap (raised 2026-08-11)

Codex recommended one live ask per occurrence, conservatively, and that is not
what was built.

An offer stands for thirty minutes. One at a time means a three-seat car leaving
in two hours takes ninety minutes of serialised answer windows to fill, and the
third passenger is asked after the driver has left. The bound implemented is the
seats themselves: the sum of party sizes across live offers never exceeds the
free seats. It gives the same protection on the case that matters — exactly one
offer on a one-seat trip, which is where the "you have it / you have lost it"
whiplash comes from — without penalising a full car.

Codex's related suggestion of a per-driver ceiling is *not* built. A driver with
ten upcoming departures being asked about each is ten separate journeys, not
spam. Revisit if the recurring-schedule work makes a driver materialise many
occurrences at once and the asks arrive together.

## A suspension that the matcher never heard about (found 2026-08-11)

Found by a QA pass over the account-status boundary, with a Codex review of the
fix.

Suspending somebody stopped them using the app and never stopped the app using
them. Their open request stayed in the queue, so the next trip published on that
corridor woke it and a driver was offered a real seat to somebody who could not
open the app to agree a meeting point or say they were not coming. An offer
already on a driver's phone was still a tap away from the same booking. From the
driver's side there is nothing to see: an ordinary passenger who never replies.

Worse on the driver's side of it. The cascade cancels the departures and not the
template behind them, and the materializer's job is to make what a schedule
implies — including reviving a cancelled departure, which is right when a driver
lifts an exception they had set. It looks for departures with no confirmed
booking and no live offer, which the cascade has just arranged, and puts them
back. Measured: one pass, nine departures, a suspended driver back in the pool.

Four guards at four distances, because one of them being enough is what got us
here:

- the cascade withdraws the live offers, requests and weekly arrangements
- the materializer skips a template whose driver is not active
- both matching filters read the account, `= 'active'` and never
  `<> 'suspended'`, which would let a closed account back in
- accept and ask creation hold both people's rows `FOR SHARE` first, because a
  status read is a filter and not a gate: suspension can commit between the read
  and the insert, after the cascade has swept past.

The materializer checks the driver rather than mirroring a flag onto the
template, so reinstatement needs no bookkeeping about which routes a moderator
switched off and which the driver had retired themselves.

Two things deliberately not done. Reinstating within the horizon will let the
materializer plan the mornings that were called off; the bookings stay cancelled
and the departure simply becomes available again, which is what a reinstated
driver's schedule means. And a push queued before the cascade can still be
delivered for an ask that is now cancelled — the ask screen already handles a
stale tap and says why, so revalidating every notification at delivery time buys
a quieter phone at the cost of a database read per push and a new way for
delivery to fail.

## What the QA pass found that was not about suspension (2026-08-11)

- `DELETE /v1/requests/{id}` discarded its row count, so every call answered 204:
  somebody else's request, one that never existed, and one a driver had just
  accepted. The screen it is called from is the one somebody sits on *while*
  drivers are being asked, so the third case is ordinary rather than rare, and
  the app read 204 as done and navigated home. They were told they were not
  going while a seat was held in their name.
- Nothing ever expired a ride request. Two filters read the window rather than
  the status and each said in a comment that a sweeper would come along; there
  was no sweeper, and `expired` was a status the schema allowed and no code path
  could produce.
- Erasure scrubbed `driver_profiles` and never touched `vehicles`, so the
  registration plate survived a deletion. The export had the mirror gap: no car,
  no driver settings, no blocks, no complaints filed.

Left alone, with the reasoning:

- The self-scoped lists (`/me/routes`, `/me/vehicles`, `/arrangements`,
  `/me/reports`) have no pagination. Each is somebody's own data and bounded by
  their own use of the app. Paginating five endpoints and five screens is
  disproportionate before a pilot; revisit if publishing ever becomes automated.
- A request whose window is in the past is accepted at creation. It matches
  nothing and the sweeper now finishes it, so refusing it would only be a
  friendlier error, not a fix.

## A trip is a candidate until it is due to leave, not until it has left (raised 2026-08-12)

The matcher compared a departure against the passenger's window, which is
written once and never moves. Drivers are asked one at a time with half an hour
each, so a request naming a morning is still being matched hours into that
morning, and every departure inside that window which had already gone stayed a
candidate. Both directions had it: the forward filter offered them, and a seat
handed back after a car left woke everybody waiting on the corridor.

The boundary now used is `depart_earliest`, because that is what the rest of the
app already means by departed: the moment a pair may confirm the ride and rate
each other, and the moment a driver may no longer call the trip off. A second
definition in the matcher would be exactly the sort of drift that produces a
trip which has left for one rule and not for another.

**What it costs.** Somebody running for a driver who is late cannot be matched
into that car, even though the driver may not pull away until the far end of
their window. Doing it properly needs a real departure time — the driver saying
"leaving now", or a live position — rather than the near end of a window. Both
are post-MVP. Until then the app declines a booking it cannot promise.

## Reopening a stranded request happens after the cancellation, not inside it (decided 2026-08-12)

Raised the same day as a lock-ordering hazard and closed rather than left open.

A cancelled departure has to put its passengers back in the matching queue.
Doing that in the transaction that cancels the departure meant a suspension, an
account closure or a driver calling off a trip also locking a `ride_requests`
row belonging to every passenger it stranded — taken after the occurrence and
without those passengers' user rows, which is the one order the rest of the app
does not use. Two moderators acting at once could each hold what the other was
waiting for.

The alternative considered was pre-locking every affected passenger's user row
in sorted order before touching anything. That was rejected: it turns one
moderator action into a lock over an unbounded set of accounts, and it still
has to discover that set before it can sort it.

So the cancelling transaction records one `reopen_request` event per lost seat
and commits. The dispatcher does the reopening, where the passenger and their
request can be taken in the ordinary order. The three copies of the reopening
logic became one.

**What the split costs.** There is a moment when the booking is cancelled and
the request has not reopened yet. The passenger has already been told their ride
is off, so nothing is claimed that is not true; nothing further is promised
until a driver is actually asked. And because time passes, everything the
producer knew is re-read: the request is reopened only if it is still matched,
still inside its window, has no live booking of its own, and belongs to an
active account. Each of those was a way somebody could have moved on — cancelled
the journey themselves, been suspended, been matched by somebody else — and each
is a quiet no-op rather than a failure.

## Confirming a ride only proves the clock passed (raised 2026-08-12, narrowed the same day)

Confirmation required `depart_earliest <= now` and nothing else, so a pair could
confirm — and then rate each other — for a journey neither made. Every rating in
the app rests on that.

The boundary has moved to `depart_latest`, the far end of the departure window.
A trip that may leave any time between 07:00 and 07:30 has not happened at
07:01, and the app was agreeing that it had while the driver was still at the
kerb. Both sides must confirm before a booking completes, and only a completed
booking can be rated, so this is the gate in front of every rating.

**What is still open.** Two people who agree to it can confirm a ride neither
took. No timestamp rule can fix that; it needs evidence that a journey happened
— a driver marking "on the way", two devices near each other at the pickup, a
code read out at the kerb — and the first two are the post-MVP location work
this file already parks. A rating here means "both of them said it happened",
and the app should not be built to imply more than that.


## A new outbox kind is not safe to introduce during a rolling deploy (raised 2026-08-12)

`reopen_request` is the first message kind added since the dispatcher was
written, and Codex pointed out what that costs at deploy time: an old worker
treats an unknown kind as a permanent failure, so during a rolling deploy a new
producer can commit a message that an old worker dead-letters. The passenger it
was for is then not put back in the queue by anything except the hourly
reconciler.

Not a problem today — the pilot runs one instance and deploys stop the world —
and it is written down because the shape of the mistake outlives that. Before
there are two workers, either unknown kinds have to become retryable rather than
permanent (so an old worker leaves them for a new one), or a kind has to be
taught to every worker in one release and produced in the next.

## What a deleted account still holds, and where that is enforced (raised 2026-08-12)

Erasure now takes the photograph, the bio and the preference tags, so the
endpoints that read those columns come back empty for somebody who has left.
That means the reads are safe *because the data is gone*, not because they
check. Only the photo endpoint checks as well, and it does because a photograph
is the one held thing that identifies somebody to a stranger on sight and it
lives in a table of its own that an erasure has already been caught missing once.

Codex's suggestion, recorded rather than built: centralise the user projection
so that a deleted account exposes the placeholder identity and nothing else,
whatever new column somebody adds to a profile later. That is the real fix for a
class that has now been found three times — the plate, then the face and the
bio. It is a refactor across every endpoint that names a person, and doing it
badly would break the trip screens, so it wants its own change rather than the
tail of a QA pass.

**What is deliberately kept.** Chat messages written by somebody who has left
stay standing, because a message is half of somebody else's conversation and
deleting one side rewrites the other person's record. Codex is right that this
means the app holds free text authored by an erased account, with a stable link
to the thread it was in, and that this is retained personal data rather than
anonymised data. It is covered by the ordinary retention window that deletes old
trips and their messages; it is not covered by "erase me" and should not be
described as if it were.

## Between the two ends of a departure window a driver can do nothing (raised 2026-08-12)

Moving confirmation to `depart_latest` left a gap that Codex named: from the
moment a trip may leave until the moment it must have left, the driver can
neither call it off (that closes at `depart_earliest`, because a trip that may
already be moving is not one to cancel) nor confirm it. A driver still sitting
at the kerb at 07:01 with a passenger who has not come has no honest action for
the rest of the window.

Left as it is rather than papered over. Extending cancellation to the far end
would let a driver call off a trip they are already driving, with passengers
aboard, which is worse than a gap. What the gap actually wants is a departure
the driver declares — "leaving now", or a no-show recorded against a booking —
and both are the post-MVP work this file already parks. Recorded so that the
next person to read the cancellation rule knows it is half of a pair.

## No cancellation notice has ever been delivered (found 2026-08-13)

Six places queued a push describing something that had happened —
`booking_cancelled` from four different cancellation paths, and the four
standing-arrangement events. Each wrote `{"event": …, "recipient_id": …}`. The
dispatcher decoded neither field: it inferred the recipient from which ids
happened to be present, recognised only the two shapes the ask and accept paths
write, and dead-lettered everything else as "payload names no recipient".

Measured against a running server before the fix: two of two `booking_cancelled`
messages dead. So nobody has ever been told their ride was called off — not when
the driver cancelled the departure, not when the driver closed their account,
not when either side was suspended — and no standing arrangement has ever been
answered on somebody's phone.

Every producer had a test, and every one of those tests asserted that the outbox
row had been written. Not one carried the row the rest of the way. That is the
whole of how a producer and a consumer can disagree for months in a codebase
with 780 tests: both halves were tested and the seam between them was not.

**The fix.** `internal/notices` is now the only thing that builds a notice and
the only thing that reads one back. Producers no longer hand-roll a JSON map;
they call a constructor that refuses to return an incomplete payload, so the
error surfaces inside the transaction that would have queued it rather than at
delivery. The dispatcher's test walks `notices.All()`, so an event added without
words for it fails the build rather than somebody's lock screen.

`booking_cancelled` also became two events. One name covered both audiences, and
no sentence is true for both: a passenger has lost a ride, a driver has a seat
back. Neither says who did it, because the same fact reaches a passenger whether
the driver cancelled, closed their account or was suspended, and only the first
is the driver's doing.

**What is knowingly not done.** An unknown event is dead-lettered rather than
retried through a compatibility window, so a producer shipped ahead of its
dispatcher loses those notices; the row keeps its payload and can be replayed by
hand, which is the same trade already recorded for `reopen_request` under
rolling deploys. There is no alerting on the dead-letter table — reading it is
still somebody remembering to look. And the two older payloads (an ask offered,
a booking confirmed) still travel in the shape-inferred form, kept so that rows
queued by the version running before a deploy stay deliverable by the version
after it; they should move to the envelope once no such rows can exist.

## `trip_km` was the crow-flies distance (found 2026-08-13, fixed 2026-08-14)

MATCHING.md line 164 defines `trip_km` as the OSRM distance along the driver's
route between the passenger's pickup and drop-off. The code priced with
`ST_Distance(origin, destination)` — the straight line between the passenger's
own endpoints, which ignores both the road and the driver's route.

It was recorded here as a documentation drift on the grounds that the straight
line always under-quotes, and that under-quoting is the safe direction for a
formula whose job is to stop a driver coming out ahead. Both halves of that were
wrong enough to matter:

- Under-quoting is not harmless where `free_below_km` sits. A driver's threshold
  is five kilometres by default. Two stops on opposite banks of the Έβρος are
  four hundred metres apart on the map and eleven kilometres apart by road:
  every such ride was waived outright, and the driver gave away eleven
  kilometres of their own petrol because a straight line does not know about the
  river.
- The straight line is not even reliably shorter. Codex produced the
  counterexample: a pickup three kilometres north of the route and a drop-off
  three kilometres south of it are six kilometres apart, while the arc between
  their projections is one. A fallback to the straight line can over-quote by
  six times, so it is not a safe fallback either.

**The fix cost one line.** The gross filter already computes this exact arc —
`(f_d - f_o) * route_km` from `ST_LineLocatePoint` — to decide whether an
overlap is worth an offer, and every candidate arrives carrying it as
`SharedKm`. Pricing was the one place that went and measured something else.
Because the filter that admits a candidate has already established that the
stops are in order and the arc is at least a kilometre, there is no error path
to fall back from: no extra query, no second projection to disagree with the
first.

**Amended 2026-09-07, after migration 31.** "No failure mode" was written when
every stored route was required to be simple. Migration 31 dropped that
constraint, because real roads cross themselves and the Αλεξανδρούπολη → Μάκρη
route could not be published. On a self-crossing line `ST_LineLocatePoint`
returns the earliest position of minimum distance rather than failing, so a stop
sitting within metres of a crossing can be located on the first pass when the
passenger means the second. Two consequences for pricing, both bounded:

- The arc can be measured over the wrong passage, which on the measured route is
  0.54 km of difference. The figure stays a suggestion, and the gross filter's
  one-kilometre floor still applies, so it cannot turn a short hop into a long
  one.
- If both stops land on reversed passages, `f_d - f_o` can come out negative;
  the filter rejects the candidate before pricing sees it, so this shows up as
  a trip that is never offered rather than as a negative price.

What is not claimed any more is that the arc is exact for every stored route.
It is exact for a simple one — which is nearly all of them — and on the rest it
is the arc of whichever passage the projection chose. Fixing this properly means
carrying the intended fraction from the match rather than re-projecting, and
that is worth doing when a corridor with a crossing carries real traffic, not
before.

`BasisVersion` is 3, and a receipt now records `trip_km_source`. A receipt
without one was written before this and is read as the straight line, because a
figure whose provenance is unrecorded is a figure nobody can argue with.

**What this does not change.** The distance is the arc between the projections
of the two stops, not the passenger's own road distance and not the driver's
detour out to fetch them. A passenger picked up three kilometres off the
corridor does not pay for those three kilometres; that is the driver's detour
budget, which they set and agreed to. It also means the figure is not the same
one the pre-request estimate quotes — that endpoint routes the passenger's own
journey, which is a different question with a legitimately different answer.

**What was checked rather than assumed.** Codex raised two ways a frozen receipt
could be silently invalidated: seats changing between quotes (four passengers
frozen at divisors 2, 3, 4 and 5 collect 1.28 times the cost) and the route
moving under a price derived from it. Neither is reachable — the materializer's
mutable predicate freezes an occurrence whole the moment anything is booked on
it or a driver is being asked about it — and there is now a test asserting that
against the columns pricing reads rather than against the status.

**Left alone.** `match.Gross` still measures its shared-distance floor against
the crow-flies request length (at least a kilometre, and at least 20% of it).
That is a matching filter and not a price: it decides whether an overlap is
worth an offer. Codex is right that a crow-flies request length is not a
conservative bound for the arc, so the floor could in principle reject an
off-corridor candidate it should keep. Changing it changes who gets matched,
which is a different question from what they are asked to contribute, and it
wants its own change with its own recall evidence.
