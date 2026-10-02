# Driving now — a driver opens their seats for the next hour

Status: design, 2026-10-02, reviewed once by Codex (gpt-6.1-sol). Nothing here
is built yet. Once phase 1 lands it replaces the "Driving detection" and
"Live location" lines under *Explicitly deferred* in SPEC.md.

## The case

A driver is about to leave, or has already left, with empty seats. Somebody on
their road wants to go the same way *now*. Today the app cannot put the two
together: a request's window is matched against a schedule's window, never
against when or where the car really is. RISKS.md already records the cost —
a passenger cannot join a driver who is running late, because matching stops
at `depart_earliest`.

The rule that shapes everything below: **the driver never waits.** A pickup is
only offered if the passenger is ready before the car gets there.

## One concept: a drive

"Before I leave" and "while I drive" are the same object with a different
starting point. A **drive** is an occurrence with a real departure and a
matching deadline:

```
planned ──start──▶ active ──arrive / timeout / «Τέλος»──▶ ended
   │ departs_at (≤ 15 min ahead)    departed_at, position
   └─ offers_until = departs_at + 60 min (default), after it: no new offers,
      existing bookings finish normally
```

- `departs_at` (planned, may be in the future) and `departed_at` (actual) are
  separate fields. Pressing the button "in 10 minutes" does not mark departure.
- One planned/active drive per driver; start and end are idempotent.
- A drive on a scheduled occurrence uses that occurrence. A drive with no
  schedule creates an ad-hoc one-off route template + occurrence, so every
  existing join through `route_templates` keeps working.
- Matching looks only at the road still ahead: from the route origin while
  planned, from the car's last fresh position while active.

Entry points, all leading to the same «Φεύγω τώρα» sheet:

| Entry | What happens |
| --- | --- |
| Tap on a scheduled occurrence | Drive on that occurrence. |
| Tap with no schedule | Pick destination (saved route or place) → ad-hoc drive. |
| Push nudge (below) | Opens the sheet for that occurrence. Receipt is never departure. |

## When is a match good

For a request with window `[ready_from, wait_until]` and a pickup time `eta`:

| Case | Condition | Verdict |
| --- | --- | --- |
| Passenger ready, car coming | `ready_from ≤ eta − margin` and `eta ≤ wait_until` | **Match.** Rank by passenger wait `eta − ready_from` (≥ 0), then detour. |
| Passenger not ready yet | `eta − margin < ready_from` | **No.** The driver would wait. |
| Passenger can't wait that long | `eta > wait_until` | **No** in phases 1–2. A "later than you asked" offer needs the passenger to widen their window first, which is a separate feature. |

`margin` covers walking to the pickup point and ETA error: walking time from
the request origin to the snapped pickup point + 3 min. For a passenger whose
window starts in the future, the margin is what makes "ready in 30 minutes,
car in 10" a non-match automatically.

Every offer's deadline is `min(now + TTL, eta − margin − time to stop)`. Each
acceptance step re-checks feasibility; a pickup the car has passed rejects
cleanly.

## Seats: one reservation model

There is one seat pool and one ask state machine. Drives add states to the
existing `asks` flow rather than a parallel one:

```
offered (driver or passenger) ──▶ awaiting_other_side ──▶ booked
        └──────────── expired / declined / cancelled ◀──────┘
```

- Both pending states count in `SeatsSpokenFor`, so the ask admission cap
  (sum of party sizes across live offers ≤ `seats_free`) covers drives too.
- Booking consumes the hold, decrements `seats_free` once and invalidates
  competing offers for that request, under the existing lock order and
  `itinerary_version` check.
- Starting a drive invalidates the occurrence's old 30-minute asks; their
  sweeper must not requeue into the scheduled flow.
- At every reservation and booking, re-check blocks, suspensions, women-only,
  luggage, pause-asks and remaining seats — they can change after an offer is
  sent. The seats in the sheet can only lower what is offered; they never
  overwrite `seats_free` below existing bookings and holds.
- Detour is checked against **all** confirmed stops: two +5′ detours that each
  pass must not exceed the driver's total budget or an earlier passenger's
  promised arrival. OSRM failure creates no booking.

## Who confirms, given the driver may be driving

Nobody judges a request at 80 km/h. So:

- **Planned drive (parked):** the existing flow — driver is asked, with a TTL
  capped by the deadline above. Works today's way, just with a real time.
- **Active drive (moving):** one tap, no reading. The driver says yes
  *before* the passenger walks anywhere; nobody walks to a corner for a car
  that may not stop.

  ```
  passenger: request "now" (already says: ready now)
  matcher:   pickup reachable on foot before the car → offer
  driver:    «Επιβάτης στον δρόμο σου · +3′»  [Πάρ' τον] [Όχι]
  passenger: «Ο Γιώργος περνά από Πλατεία Χ στις 08:12 — ξεκίνα τώρα (4′ με τα πόδια)»
  passenger: «Είμαι εδώ» on arrival → driver's drive screen shows it
  ```

  The walk is inside the margin, so the passenger is there first. No answer
  by the deadline = no; nothing is booked on silence. If the passenger is not
  at the pickup by the pickup time + 2′, the driver goes on. Decided
  2026-10-02: Harold wants the driver to have the last word, at the cost of
  one tap.

### Where they meet: detour is a default plus a per-drive choice

| Setting | Pickup point | Passenger walks |
| --- | --- | --- |
| «Μόνο στον δρόμο μου» (0′) | the point on the driver's route nearest the passenger | up to ~600 m / 8′ |
| «Κάνω παράκαμψη έως +N′» | at or near the passenger | little or nothing |

- The route has a default (set once). The «Φεύγω τώρα» sheet prefills it as
  chips **[0′] [+3′] [+5′] [+10′]**; one tap changes it for this drive only.
  The drive screen keeps the same chips, so "not today" is one tap mid-drive
  too (stops new offers above it; booked pickups stay).
- Each offer shows its own cost («+4′»), so [Όχι] is always the per-case
  "not this time".
- Details beyond that (the other side of the road, a petrol station 100 m
  on) are what the trip chat is for.

### Which road the car really takes

The URL hand-off does not tell us Google's chosen route, and Google does not
expose it to other apps. It does not need to: **the pickup is a waypoint**,
so whatever road Maps picks to avoid traffic, it is forced through the
pickup point. The only thing that can differ is the road *between* stops,
which matters for matching candidates, not for meeting. Matching uses our
OSRM route (already stored per template); from phase 2 the car's real
positions replace the planned line behind it. Asking Google's Routes API for
its route would cost per call and still not be the route the driver's phone
picks.
- **[Πάρ' τον] hands navigation off**, it does not build it. It opens the
  driver's maps app with the pickup as a waypoint and the drive's destination
  as the end:
  - Google Maps (both platforms): `https://www.google.com/maps/dir/?api=1&destination=…&waypoints=…&travelmode=driving&dir_action=navigate`
  - Apple Maps (iOS 18.4+): `https://maps.apple.com/directions?destination=…&waypoint=…&mode=driving`
  - Waze has no waypoints: navigate to the pickup only, and a second
    notification after pickup offers the destination.
  The choice of app is a setting, asked once. The external app tells us
  nothing back, so arrival at the pickup comes from our own position updates
  (phase 2) or the passenger's «Μπήκα».
- Not possible today, checked 2026-10-02 via Grok: answering a notification's
  buttons by voice (Siri Announce and Android Auto read and reply to
  messages only), or a CarPlay entitlement for a carpool matcher. A Siri App
  Intent «πάρε τον επιβάτη» is the voice path, later.
- In-app turn-by-turn is not worth building: free means MapLibre + our own
  OSRM + our own guidance UI (weeks); Mapbox Navigation is free only to 100
  monthly drivers, then $0.30 each + $0.08 a trip.
- No-show: the driver leaves after the pickup time, nothing more; the
  passenger's reliability counts it, as with any missed pickup.

## The nudge (no motion sensors needed)

For a scheduled occurrence whose route has «Ρώτα με όταν ξεκινάω» on (off by
default), shortly before the window opens push the driver once:
«2 άτομα στον δρόμο σου θέλουν να πάνε τώρα. Τους παίρνεις;» — only if

- `seats_free` minus holds > 0 (full car → never),
- at least one request is a match by the table above, computed from the
  earliest departure still possible *now* (a late job must not use a stale
  `depart_earliest`),
- no drive was started and no nudge was sent for this occurrence.

Through the outbox with an idempotency key per occurrence, re-checked at
delivery. Counts only, never names or places. This covers most of the value of
"detect that I'm driving" with no new permission.

## Location and battery

`internal/live` already keeps one overwritten row per person per trip and
decides who sees what. Drives reuse it, with these additions:

- **Freshness for matching is not freshness for display.** `live.Fresh` (10
  min) is for showing a dot. A moving drive takes new offers only while the
  last fix is < 2 min old and accuracy < 200 m; otherwise new offers pause.
  Extrapolation along the route is bounded to that horizon. Out-of-order
  uploads (older `observed_at`) are rejected.
- Unbooked passengers see an ETA and pickup point, never the raw position.
  Passengers still do not see each other.
- Ending a drive revokes access and rejects later uploads, so a delayed PUT
  cannot resurrect a deleted row. Server expiry covers a dead phone.
- No coordinates in request logs or crash reports.

Sampling, phase 2:

| Situation | Settings |
| --- | --- |
| Planned, not moving | nothing sent; ETA = route duration from origin |
| Active, no pickup near | `Balanced`, `distanceInterval` 500 m, deferred 60 s / 500 m, iOS `activityType: AutomotiveNavigation` |
| Pickup within ~3 km | `distanceInterval` 50 m — the "where is he" moment |
| Arrived / route time + 30 min / «Τέλος» | stop, delete row |

Upload the latest point only, never a track. These settings are hints to the
OS, not a delivery cadence; gaps and battery drain get measured on two real
phones before anything is promised in the UI. `pausesUpdatesAutomatically` is
left off: at a red light it can stop updates that matter.

**Expo is enough.** `expo-location` `startLocationUpdatesAsync` +
`expo-task-manager`, config plugin `isIosBackgroundLocationEnabled` and
`isAndroidForegroundServiceEnabled` (Android shows the mandatory ongoing
notification). The drive is always user-started; request When In Use only,
not Always and not Android `ACCESS_BACKGROUND_LOCATION`. Unverified until
tested on a release dev build: that Expo's task path keeps a When-In-Use
session alive in the background on iOS. If it does not, phase 2 falls back to
"keep the drive screen open" and says so on screen, as `LiveCard` does now.

## Detecting "I'm driving" (phase 3, opt-in)

1. **Android Activity Recognition Transition API** (`IN_VEHICLE` enter) in a
   small local Expo Module (Kotlin). Wakes the app even when killed (not after
   force-stop); needs `ACTIVITY_RECOGNITION`; re-register on boot and app
   update. "In a vehicle" is not "driving" — it can be a bus — so on wake the
   phone checks, locally, that it is near the origin of one of the driver's
   routes inside its usual window, then asks `GET /routes/{id}/demand-now`
   (route id only, no position) and shows a local notification only if the
   count is > 0.
2. **iOS Shortcuts tip**: «Όταν ενεργοποιείται η Εστίαση Οδήγησης / συνδέεται
   το CarPlay → άνοιξε `carpool://driving`». User-configured, no permissions.
   Whether it runs without a confirmation prompt is to be tested on a device.
3. iOS has no wake-on-driving. Region monitoring needs Always — skipped.
4. Transistorsoft `react-native-background-geolocation` covers all of it,
   paid licence for Android release builds. Fallback only.

The server never receives a position outside a drive the driver started. Same
rule as MOBILE.md: no passive tracking.

## Phases

1. **Planned drive + nudge** — BUILT 2026-10-02. No location, no new
   permission. `POST /v1/trips/{id}/drive` pins the minute (the window
   becomes it; `scheduled_*` keep the published one), `offers_until` keeps
   the drive open past its minute, ETA-from-origin matching with the walk
   margin and the detour budget on the late side, in gross, `Waiting`, ask
   admission (offer expires at the feasibility deadline), the offers already
   made, and Accept. Driver-ask flow unchanged — no separate reservation
   state was needed, because the ask cap already holds seats. Nudge job,
   re-checked at delivery. App: «Φεύγω» card, pickup time for passengers,
   per-route nudge switch.
   Known approximation: the ETA ignores earlier stops except through the
   detour budget, and after departure it is the planned one until phase 2
   adds the car's position.
2. **Moving drive** — BUILT 2026-10-02. After departure the estimate starts
   from the car's last trusted fix (under 2 min, within 200 m, on the
   route; none → no offers). Offers on a drive carry «Πάρ' τον» / «Όχι»
   (push category `drive_offer`), 2 minutes to answer once moving;
   «Πάρ' τον» accepts and opens Google Maps / Apple Maps / Waze with the
   pickup as a stop. Stop / resume / detour mid-drive, «Τέλος»,
   «Είμαι εδώ». Background location via expo-location + expo-task-manager,
   When In Use only, High accuracy every 150 m or 30 s, newest fix per batch.
   The hand-off lists every stop not yet visited, in road order; a stop is
   visited once a fix within 50 m accuracy came within 80 m of it, recorded
   on the phone as the drive goes.
   Without a schedule: «Φεύγω τώρα» in Publish makes a one-off for the next
   quarter of an hour and opens its departure, where «Φεύγω» is.
   Not built: finer sampling near a pickup. Changing the session's options
   from the background task means starting the location service again from
   the background, which Android 12+ restricts; one High-accuracy mode at
   150 m / 30 s is what runs.
3. **Detection** — BUILT 2026-10-02, opt-in («Κατάλαβε πότε οδηγώ»).
   Android: local Expo module `app/modules/drive-detect` (Activity
   Recognition IN_VEHICLE enter, re-registered on boot/update) shows one
   local notification per departure, only inside the driver's own windows
   (an hour either side), which the app writes on the phone. Any phone with
   the app open: expo-location's motion activity (SDK 57) shows «Φαίνεται
   ότι οδηγείς» on home. iOS: Shortcuts tip opening `carpool://driving`.
   Nothing leaves the phone. Compiled against the Android SDK here; not
   yet run on a device.

## Phase 1 acceptance tests (write first)

Fake clock and router, real PostGIS:

1. Two workers, two passengers, last seat, driver-ask and drive offer racing:
   one booking, no oversell, multi-seat parties included.
2. Accept vs expiry / cancel / drive end: one terminal state, seats released
   once, invalidated old asks are not revived by the sweeper.
3. ETA boundaries: passenger ready 30 min after the car → no offer; ready now,
   car in 10 → offer; deadline expires at the feasibility cutoff.
4. Late driver: an active drive matches after `depart_earliest`; a stale
   scheduled occurrence still does not. Double start and concurrent ad-hoc
   starts are safe.
5. Cumulative detour over budget fails; OSRM timeout books nothing.
6. Block / suspension / pause / lowered seats between offer and accept defeats
   the accept.
7. Retried nudge job sends one push; full car or no matching demand sends none.
