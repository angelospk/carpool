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

## Deferred tech (parked, with triggers to revisit)

| Deferred | Revisit when |
| --- | --- |
| H3 hex prefilter | gross-match SQL > ~100 ms p95 |
| Materialized candidate views | per-event matching > ~1 s p95 |
| NATS / broker, multi-node API | single VPS saturated |
| MapLibre + self-hosted tiles | Google Maps key/cost pain |
| Driving detection (bluetooth/activity) | post-MVP, product pull exists |
| Live location sharing | post-MVP milestone 2 |
| Rust rewrite of hot paths | never, unless profiling proves need |

## Process notes

- grok CLI is currently broken (grok.com WS protocol change, `session.created`
  frames unparsed) — research fallback is WebSearch until grok-cli is fixed.
- Implementation plan: build on the mini PC; parallelize with Codex offload lanes
  (disjoint file scopes, tests as contract) per Harold's codex-offload workflow.
