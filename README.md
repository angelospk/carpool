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

One command brings the whole thing up and prints where to open it:

```bash
scripts/dev-lan.sh
```

It starts PostGIS and OSRM, rebuilds the Go binary, runs the API in DEV_MODE
bound to the LAN rather than to localhost, exports the Expo web bundle and
serves it. Every step is safe to repeat, so after a reboot you re-run it instead
of working out which of the four pieces died.

Then, once, so there is something to click on:

```bash
scripts/seed-demo.sh
```

That signs in three people, gives the two drivers cars through the real API, and
publishes two routes:

| Number | Who | What they have |
| --- | --- | --- |
| `+306900000101` | Γιώργος | driver, Toyota Yaris, Αλεξανδρούπολη → Κομοτηνή, 3 seats |
| `+306900000102` | Μαρία | driver, Corolla Verso, Αλεξανδρούπολη → Ορεστιάδα, 2 seats |
| `+306900000103` | Νίκος | passenger, no car |

Sign in as any of them. In DEV_MODE the one-time code is not texted anywhere:
the API returns it and the app fills the field in, so there is nothing to look
up. Type the number in full including `+30`. The field starts at `+30` and the
button stays dead until the whole E.164 number is there.

One thing will look odd while you click around: every sign-in asks for your name
again, because the app collects the name and region before it calls verify
rather than after.

Pickup points are typed as town names now, in Greek or Latin letters, with or
without accents. The list covers the Evros prefecture and stops at its boundary,
which is a real limit rather than a rounding: Κομοτηνή is in Ροδόπη. RISKS 15.

To stop everything:

```bash
pkill -f bin/api; pkill -f serve-web.py
docker compose -f docker-compose.dev.yml down
```

### The pieces on their own

```bash
# backing services: PostGIS 16 + OSRM
docker compose -f docker-compose.dev.yml up -d

# the API, from source
cp .env.example .env          # then edit DATABASE_URL / JWT_SECRET
go run ./cmd/api

# the whole test suite, including integration tests against real PostGIS
export CARPOOL_TEST_DSN="postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable"
make check

# `go test` caches passing results, so `make check` can print green from a run
# made when the database was up. After starting the stack, or whenever the
# result matters, clear the cache first.
go clean -testcache && make check

# the end-to-end scenario. It drives the real HTTP API, so it needs an API
# process of its own: its own database, OTP codes on the console instead of by
# SMS, the regions seeded, and the log where the script reads the codes from.
psql "postgres://carpool:carpool@localhost:5433/postgres?sslmode=disable" \
     -c "DROP DATABASE IF EXISTS smoke" -c "CREATE DATABASE smoke"
DATABASE_URL="postgres://carpool:carpool@localhost:5433/smoke?sslmode=disable" \
JWT_SECRET="e2e-secret-that-is-long-enough-32b" \
DEV_MODE=true HTTP_ADDR=":8099" REGION_FIXTURE=fixtures/regions/evros.geojson \
  go run ./cmd/api > /tmp/api-e2e.log 2>&1 &
scripts/e2e.sh

# the app
cd app && npm install --legacy-peer-deps && npm run check
npx expo start
```

The web bundle has the API URL compiled into it, read from `app/.env`, because
`expo export` ignores `EXPO_PUBLIC_*` in the environment. Always export with
`--clear`. Metro will happily rebuild in under a second while shipping
yesterday's URL, and the failure then shows up on the device with nothing in
either log to explain it. `scripts/dev-lan.sh` handles both.

The OSRM extract is not in git. Build it once:

```bash
scripts/build-osrm.sh osrm-data/evros-buffered.osm.pbf
```

Preprocessing peaks around 5.7 GB for whole Greece, so it runs off the server.

## Recent work

### Cost sharing was quietly turning into profit

The per-passenger price was divided by the number of seats sold rather than the
number of people in the car, so a full car collected more than the trip cost:
1600 cents on a 1260-cent trip with four seats. Rounding went up as well. Both
are fixed, the divisor now counts the driver, and a test asserts the total
collected can never reach the cost. [docs/pricing.md](docs/pricing.md) has the
whole of it, and is the file to read before touching anything that produces a
number a passenger pays.

### The car says what it drinks, not the driver

Fuel consumption used to be a flat 7.0 l/100km for everybody, which overcharged
a small petrol car by around a quarter. A driver now names their car and the app
works the figure out three ways, in descending order of how much each one knows:
a 2761-entry dataset harvested from the EEA, the CO2 figure in field V.7 of the
άδεια κυκλοφορίας, or a floor derived from the dataset's own tenth percentile.
Which route was used is stored per driver, so profiles built under the old flat
default can be found and reassessed.

### The app runs in a browser

`expo export --platform web` plus a static server means the flows can be clicked
through from any machine on the network, without building for a phone. Two
things had to change for it. The API needed CORS, which is bound to DEV_MODE and
echoes the requesting origin. And credential storage needed a web path, because
`expo-secure-store` has no web implementation and threw before the app rendered
anything at all. On web the tokens go to localStorage, which is written down in
the file rather than left to be discovered.

### Signing in no longer needs a log file

In DEV_MODE the API returns the one-time code in the response and the app
prefills it. There is deliberately no fixed test code like `0000`, because a
fixed code would exercise a path production never runs. Outside DEV_MODE the
response stays empty and a test asserts it, since otherwise knowing a phone
number would be enough to take an account.

### Type a town name instead of a coordinate

Both ends of a route were typed as `25.8743, 40.8476 Αλεξανδρούπολη`, which is
not something a person does twice. 153 settlements harvested from OSM against
the Evros relation rather than a bounding box, since a box around this
prefecture reaches into Turkey and Bulgaria.

The matching is the substance. Greek romanisation has an ISO standard, a UN
standard, the spelling on the road signs and the spelling people actually use,
and they disagree about the same town, so each place is indexed under several
spellings rather than one. Accents fold away, dialytika does not, and digraphs
are romanised as digraphs: a letter-for-letter table writes μπ as `mp` and ευ as
`eu`, which is how nobody spells them.

### A person can change what the app knows about them

Name, photo and the two social handles, and the number the account signs
in with. The last is a change of credential rather than of profile, so it
runs as one: a code to the new number, a challenge quoted back rather than
a number, and every refresh token revoked on the swap. What it cannot do
is prove the session belongs to the account's owner, because there is no
second factor to ask; what bounds that is the access token's fifteen
minutes, which is written down rather than assumed.

Photos are re-encoded rather than stored as they arrive. That is not about
file size: it removes the EXIF block that routinely carries the
coordinates of the person's house, and anything appended after the image
data. They live in Postgres because there is no object store and none is
being added for a hundred users. What that costs is written in the
migration that creates the table.

Social links are handles now, not URLs. Asking for a URL asks somebody to
produce something they do not have to hand, and the one row in the
development database proved it by holding facebook.com/notifications/.

### A driver can see their routes and call off a departure

Both were missing. A driver could publish a route and never look at it
again, and no endpoint could cancel one morning's departure at all.

They are separate acts on purpose. Retiring a route stops it being offered
from now on and leaves arranged departures alone; cancelling a departure
tells the people expecting a seat in it. Doing the second silently when a
driver asked for the first is how somebody finds out by standing at a
corner.

The trip screen also stopped asking everybody the same two questions. A
driver looking at tomorrow's empty car was asked whether the trip had
happened and invited to rate a passenger who did not exist.

### Half the pilot's main corridor cannot be published

The `route_templates_geom_simple` constraint rejects a route whose road geometry
crosses itself, and the matcher depends on that being true. Measured:
Αλεξανδρούπολη → Ορεστιάδα passes, Ορεστιάδα → Αλεξανδρούπολη does not. Φέρες →
Αλεξανδρούπολη passes, Διδυμότειχο → Αλεξανδρούπολη does not. So a driver can
publish the morning commute but not the evening return. It is not fixed, because
both ways out (simplifying the geometry before storing it, or ending the
matcher's dependence on `ST_LineLocatePoint`) are decisions rather than patches.
RISKS.md has the numbers.

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
scripts/          dev-lan and demo seed, OSRM build, e2e, backup, restore drill
fixtures/         region boundaries (OSM, ODbL)
```

## Docs

- [docs/pricing.md](docs/pricing.md) — how a price is arrived at, and why
- [docs/research/sources.md](docs/research/sources.md) — the outside sources, with checked claims kept apart from reported ones
- [docs/legal/](docs/legal/) — terms, privacy notice, and what is still unbuilt

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
