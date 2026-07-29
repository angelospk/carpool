# Progress

Running status log so any future session can resume cold. Updated every work session.

## Current state

**Last updated:** 2026-07-29.
**Not blocked.** Docker is installed and the dev stack runs. The whole suite is
green against the spec'd `postgis/postgis:16-3.4`.

## How to run things

```bash
docker compose -f docker-compose.dev.yml up -d      # postgis + osrm
export CARPOOL_TEST_DSN="postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable"
make check                                          # vet + staticcheck + tests (~8s)
```

Integration tests skip themselves when `CARPOOL_TEST_DSN` is unset. Each test
creates and drops its own database, so they are safe to run in parallel and
against any server.

The dev database runs with `fsync=off`. That is a deliberate development-only
trade: each integration test creates and drops its own database, and fsync
dominated the runtime (~240s before, ~8s after). The production compose must
never carry those settings.

Rebuilding the OSRM extract:

```bash
scripts/build-osrm.sh osrm-data/evros-buffered.osm.pbf
```

## Environment notes (this machine — mini PC)

- Go 1.23.3 installed; module pinned to `go 1.25.0` (pgx v5.10 needs it), so Go's
  auto-toolchain fetches 1.25.x transparently.
- `staticcheck` 2026.1 installed to `$(go env GOPATH)/bin` — the Homebrew one on
  PATH is 2024.1.1 and cannot parse Go 1.25, so the Makefile calls the GOPATH
  binary explicitly.
- Homebrew (rootless) provides `goose`, `osmium`, `osrm-backend`,
  `postgresql@16`, `postgresql@17`.
- Docker 29.1.3 + compose v2.40 installed. `docker-compose.dev.yml` runs
  `postgis/postgis:16-3.4` on host port 5433 and OSRM on 5000.
- The OSRM image ships neither wget nor curl, so its healthcheck uses bash's
  `/dev/tcp`.
- Earlier sessions used a rootless PG 17 + PostGIS 3.6 as an interim target
  before Docker existed. That is no longer needed; everything has since been
  re-verified on PG 16 + PostGIS 3.4.

## Milestones

- [x] **M0 Skeleton** — module layout, `make check`, config, injectable clock,
      chi router with `/livez`+`/readyz`+`/health` and the JSON error envelope,
      embedded advisory-locked goose runner, integration harness, dev compose,
      `cmd/api` with graceful shutdown.
- [x] **M1 Schema + auth** — full DATA-MODEL.md schema; phone OTP (hashed,
      single-use, attempt- and rate-limited, SMS behind an interface with a
      console dev sender); access JWTs with HS256 pinned; refresh tokens with
      rotation, replay detection and per-device families; `/me` read+update;
      public region listing; region seeding from the checked-in Evros fixture.
- [x] **M2 Routes & occurrences** — DST-safe wall-time resolver; schedule
      planner; OSRM client; route template + schedule store; occurrence
      materializer reconciling against a rolling calendar horizon under a
      per-template advisory lock, freezing anything booked, asked-about or past.
- [x] **M3 Matching** — stage-1 gross SQL (direction-aware, distance floor,
      block-aware) with an EXPLAIN index assertion and the full geometry table;
      stage-2 OSRM insertion detour with the 2-party cap; ranking with a total
      tie-break; suggested-contribution formula.
- [x] **M4 Ask/booking lifecycle** — outbox with leased dispatcher, per-claim
      fencing, backoff, dead-lettering; sequential asks with TTL + sweeper;
      transactional accept with optimistic itinerary re-verification; Expo push
      behind an interface; `POST /routes`, `POST /requests`,
      `POST /asks/{id}/accept|decline`, `PUT /me/push-token`; background workers
      in `cmd/api`; `scripts/e2e.sh` drives the whole journey over real HTTP.
- [ ] **M5 Trips & social** — not started. Trip endpoints, confirm/complete,
      ratings, chat (REST + ws hub), report/block.
- [ ] **M6 Expo app** — not started.
- [ ] **M7 Deploy readiness** — RAM measured; production compose, backup +
      restore drill and the final numbers outstanding.

## Deviations from spec

All logged in `specs/RISKS.md` under "Open decisions". Summary:

1. Buffered GIST index replaced by a `(route_geom::geography)` GIST index — the
   specced buffered index could never be used by the specced `ST_DWithin` query.
2. Occurrence identity keyed `(template_id, service_date)` instead of
   `(template_id, depart_earliest)` — DST/tzdata safety.
3. Explicit policy for nonexistent/repeated local wall times in recurrence.
4. Accept-path detour re-verification made optimistic (no OSRM call under lock).
5. Outbox documented as at-least-once with lease-based recovery.

Plus, recorded in RISKS.md risk 3: **the region-clipped OSRM extract is now
required, not optional** — whole-Greece serving RSS is 1.15 GB against the
200–500 MB DEPLOYMENT.md assumed, which would leave no headroom on a 2 GB VPS.
The Evros clip is 39 MB for identical in-region routes.

## Adversarial review (Codex, high effort)

Two rounds, both against the real code rather than the plan. Round 1 covered the
accept and outbox paths, round 2 the materializer and insertion search. Fifteen
findings were real and are fixed; each fix landed with a regression test. The
ones worth remembering:

- **Outbox results were unfenced.** A worker whose lease expired mid-delivery
  could clear a newer owner's lease, or mark dead a message the new owner had
  already delivered. Results now carry a per-claim token.
- **MaxAttempts was unenforceable.** Attempts are spent at claim time, but
  dead-lettering only happened on a reported failure, so a message that kept
  killing its worker retried forever. Exhausted messages are now retired at claim.
- **Clock domains were mixed, twice.** `run_after` and the materializer's
  "has it departed" test used SQL `now()` while every comparison used the
  injected clock. They agreed only by coincidence of the fixture dates.
- **The materializer never applied edits.** It only inserted missing rows, so a
  changed departure time never reached an existing occurrence, and a date
  cancelled by a since-lifted exception was blocked forever by its tombstone.
- **Cancellation had no upper bound**, so every occurrence beyond the horizon
  looked unwanted.
- **The insertion search gave up on the first unroutable ordering**, silently
  dropping drivers a different ordering would have served.

Two findings were false positives caused by the excerpt sent for review: the
`bookings_one_live_per_request` partial unique index and the `asks_one_pending`
index already enforce invariants Codex flagged as missing. Verified rather than
assumed before dismissing.

## Notes on method

- Every unit was written test-first; each test was watched failing before the
  implementation existed.
- Key assertions were mutation-tested rather than trusted:
  - dropping the geography index makes the EXPLAIN test fail;
  - removing both the ordering check and the distance floor makes reverse and
    crossing routes match, while removing *either alone* does not — the floor
    subsumes the direction check, since a backwards overlap computes as negative;
  - removing the occurrence row lock does *not* cause overbooking, because the
    `seats_free >= 0` CHECK rejects the second decrement. The lock buys the clean
    error, the constraint buys the invariant;
  - disabling the requeue guard does re-ask a driver for an already-matched
    request.
- The e2e script runs against the real API, real PostGIS and real OSRM. Its
  computed detour (14.3 min) matched an independent OSRM calculation exactly, and
  the default 10-minute budget correctly refused that ride until the driver
  widened it.
- Codex reviewed the plan at high effort before any code was written; the adopted
  and rejected corrections are listed below.

## Plan review (Codex, high effort, 2026-07-29)

Adopted: inject a `Clock` from M0; split `/livez` from `/readyz`; move the outbox
*table* into M1 so external effects are transactional from the start; store
refresh tokens for rotation/revocation; add `blocks` in M1 because matching must
exclude blocked users; snapshot route geometry and pricing onto occurrences so a
later template edit cannot change an existing booking; gate regeneration on a
state predicate (no booking, no active request, no pending ask, not started)
rather than "future unbooked"; keep OSRM calls out of the accept transaction.

Not adopted: pulling a mobile slice into M1/M3 (churn against a still-moving API
contract — the error envelope and auth contract were frozen in M0 instead); a
dead-letter management UI (schema, metrics and a manual replay command suffice
for a pilot).

## Next steps

1. M5: trip endpoints, confirm/complete, ratings, chat (REST + ws hub via
   `github.com/coder/websocket`), report/block.
2. M6: the Expo app per MOBILE.md, pointed at this machine.
3. M7: production compose (without the dev fsync settings), backup + restore
   drill, and the final measured RSS table.
