# Progress

Running status log so any future session can resume cold. Updated every work session.

## Current state

**Last updated:** 2026-07-29.
**Blocked on:** Docker is not installed (see "Environment notes"). Everything that
does not need it has been pushed forward; DB work currently runs against a native
PostgreSQL 17 + PostGIS 3.6 instead.

## How to run things

```bash
# unit tests only
make check

# with integration tests (real PostGIS)
export CARPOOL_TEST_DSN="postgres://carpool@localhost:5433/postgres?sslmode=disable"
make check
```

Integration tests skip themselves when `CARPOOL_TEST_DSN` is unset. Each test
creates and drops its own database, so they are safe to run in parallel and
against any server.

Starting the interim native stack (until Docker exists):

```bash
export PATH="/home/linuxbrew/.linuxbrew/opt/postgresql@17/bin:$PATH"
PGDIR=/tmp/.../scratchpad/pgdata   # any directory
pg_ctl -D "$PGDIR" -o "-p 5433" -l "$PGDIR/pg.log" start
osrm-routed --algorithm mld --port 5000 osrm-data/evros-buffered.osrm
```

## Environment notes (this machine — mini PC)

- Go 1.23.3 installed; module pinned to `go 1.25.0` (pgx v5.10 needs it), so Go's
  auto-toolchain fetches 1.25.x transparently.
- `staticcheck` 2026.1 installed to `$(go env GOPATH)/bin` — the Homebrew one on
  PATH is 2024.1.1 and cannot parse Go 1.25, so the Makefile calls the GOPATH
  binary explicitly.
- Homebrew (rootless) provides `goose`, `osmium`, `osrm-backend`,
  `postgresql@16`, `postgresql@17`.
- **Docker is still not installed** and `sudo` needs a password. Harold chose to
  install it; the command is in the session. Until then:
  - **PostGIS**: brew's PostGIS 3.6 ships modules for PG 17/18 only, *not* PG 16,
    so the interim test target is **PG 17 + PostGIS 3.6** on port 5433, not the
    spec'd PG 16 + PostGIS 3.4. Nothing used so far is version-sensitive
    (`ST_DWithin`, `ST_LineLocatePoint`, `ST_ClosestPoint`, GIST on geography),
    but the suite must be re-run against `postgis/postgis:16-3.4` before M7.
  - **OSRM**: running natively from brew rather than as a container. Same binary
    and algorithm (MLD, car profile), so routing results are unaffected.
- `docker-compose.dev.yml` is written and ready; it has never been executed.

## Milestones

- [ ] **M0 Skeleton** — mostly done.
  - [x] Go module, `internal/...` layout per ARCHITECTURE.md
  - [x] `make check` (vet + staticcheck + test), all green
  - [x] env-only config loader; injectable `Clock`
  - [x] chi router, `/v1/livez` + `/v1/readyz` + `/v1/health`, JSON error envelope
  - [x] goose migration runner, embedded, advisory-locked, concurrency-tested
  - [x] integration harness (`internal/store/testdb`), DSN from env
  - [x] `docker-compose.dev.yml`, `.env.example`
  - [ ] `cmd/api/main.go` with graceful shutdown
- [ ] **M1 Schema + auth** — schema and tokens done, flow outstanding.
  - [x] full DATA-MODEL.md schema as one migration, applies and rolls back clean
  - [x] access JWTs (HS256 pinned) + opaque, hashed, rotatable refresh tokens
  - [x] Evros region GeoJSON fixture (OSM relation 939217, ODbL)
  - [ ] OTP request/verify flow, SMS behind an interface, dev console OTP
  - [ ] `/me`, region seeding from the fixture, rate limiting
- [ ] **M2 Routes & occurrences** — pure logic done, persistence outstanding.
  - [x] DST-safe wall-time resolver (skipped/repeated local times)
  - [x] schedule → occurrence planner (half-open calendar horizon, exceptions,
        effective range, one-off, deterministic ordering)
  - [x] OSRM client (`/route`, `/nearest`) with NoRoute/NoSegment sentinels
  - [ ] route template + schedule CRUD, materializer persistence, regenerate-on-edit
- [ ] **M3 Matching** — stage 1 done.
  - [x] stage-1 gross SQL: direction-aware, shared-distance floor, block-aware
  - [x] geometry cases: same-direction accepted; reverse, crossing, sub-floor
        overlap, out-of-radius, full, non-scheduled, wrong-window, muted rejected
  - [x] EXPLAIN test asserting the geography GIST index is used
  - [x] suggested-contribution formula (integer cents)
  - [ ] stage-2 OSRM insertion detour, 2-party cap, ranking
- [ ] **M4 Ask/booking lifecycle** — not started.
- [ ] **M5 Trips & social** — not started.
- [ ] **M6 Expo app** — not started.
- [ ] **M7 Deploy readiness** — RAM measured, rest not started.
  - [x] **OSRM memory measured** — see the table in RISKS.md risk 3.

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

## Notes on method

- Every unit was written test-first; each test was watched failing before the
  implementation existed.
- The two highest-risk assertions were mutation-tested rather than trusted:
  dropping the geography index makes the EXPLAIN test fail, and removing both the
  ordering check and the distance floor makes reverse and crossing routes match.
  Removing *either alone* does not — the floor subsumes the direction check,
  because a backwards overlap computes as a negative distance.
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

1. `cmd/api/main.go` + graceful shutdown, closing out M0.
2. M1 OTP flow and region seeding from the checked-in fixture.
3. M2 persistence: template/schedule CRUD and the materializer reconciliation
   transaction.
4. M3 stage 2: OSRM insertion detour and ranking.
5. Once Docker exists: re-run the whole suite against `postgis/postgis:16-3.4`
   and verify the e2e scenario under `docker compose -f docker-compose.dev.yml up`.
