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
3. **OSRM real memory footprint.** "A few hundred MB" is an estimate, not a
   measurement. Action: build the Greek/prefecture extract on the mini PC and
   measure serving RSS **before** choosing the VPS plan.
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
