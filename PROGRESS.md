# Progress

Running status log so any future session can resume cold. Updated every work session.

## Current state

**Milestone in flight:** M0 (skeleton).
**Last updated:** 2026-07-29.

## Environment notes (this machine — mini PC)

- Go 1.23.3 installed; module pinned to `go 1.25.0` (pgx v5.10 requires it), Go's
  auto-toolchain fetches 1.25.12 transparently. `GOTOOLCHAIN=auto`.
- `staticcheck` 2026.1 (v0.7.0) installed to `$(go env GOPATH)/bin` — the Homebrew
  one on PATH is 2024.1.1 and cannot parse Go 1.25, so the Makefile calls the
  GOPATH binary explicitly.
- Homebrew (rootless, `/home/linuxbrew/.linuxbrew`) provides `goose`, `osmium`,
  `postgresql@16`, `osrm-backend` — no root needed.
- **PostGIS via brew is 3.6.4 and ships modules for PG 17/18 only, not PG 16.** So
  the native brew stack cannot reproduce the spec'd PG16+PostGIS3.4 pair. DB work
  therefore targets Docker (`postgis/postgis:16-3.4`) — Harold's choice.
- Docker was not installed; Harold is installing it
  (`docker.io docker-compose-v2 docker-buildx` + `setfacl` on the socket so
  already-running shells get access without a re-login).
- Greece OSM extract (`greece-latest.osm.pbf`) downloading in the background to
  `osrm-data/` — needed for M2 OSRM validation and M7 RSS measurement.

## Milestones

- [ ] **M0 Skeleton** — module layout, compose, goose runner, `make check`, health.
- [ ] **M1 Schema + auth** — migrations, phone OTP, JWT, `/me`, region seeding.
- [ ] **M2 Routes & occurrences** — templates + schedules CRUD, OSRM client, materializer.
- [ ] **M3 Matching** — gross SQL, fine detour, ranking, suggested price.
- [ ] **M4 Ask/booking lifecycle** — outbox dispatcher, sequential asks, accept tx, push.
- [ ] **M5 Trips & social** — trips, confirm/complete, ratings, chat, report/block.
- [ ] **M6 Expo app** — expo-router tree, auth, publish, search, ask screen, trip + chat.
- [ ] **M7 Deploy readiness** — prod compose, backups, measured RSS numbers.

## Deviations from spec

All logged in `specs/RISKS.md` under "Open decisions". Summary:

1. Buffered GIST index replaced by a `(route_geom::geography)` GIST index — the
   specced buffered index can never be used by the specced `ST_DWithin` query.
2. Occurrence identity keyed `(template_id, service_date)` instead of
   `(template_id, depart_earliest)` — DST/tzdata safety.
3. Explicit policy for nonexistent/repeated local wall times in recurrence.
4. Accept-path detour re-verification made optimistic (no OSRM call under lock).
5. Outbox documented as at-least-once with lease-based recovery, not exactly-once.

## Plan review

Codex reviewed the implementation plan (high effort, 2026-07-29) before any code was
written. Corrections adopted: inject a `Clock` from M0; split `/livez` and `/readyz`;
move the outbox table (not the dispatcher) into M1 so external effects are
transactional from the start; store refresh tokens for rotation/revocation; add
`blocks` in M1 because matching must exclude blocked users; occurrences snapshot
route geometry and pricing inputs so later template edits cannot change the meaning
of an existing booking; regeneration gated on a *state* predicate (no booking, no
active request, no pending ask, not started) rather than "future unbooked".

Deliberately not adopted: pulling a mobile slice into M1/M3 (churn against a still
moving API contract — the error envelope and auth contract are instead frozen in M0
so the M6 integration is not a surprise); a dead-letter management UI (schema +
metrics + a manual replay command is enough for a pilot).

## Next steps

1. Finish M0 skeleton under TDD (config, clock, health endpoints, migration runner).
2. Wire the integration harness once Docker is available; DSN comes from env so the
   same suite runs against testcontainers or a compose db.
3. Build the OSRM extract once the download lands, and take a first RSS reading.
