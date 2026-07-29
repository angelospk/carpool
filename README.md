# Carpool — local carpooling, open source

Working name: **carpool**. A local-first carpooling app scoped to a region
(νομός): drivers publish recurring or one-off routes, passengers request trips,
the backend matches by route overlap plus a detour budget and asks drivers via
push. Cost sharing is a suggestion (fuel formula), never a payment.

**Non-goals:** not ride-hailing, no in-app payments, no national scale.

## Status

The backend MVP and the Expo app are built. `make check` is green, the app's
test suite is green, and the full scenario runs end to end against real
PostGIS and real OSRM:

```
publish a recurring route → passenger request → driver ask → accept
→ trip visible to both sides → chat → both confirm → mutual ratings
```

See [PROGRESS.md](PROGRESS.md) for milestone state and
[specs/RISKS.md](specs/RISKS.md) for measured numbers and open decisions.

## Running it

```bash
# backing services: PostGIS 16 + OSRM
docker compose -f docker-compose.dev.yml up -d

# the API, from source
cp .env.example .env          # then edit DATABASE_URL / JWT_SECRET
go run ./cmd/api

# the whole test suite, including integration tests against real PostGIS
export CARPOOL_TEST_DSN="postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable"
make check

# the end-to-end scenario against a running stack
scripts/e2e.sh

# the app
cd app && npm install --legacy-peer-deps && npm test
npx expo start
```

The OSRM extract is not in git. Build it once:

```bash
scripts/build-osrm.sh osrm-data/evros-buffered.osm.pbf
```

Preprocessing peaks around 5.7 GB for whole Greece, so it runs off the server.

## Stack

| Layer | Choice | Why |
| --- | --- | --- |
| Mobile | React Native **Expo** (expo-router) | Must be an app; fastest solo iteration |
| Backend | **Go**, single binary (chi, pgx, coder/websocket) | Tiny footprint — measured at 7 MiB resident |
| Datastore | **PostgreSQL + PostGIS** (only datastore) | Geo matching, outbox and everything else; no Redis/H3/Kafka at this scale |
| Routing | Self-hosted **OSRM**, region-clipped extract | Lightest and fastest for car-only; BSD |
| Push | Expo Push API (direct HTTP client in Go) | No third-party ambiguity |
| Hosting | One small VPS | Measured ≈ 330 MiB for the whole stack |

## Layout

```
cmd/api/          the single binary: API, ws hub and background workers
internal/
  api/            chi routes, handlers, middleware
  asks/           the sequential driver-ask lifecycle
  auth/           phone OTP, JWTs, refresh-token rotation
  clock/          injected time source
  dispatch/       routes outbox messages to their effects
  domain/         entities and rules; no infrastructure imports
  geo/            OSRM client and geographic primitives
  jobs/           the occurrence materializer
  match/          two-stage matching: PostGIS gross filter, OSRM fine filter
  outbox/         transactional side effects with a leased dispatcher
  push/           Expo Push client
  store/          pgx repositories and migrations
  ws/             in-process websocket hub
app/              the Expo app (expo-router)
scripts/          OSRM build, e2e scenario, backup, restore drill
fixtures/         region boundaries (OSM, ODbL)
```

## Spec files

- [specs/SPEC.md](specs/SPEC.md) — product spec: personas, flows, features, settings
- [specs/ARCHITECTURE.md](specs/ARCHITECTURE.md) — services, realtime and event design
- [specs/DATA-MODEL.md](specs/DATA-MODEL.md) — schema
- [specs/MATCHING.md](specs/MATCHING.md) — the two-stage matching algorithm
- [specs/MOBILE.md](specs/MOBILE.md) — Expo app
- [specs/DEPLOYMENT.md](specs/DEPLOYMENT.md) — VPS layout, OSRM pipeline, backups
- [specs/RISKS.md](specs/RISKS.md) — risks, measurements and open decisions

Where the implementation diverged from a spec, the reason is recorded under
"Open decisions" in RISKS.md rather than changing the spec silently.

## Provenance

Distilled from Harold's vault notes, web research (BlaBlaCar Zen matching
architecture, routing engine comparisons, the Expo maps ecosystem), and Codex
plan and code reviews whose corrections are folded in.
