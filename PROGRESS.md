# Progress

Running status log so any future session can resume cold. Updated every work session.

## Current state

**Last updated:** 2026-08-11.
**All milestones complete.** Backend and app are built, reviewed and verified.

Since the milestones closed, the work has been what testing the app on a real
device turned up. In order:

| What | Why it mattered |
| --- | --- |
| Cost sharing collected more than the trip cost | A full car made a profit. See docs/pricing.md |
| Fuel consumption is worked out from the car | A flat 7.0 l/100km overcharged small petrol cars by a quarter |
| The app runs in a browser | Testing flows without a build on a phone |
| A web build could only talk to the network it was exported for | The bundle carried a LAN address, unroutable from Tailscale |
| Towns are typed by name, in either script | Pickup points were typed as coordinates |
| Profile: name, photo, social handles, phone number | None of it could be changed |
| A driver can list and retire routes, and call off a departure | No endpoint could do any of it |
| Declaring a car makes somebody a driver | The garage wrote `vehicles`, publishing checked `driver_profiles`, and no screen wrote the second: a driver added their car and was told to add their car details |
| The device registers for the notifications the server sends | `api.registerPushToken` existed and was called from nowhere |
| A trip appearing looks for who was waiting for it | The matcher only ran from the passenger's side, so a request that found nobody was never reconsidered |

Definition of Done, as measured on this machine:

| Check | Result |
| --- | --- |
| `make check` (vet + staticcheck + tests) | green |
| Concurrency packages under `-race` | green |
| App `tsc --noEmit` + `jest` | clean, 96 tests |
| Android bundle exports | 2.9 MB Hermes |
| e2e scenario from a fresh database | passes (14 steps) |
| Backup + restore drill | passes |
| RAM measured | ≈ 330 MiB whole stack |

The e2e covers the whole DoD path: publish a recurring route (snapped by real
OSRM) → passenger request → driver ask → accept → trip visible to both sides
with the plate revealed → chat → both confirm → mutual ratings. Steps 12 and 13
then cover the reverse direction: somebody asks for a journey nobody drives and
is told nothing, the driver publishes that route, and she is offered it without
anything touching her request.

Step 2 was seeding `driver_profiles` with an INSERT and had gone stale against
the garage work — publishing now refuses without a car, so the script had been
failing at step 3 since vehicles landed. It declares the car through the real
endpoint instead, which is also what makes the suggested contribution real
rather than a fixed consumption figure.

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
- [x] **M3b Matching in both directions** — `match.Waiting` asks a trip who has
      been waiting for it, and a `wake_waiting` outbox message runs it whenever
      a trip becomes available: published, materialized, a seat returned by a
      cancellation, suspension or account closure, or an offer declined or
      lapsed. Each woken request goes back through `AskNext`, so there is one
      matching engine with two triggers rather than two engines. An hourly
      reconciler covers the changes that have no event (a driver unmuting,
      widening their radius, coming off a suspension) and whatever the wake's
      shortlist left behind. A trip is never offered more seats than it has
      left, enforced in the filter and again under the occurrence lock.
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

## Pricing that nobody can tilt (planned, 2026-08-02)

Two inputs to the suggested contribution are currently taken on trust, and both
are the driver's word.

**Consumption.** The driver types a litres-per-100km figure into their profile.
It defaults to 7.0 and nothing checks it. Since the price is linear in it,
typing 14 doubles what every passenger is asked for. The plan is to remove the
field from the driver entirely: they pick a make and model, and the consumption
comes from a table shipped with the app, the same way toll prices already do.

That table is the work. Options, roughly in order of how much they cost:

- A checked-in dataset of common models in Greece with a combined figure each,
  harvested once and reviewed, exactly like `internal/tolls/data/greece.json`.
  The same review gate applies: a refresh that moves a figure by more than a
  threshold stops and asks a human.
- Falling back to a class average (small petrol, medium diesel, van) when the
  model is not in the table, and saying on screen that it is an estimate.
- Letting a driver appeal a figure to the operator rather than edit it. The
  point is that the number is not theirs to choose.

Note what this buys and what it does not: it stops a driver inflating the
suggestion, but it cannot stop two people agreeing on any figure they like in
the car. The app never moves money. What it protects is the number it puts on
the screen, which is the thing people will treat as fair.

**Fuel price.** `regions.fuel_price_eur` is seeded per region and never moves.
The source to use is not a scraper: the Παρατηρητήριο Τιμών Υγρών Καυσίμων is
published as an open dataset on data.gov.gr
(`parathrhthrio-timwn-ygrwn-kaysimwn`), served by a JSON API at
`api/v1/query/<dataset>` behind a free token. Structured data with a licence
beats parsing the ministry's HTML, and it removes the question of whether
automated fetching is allowed. Confirm the per-prefecture and per-fuel breakdown
against the live response before building. Three things that will
bite whoever builds it:

- Diesel and petrol are different numbers and the car decides which applies, so
  the driver profile has to record fuel type before the price can be picked.
- A scraper that fails silently leaves a stale price that still looks live. It
  needs a fetched-at timestamp, and the price needs to be visibly an estimate
  once it is older than a few days.
- Prices move for everyone at once. A quote frozen at booking must keep the
  price it was quoted at, which the schema already does for bookings.

Neither of these is scheduled. They are written down because the design
decision, that the driver does not get to set what a kilometre costs, should be
made before the code and not after the first argument about a price.

## What GoRide's own terms were worth reading for (2026-08-05)

Two Greek services publish terms: BlaBlaCar and GoRide. Both describe the same
shape — the platform *suggests* a figure and the driver decides at or under it,
never above cost. That is what migration 00024 moved us to, and it turns out we
divide more strictly than GoRide does: they split by "τον συνολικό αριθμό των
επιβαινόντων", we split by offered seats plus the driver, so an empty seat still
counts in our denominator. A driver earns less here than they would there, by
construction.

Reading them properly surfaced three things we did not have. All three are now
built.

**No age gate at all.** Theirs says "οι χρήστες της υπηρεσίας πρέπει να είναι
ενήλικοι - 18 ετών". Ours asked nothing. Now asked once, at signup, because age
only goes one way: eighteen on the day you join is eighteen for ever after, and
there is no later moment at which the answer changes. Their allowance for a
minor travelling with an adult family member is deliberately not copied —
nothing in a booking can tell the app that the adult in the next seat is
somebody's parent, and a rule the app cannot check is a rule it should not claim
to have.

**Luggage counted, not just sized.** Their booking request captures "τον αριθμό
και το μέγεθος των αποσκευών". We had only the size, so three passengers with a
suitcase each all fitted in a boot with room for one. Fixed as uniform slots
with the space derived from the bookings holding it, rather than as a counter —
seats already have six places that give them back, and a seventh counter through
all of them is the leak that gets missed.

**Contact details in free text.** They forbid links, emails and external profile
names in listings. For us it is safety before rules: a bio is read by strangers,
so a number in one is a public number. Filtered now, with the obfuscated forms
covered and with ordinary Greek prose as the larger test set — see RISKS.md for
why it is a speed bump rather than a wall.

## Retention, eight tenths of the way (2026-08-08)

The privacy policy names ten retention periods for database rows. Four were
enforced; ten now are minus two, and the two that are left are decisions rather
than work — see RISKS.md.

The interesting part was not writing the rules. It was that the obvious
implementation of "delete trips after 24 months" would have destroyed three
other categories through foreign keys nobody was looking at: ratings cascaded
through bookings, messages cascaded off the occurrence despite having their own
clock, and reports would have survived with a null where their evidence was.
Codex found the third of those; the first two came out of reading the FK graph
before writing any SQL, which is a habit worth keeping.

Ratings needed the cascade broken (00028). Messages and reports did not — both
have bounded lifetimes of their own, so the trip simply waits for them and goes
at a later run. Waiting only works because the wait is bounded; it would have
been the wrong answer for ratings, which never expire while the account lives.

Push tokens needed a `last_used_at` that did not exist, so the policy's "24
months from last use" had been unenforceable rather than unenforced. It is
written on delivery now, monotonically.

## The QA pass over account status and silent success (2026-08-11)

A deliberate hunt rather than a feature: a QA plan reviewed by Codex, worked
through track by track, with each fix held by a test that fails when only that
fix is removed. Five bugs, in rough order of how badly they would have gone.

**A suspension never reached the matcher.** A suspended passenger's open request
stayed in the queue, so the next trip published on that corridor woke it and a
driver was given a real seat to somebody who could not open the app. Worse the
other way round: the cascade cancels a suspended driver's departures and not the
template behind them, so the materializer — whose job is to make what a schedule
implies, and which quite correctly revives a cancelled departure nobody has
booked — put every one of them back. Measured at nine departures in a single
pass. Fixed at four distances: the cascade withdraws the live work, the
materializer skips a template whose driver is not active, both filters read the
account, and the two paths that create work hold both people's rows FOR SHARE
first so a suspension mid-transaction is decided rather than raced.

**Cancelling a ride request always said yes.** One UPDATE whose row count was
discarded, so somebody else's request, a nonexistent one and one a driver had
just accepted all answered 204. The screen it is called from is the one somebody
sits on *while* drivers are being asked, so the third is the ordinary case; the
app read 204 as done and went home, telling them they were not going while a
seat was held in their name.

**Nothing ever expired a request.** Two filters read the window rather than the
status and each explained, in a comment, that a sweeper would come along. There
was none, and `expired` was a status the schema allowed and no code could reach.

**Erasure did not reach the garage.** `driver_profiles` was scrubbed and
`vehicles` was not, so a registration plate survived a deletion — the sharpest
identifier here, since a plate is on the outside of the car. The export had the
mirror gap: no car, no driver settings, no blocks, no complaints filed.

**And one I introduced, caught by CodeRabbit:** withdrawing a pending ask left
the *other* passenger's request at 'asking' with nothing left to answer it, and
nothing looks at a request in that state again.

Two things worth keeping from how it went. The first set of suspension tests
stayed green with both matching filters deleted, because the cheaper checks
above them caught it first — the mutation run is what found that, and the tests
moved down to the layer they were actually about. And the ownership sweep in
`internal/api/ownership_test.go` is meant to be walked next to `router.go`: a
path parameter with no line in that table is a gap.

## The QA pass over time and cancellation (2026-08-12)

A second hunt, same method: a plan reviewed by Codex before any code, each fix
held by a test proven to fail when only that fix is reverted, then CodeRabbit
over the result. Four bugs, and the last one is the one that would have hurt.

**The matcher offered seats in cars that had left.** The departure was compared
against the *passenger's* window, and a window is written once, when somebody
asks for a ride. Drivers are asked one at a time with half an hour each, so a
request naming a morning is still being matched hours into that morning against
a window that has not moved, and every departure inside it that had already gone
stayed a candidate. Both directions: the forward filter offered them, and a seat
handed back ten minutes after a car left woke everybody waiting on the corridor.
Three layers now: neither filter offers a departure due to leave, an offer is
given a deadline that ends at the departure rather than half an hour later, and
the accept path refuses one that reaches it anyway.

**Blocking somebody did not reach the offer already on their phone.** The
matcher will not introduce a blocked pair, and that was the whole of the rule.
But an ask is written at one moment and answered at another, and blocking writes
a row and nothing else: the driver taps accept half an hour later and the two of
them have a seat in the same car, each other's meeting point and a chat channel.
Now read where both people are already held, and refused as "no longer pending"
so that nobody learns from the refusal that a block exists.

**A called-off departure told its passengers and then stopped.** Their request
stayed at 'matched', and nothing reads that state looking for work, so the
request that already says where they are going and when was never shown to
another driver. All three paths that call off a departure had it — the driver
doing it themselves, a suspension, an account closing.

**And the closure path never cancelled the bookings at all.** It cancelled the
departures and left the seats in them 'confirmed', with no push: the passenger's
app went on showing a trip with a meeting point and a time for a driver who no
longer exists, and the first they would have known is standing at the kerb.

Two from the reviews, both real. CodeRabbit found that the queued ask was keyed
on the request, so somebody stranded twice produced the same key twice and the
outbox dropped the second as a duplicate — that passenger then waited for an ask
nobody was going to send. And an ask could be born already expired, because the
filter checks the departure without a lock and seconds earlier.

Codex's diff review raised two "high" findings that turned out not to hold — the
clock *is* read after the locks, and `bookings_one_live_per_request` already
guarantees the cancelled booking was the one that owned the match — and two that
did: the closure path inferred its work from "cancelled and in the future"
rather than from the statement that cancelled them, and these reopenings add a
lock order worth writing down. Both handled; the second is in RISKS.md as
bounded rather than closed.

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

## Before notifications work on a phone

The server has queued pushes since M4 and the app now registers a token on
every launch while signed in (`src/push/`). One thing is still missing and it is
not code: `app.json` has no `extra.eas.projectId`, and `getExpoPushTokenAsync`
has nowhere to fetch a token from without one. It throws
`ERR_NOTIFICATIONS_NO_EXPERIENCE_ID`, which is caught and logged as a plain
sentence rather than a code, because the person reading that log is the person
who has to go and create the EAS project.

Nothing else about the app is affected: registration fails quietly and every
other screen works. It sits with the MapTiler key as a thing the owner supplies.

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
- `match.Waiting` is a superset of `Gross` rather than its mirror: it carries
  the cheap indexable narrowing and leaves the shared-distance floor to the
  forward filter. So a wake can shortlist a request the forward filter then
  rejects, costing one indexed query. The containment property — everything
  `Gross` matches, `Waiting` returns — is tested rather than assumed, because
  the opposite error is silent and looks exactly like the bug this fixed.
- A wake wakes at most ten requests. The rest keep their place (the order is by
  how long each has waited) and are reached by the next change to that trip or
  by the hourly reconciler, so a very busy corridor answers some people a wake
  later than it could.
- The reverse query could not use `ride_requests`' GIST indexes, because those
  are on plain geometry and the proximity test casts to geography. Measured at
  5,000 open requests: 11.5 ms, sequential scan, 4,984 rows discarded. Migration
  00030 adds the expression indexes on the cast, partial on `status = 'open'`;
  the same query is 5.6 ms and discards none. A plan test asserts the index is
  still reached, because the failure is a latency graph nobody can explain
  rather than a broken test.

## If you are picking this up cold

1. `docker compose -f docker-compose.dev.yml up -d`
2. `export CARPOOL_TEST_DSN=...` then `make check`
3. `scripts/e2e.sh` against a running API to see the whole product work
4. Read `specs/RISKS.md` "Open decisions" for every place the implementation
   knowingly diverges from the specs, and why
5. Read `specs/NEXT.md` before starting a feature. The next constraint is a pilot
   community rather than code, and that file says which one and why
