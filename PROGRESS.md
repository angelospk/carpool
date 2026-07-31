# Progress

Running status log so any future session can resume cold. Updated every work session.

## Current state

**Last updated:** 2026-07-29.
**All milestones complete.** Backend and app are built, reviewed and verified.

Definition of Done, as measured on this machine:

| Check | Result |
| --- | --- |
| `make check` (vet + staticcheck + tests) | green |
| Concurrency packages under `-race` | green |
| App `tsc --noEmit` + `jest` | clean, 50 tests |
| Android bundle exports | 2.9 MB Hermes |
| e2e scenario from a fresh database | passes (12 steps) |
| Backup + restore drill | passes |
| RAM measured | ≈ 330 MiB whole stack |

The e2e covers the whole DoD path: publish a recurring route (snapped by real
OSRM) → passenger request → driver ask → accept → trip visible to both sides
with the plate revealed → chat → both confirm → mutual ratings.

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
- [x] **M5 Trips & social** — upcoming trips (driving and riding merged), trip
      card with the plate revealed only post-booking, per-side confirm →
      complete, ratings, chat with a total `(created_at, id)` cursor, an
      in-process ws hub, block and report.
- [x] **M6 Expo app (redesigned 2026-07-29)** — full expo-router tree: auth flow, tabs
      (home/search/publish/profile), trip card + chat, the ask screen, request
      status, user actions. API client with epoch-guarded secure-store
      credentials and a single-flight refresh. Greek/English i18n at key parity.
      25 tests; tsc clean; the Android bundle exports.
      Redesigned onto a real design system (`src/design/tokens.ts`): warm paper
      canvas, floating cards, pill tab bar, pine/amber palette. The signature
      element is the **route glyph** — the real OSRM geometry drawn as a small
      vector mark, so a trip is recognisable by the shape of its road without map
      tiles or an API key. Seats read as pips; times are pinned to Athens.
      Glyph geometry, Athens formatting and pin parsing are tested modules
      (50 tests). One new dependency: `react-native-svg`.
- [x] **M7 Deploy readiness** — production compose (durability defaults kept,
      only Caddy published), 24.6 MB distroless image, Caddyfile, backup script
      and a restore drill that compares row counts, spatial indexes and a
      representative PostGIS query. Whole-stack RAM measured.

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

## Measured RAM (2026-07-29, containers under load)

| Container | Measured | DEPLOYMENT.md estimate |
| --- | --- | --- |
| `api` (distroless, 24.6 MB image) | **7.2 MiB** | ~100 MB |
| `db` (postgis:16-3.4, shared_buffers 256MB) | **261 MiB** | ~400 MB |
| `osrm` (Evros + 30 km clip, MLD) | **32 MiB** | 200–500 MB |
| `caddy` (upstream figure, not yet run) | ~30 MB | ~30 MB |
| **total** | **≈ 330 MiB** | ~1 GB |

The stack fits a 1 GB VPS provided the OSRM extract stays region-clipped.
Postgres dominates, and `shared_buffers` is what sets its floor.

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

## The queue, reviewed to a standstill (2026-07-31)

`internal/dispatch` had no tests at all — the one package where an unexercised
branch means a notification silently never arrives. It has them now, and writing
them cost three rounds of adversarial review with Codex, the second of which
rejected my own fix from the first.

What was actually broken:

- A push fans out to every device a user owns. Joining the send errors meant one
  device's permanent rejection carried `ErrPermanent` into the aggregate, so the
  dispatcher dead-lettered the message and a device that had merely timed out
  never got it.
- `run_after` still carried `DEFAULT now()`. The bug fixed in 2644142 was the Go
  side of exactly that; the schema kept the trap open one layer down.
- A blank idempotency key collided with every other blank one, and
  `ON CONFLICT DO NOTHING` reported success while dropping the effect.
- A failed result write abandoned the rest of the claimed batch, though claiming
  had already charged every row an attempt.
- A dead push token that could not be retired was logged and forgotten, leaving
  the message marked delivered and the token live forever.

And what I got wrong in the middle of fixing it: I bounded the handler with a
`context` deadline of one lease. The lease is taken from the injected clock and
a context deadline is real elapsed time, so the "fix" compared two clock domains
— the fault this project has now been bitten by four times — and, being
cooperative, enforced nothing. Withdrawn; the limit is written up as RISKS 10
instead. A fix that restates the bug is worse than the gap it fills.

RISKS 11 and 12 record what was deliberately not built and why each is
unreachable today.

## The plaza that was two plazas (2026-07-31)

CodeRabbit, run over the whole tolls change, found the defect the Codex rounds
had missed because they never looked at the data: twelve plazas carried their
two carriageways in *different* groups. The harvester bucketed by
`round(lat, 3)`, and records fifteen metres apart fell either side of a bucket
boundary. `group` exists precisely to stop a route being charged for the barrier
on the other carriageway, and for a seventh of the dataset it was doing nothing
— the heading gate was carrying that defence alone.

Regrouped by distance, in the harvester and in the shipped file, with prices and
bearings untouched. The 22 real routes still total to the same cent, which is
the evidence the fix is safe rather than merely different, and a dataset test
now asserts the invariant so a refresh cannot quietly undo it.

Worth noting what this says about review: three rounds of high-effort adversarial
review of the *code* found real bugs and never once questioned the *data* the
code was reasoning over.

## Deliberately not built

Parked with a reason, not forgotten:

- **Map pickers.** Origin and destination are typed as `lon, lat Label` rather
  than dropped on a map. There is no geocoder in the pilot, and the pin UI does
  not change the shape the screen produces, so it can land later without
  touching anything else.
- **Push registration on the device.** The server side is complete — tokens,
  pruning on `DeviceNotRegistered`, delivery through the outbox — but the app
  does not yet ask for permission and register. That needs a dev build on real
  hardware, which is the next physical step.
- **Everything in SPEC.md's "explicitly deferred" list**: live location sharing,
  driving detection, learned routes, waypoints, web app, payments.

## Tolls (added 2026-07-29)

`internal/tolls` computes the motorway barriers on a route and splits them by
the stretch each rider actually travels. 93 stations, harvested from
vriskoapostasi.gr by routing every Greek motorway corridor in both directions
(`scripts/harvest-tolls.py`) and shipped in the binary — the source cannot answer
"which barriers was this passenger in the car for", which is the whole point.

Matching is a gate line, not a proximity test: 25 stations are co-located pairs
distinguished only by direction. A route must cross a segment laid across the
carriageway, in an agreeing direction, within 10 m of the barrier — a threshold
measured from 162 true traversals (worst 3.2 m) against the A8 running 22 m past
the Attiki Odos plaza it does not pay.

Verified against 22 real routes covering every operator and both directions of
four corridors: identical stations, identical totals, to the cent. Try it:

```bash
go build -o bin/tolls ./cmd/tolls
./bin/tolls -from 40.8457,25.8740 -to 41.1214,25.4058 \
            -rider "Κομοτηνή:41.0,25.6:41.1214,25.4058"
```

Limits are written up in RISKS.md items 8 and 9.

## The clock lesson, three times over

Mixing SQL `now()` with the injected clock has now caused a bug three times, and
the third one shipped for two days before a test run happened to land on the
wrong side of noon:

- `outbox.Enqueue` defaulted `run_after` to the database's clock while the
  dispatcher judged due-ness by the injected one. A test holding the clock at
  noon passed all morning and failed all evening. In production the same gap
  opens whenever the API host and the database disagree about the time, and the
  symptom is a push that is silently never sent.
- The API test helper `departed()` had the mirror image: it moved a trip into the
  past by `now()` while the handler judged departure by the injected clock.
- `scripts/e2e.sh` derived the weekday from the local calendar and the request
  window from the UTC one, so between local midnight and UTC midnight it
  published a route for Saturday and asked for a ride on Friday.

The rule the codebase now holds to: **a value a decision is made against comes
from the clock that decision uses.** `now()` is fine for `updated_at` audit
columns, which nothing compares. A guard test holds the clock six years from
real time so the two can never agree by coincidence again.

## Known limitations

- A websocket subscription is authorized once, at connect. A passenger whose
  booking is cancelled mid-connection keeps receiving that trip's chat until
  they disconnect. Sockets are short-lived and chat is not sensitive, so this is
  accepted for the pilot rather than fixed with periodic revalidation.
- Refresh retries are idempotent per attempt id, but a retry only works while
  its successor is unused: a client that somehow used the new token and *then*
  retried the old request gets a clean 401 rather than a resend. That is the
  intended fail-closed direction.
- Family revocation is serialised by an advisory lock rather than by a family
  row. Correct, but it means every rotation takes one extra lock; a
  `refresh_token_families` table would be the tidier shape if sessions gain
  features (device names, per-session logout in the UI).
- The party cap counts parties per occurrence rather than concurrently aboard.
  Stricter than MATCHING.md's wording; it can only refuse rides a
  concurrency-aware cap would allow, never admit ones it would not.

## If you are picking this up cold

1. `docker compose -f docker-compose.dev.yml up -d`
2. `export CARPOOL_TEST_DSN=...` then `make check`
3. `scripts/e2e.sh` against a running API to see the whole product work
4. Read `specs/RISKS.md` "Open decisions" for every place the implementation
   knowingly diverges from the specs, and why
