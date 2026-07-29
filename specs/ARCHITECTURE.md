# Architecture

## Overview

```
┌─────────────┐   HTTPS/JSON  ┌──────────────────────────────┐
│  Expo app    │◄────────────►│  api (Go, single binary)      │
│  iOS/Android │   WSS (fg)   │  ├ REST API (chi)             │
└─────┬───────┘               │  ├ WebSocket hub (foreground) │
      │ Expo Push             │  ├ Matcher worker             │
      ▼                       │  ├ Outbox dispatcher          │
┌─────────────┐               │  └ Occurrence materializer    │
│ Expo Push    │◄─────────────┤        │            │
│ service      │  HTTP        └────────┼────────────┼─────────┘
└─────────────┘                        │            │ HTTP
                              ┌────────▼───┐  ┌─────▼─────┐
                              │ PostgreSQL │  │  OSRM     │
                              │ + PostGIS  │  │ (sidecar) │
                              └────────────┘  └───────────┘
```

One Go binary contains everything: HTTP API, WebSocket hub, and background workers
(goroutines). One Postgres. One OSRM container. Nothing else.

## Go binary layout

```
cmd/api/main.go
internal/
  api/          # chi routes, handlers, middleware (auth, rate limit)
  auth/         # phone OTP, JWT sessions (access+refresh)
  domain/       # entities + business rules, no infra imports
  store/        # pgx repositories, migrations (goose or tern)
  geo/          # PostGIS helpers, polyline codec, OSRM client
  match/        # 2-stage matching (see MATCHING.md)
  outbox/       # outbox table dispatcher → push/ws
  push/         # Expo Push HTTP client (tickets, receipts, token pruning)
  ws/           # websocket hub (github.com/coder/websocket)
  jobs/         # occurrence materializer, ask-timeout sweeper, cleanup
```

- Framework: **chi** (stdlib-compatible, minimal). pgx v5 pool. sqlc optional for
  query type-safety.
- Config via env vars only (12-factor). Structured logging with slog.

## Event & notification design (Codex-corrected)

**Rule: state changes and notifications are decoupled through an outbox table.**
LISTEN/NOTIFY alone loses messages when no listener is connected, so:

1. Any transaction that needs a side effect (push, ws message, next-driver ask)
   inserts a row into `outbox` **in the same transaction** as the state change.
2. A dispatcher goroutine claims rows with
   `SELECT ... FOR UPDATE SKIP LOCKED LIMIT n`, executes the effect, marks done
   (or schedules retry with backoff; `attempts` counter, dead-letter after N).
3. `NOTIFY outbox_wake` after commit merely wakes the dispatcher early; polling
   every few seconds is the fallback. Restart-safe, at-least-once.
4. All effects carry an **idempotency key** (e.g. `ask:{match_id}:{driver_id}`) —
   push may be delivered twice; the app and the server dedupe by key.

## Realtime strategy

- **Push notifications (Expo Push) are the primary channel** — the only thing that
  reliably reaches a backgrounded/killed app. Payload carries type + ids, never
  full state.
- **WebSocket only while app is foregrounded**: chat messages, live match-status
  updates, (later) live location. On reconnect or push-open, the client
  **reconciles by fetching state via REST** — the socket is an optimization, never
  the source of truth.
- No message broker. The ws hub is in-process; horizontal scaling is explicitly out
  of scope (single-VPS design). If it ever scales: NATS is the earmarked upgrade,
  not Kafka.

## Driver ask flow (sequential, not broadcast)

```
open_request matched → candidates ranked (see MATCHING.md)
  → ask top candidate (outbox → push, idempotent)
  → driver accepts within TTL (default 30 min, config):
       tx: verify seats ≥ requested, decrement, create booking,
           mark request matched, outbox: notify passenger
  → declines or TTL expires (sweeper job):
       mark ask expired, ask next candidate
  → candidates exhausted: request stays open, re-evaluated when new
    occurrences appear (event-driven via outbox from occurrence creation)
```

Transactional seat locking: `SELECT ... FOR UPDATE` on the occurrence row; unique
constraint `(occurrence_id, passenger_id)` prevents double booking; both drivers
racing to accept the same request is impossible because an ask is exclusive
(one active ask per request at a time).

## OSRM integration

- OSRM runs as a Docker sidecar (car profile, MLD algorithm), regional extract.
- Go client uses `/route/v1/driving` (detour computation, polylines) and
  `/nearest` (snap pickup points to the road network).
- All OSRM calls are server-side; the app never talks to OSRM directly.
- Route polylines stored as PostGIS geometries at publish time; detours computed
  on demand during fine matching, cached on the match row.
- Data version: `osrm_data_version` stamped on stored routes; on map data update,
  a background job re-snaps stale routes.

## Auth

- Phone OTP (SMS) → JWT access (15 min) + refresh (90 days) tokens.
- SMS provider behind an interface; start with a cheap provider or
  dev-mode console OTP; provider choice is deployment config.
- Rate-limit OTP per phone + per IP.

## Live location (post-MVP, designed now)

- Driver taps "start ride" → app starts a foreground-service location task
  (expo-location + task-manager), posts positions every 5–10 s to
  `POST /trips/{id}/positions` (HTTP, batched; no ws dependency).
- Server keeps only the latest position per active trip (`trip_live_position`
  upsert row, TTL cleanup) and fans out over ws to that trip's passengers.
- Client-side smoothing (lerp / dead reckoning) as in the vault's tracking spec.
- Positions are ephemeral: never stored historically. (Privacy by design.)

## API surface (v1 sketch)

```
POST /auth/otp/request | /auth/otp/verify | /auth/refresh
GET  /me · PATCH /me · PUT /me/driver-profile · PUT /me/push-token
GET  /regions
POST /routes                    # driver route template (+schedule)
GET  /routes/mine · PATCH /routes/{id} · DELETE /routes/{id}
GET  /occurrences?from&to&near  # passenger search
POST /requests                  # open request or targeted join
GET  /requests/mine · DELETE /requests/{id}
POST /asks/{id}/accept | /asks/{id}/decline
GET  /trips/upcoming · GET /trips/{id}
POST /trips/{id}/confirm · POST /trips/{id}/rate
GET/POST /trips/{id}/messages   # chat (ws for live, REST for history)
POST /users/{id}/report | /block
GET  /health · GET /metrics     # Prometheus, bind internal only
```

All endpoints JSON, versioned under `/v1`, OpenAPI spec generated from code
(or maintained by hand in `api/openapi.yaml` — decide at implementation).
