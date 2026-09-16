# Product Spec

## Concept

Local carpooling. Both ends of a trip must be inside Greece, which is as far as the
map, the toll data and the routing go; there is no narrower geographic rule. The
first sentence of this spec used to require at least one endpoint inside the
caller's home region, and that rule is gone — see "Region model" below for what
replaced it and why. Drivers declare routes they already drive (commutes, errands); passengers ask
for rides; the system finds drivers whose route passes near the passenger's origin
and destination with an acceptable detour, and *asks the driver* via notification.
The driver always explicitly accepts. No dispatching, no obligation.

## Personas

- **Driver**: has a car, drives recurring routes (work, village↔town) or one-off
  trips. Wants near-zero friction: publish once, get asked only when relevant,
  one-tap accept/decline.
- **Passenger**: needs a ride along a corridor. Wants to post a request and get
  matched, or browse upcoming published trips.
- Every account can act as both (single user type with a driver profile attached).

## Core flows

### 1. Driver publishes a route
1. Picks origin + destination (map pin or search), app shows the OSRM route polyline.
2. Chooses: one-off (date + departure window) or recurring (weekdays + departure
   window, e.g. Mon-Fri 07:30–08:00).
3. Sets seats (default 3), detour budget (default 10 min, slider 0–30), and
   cost-sharing preference (see Pricing).
4. Publishes. Backend snaps the route via OSRM, stores geometry, materializes
   occurrences (see DATA-MODEL).

### 2. Passenger requests a trip
1. Picks origin + destination + time window (e.g. "tomorrow 07:00–09:00") + seats.
2. Immediately sees existing matching occurrences (search), can send a join request
   to a specific trip, **or** posts an open request.
3. Open requests are matched continuously against new/existing occurrences; when a
   match appears, the *driver* gets a push: "Passenger X wants a ride from A to B on
   your Thursday route, +6 min detour. Accept?"
4. Driver accepts → passenger notified, seat reserved transactionally. Driver
   declines/ignores → next candidate driver is asked (no auction, sequential asks
   with timeout).

### 3. Ride day
1. Both see the trip card: pickup point (snapped to driver route), time, contact.
2. In-app chat (minimal, per-trip thread) for coordination.
3. Optional (post-MVP): driver taps "start ride" → live location shared with the
   trip's passengers only, foreground-service tracking, auto-ends at destination.
4. After the ride: both sides confirm it happened; mutual rating (1–5 + optional tag).

## Matching preferences (driver settings)

- Notification radius: how far off-route a pickup may be (km, default 2).
- Detour budget: max added minutes per trip (default 10).
- Quiet hours / mute notifications toggle.
- Auto-decline threshold: ignore requests needing more than N seats.
- Free-below-X-km: rides under X km cost nothing (default 5, configurable 0–20).

## Pricing (suggestion, not payment)

No in-app payments — cash or free, at the driver's choice. The app *suggests* a fair
contribution:

```
cost = fuel_consumption_l_per_100km × fuel_price_eur × (trip_km / 100)
suggested_per_passenger = floor(cost × wear_factor / (offered_seats + 1), 0.50€)
if trip_km ≤ driver.free_below_km: suggested = 0
```

The divisor is every seat the driver published, plus the driver, and it does not
change as the car fills. Dividing by the passengers actually aboard looks fairer
and is not: each price freezes at accept, so a car filling one seat at a time
collects cost/2 + cost/3 + cost/4, which is more than the trip cost. Flooring
rather than rounding to nearest closes the other end of the same leak. A driver
who fills one seat of four therefore recovers a fifth of their fuel, which is the
right direction to be wrong in — see `docs/research/sources.md` on άρθρο 18
ν. 1903/1990.

Three boundaries the formula above does not show:

- **No seats on offer** suggests no fuel share. A trip with `offered_seats ≤ 0`
  has nobody to divide across, and dividing by the driver alone would quote half
  the fuel to a passenger who cannot board.
- **`free_below_km` waives the fuel share only.** A toll is cash the driver
  handed over on that same hop, so it survives the waiver and is added after it.
- **The toll share is not rounded.** It is added on top of the floored fuel
  figure, because unlike the fuel estimate it is an exact amount. A suggestion
  that is toll-only is therefore not a multiple of 0.50 €.
- Any non-formula pricing mode suggests nothing at all, tolls included.

- `fuel_consumption`: from driver's car profile (manual entry; car model picker
  post-MVP).
- `fuel_price`: region-level constant, admin-updatable (auto-fetch post-MVP).
- `wear_factor`: default 1.0; driver may set up to 1.5 to cover wear.
- Driver can also declare a route "free contribution" (pay-what-you-want) or "free".
- Shown to both sides before accept; never enforced. This keeps us clearly in
  cost-sharing territory (not commercial transport) — see RISKS.md for the legal
  note.

## Trust & safety

- Sign-up: phone number verification (SMS OTP) — the anchor identity for a local
  community. Email optional.
- Profiles: name, photo, car (model/color/plate — plate visible only to matched
  passengers after accept).
- Mutual ratings after completed rides; report/block user.
- Women-only preference toggle for passengers (matches only women drivers) —
  common carpooling feature, decide in review whether MVP or post-MVP.
- Community bootstrap option: invite codes per region (configurable open/closed
  registration).

## Region model

- An instance hosts one or more regions (polygon per region, seeded from admin
  GeoJSON, e.g. prefecture boundaries from OSM). The shipped fixture is
  `fixtures/regions/greece.geojson`: all 75 περιφερειακές ενότητες, so every
  coordinate in the country resolves to one.
- Rule: a trip or request is admissible if **both** ends resolve to a region,
  which in practice means both ends are in Greece. Refused otherwise, with
  `outside_country`.
- The home region is not an authorisation boundary. It once was — origin OR
  destination had to be inside it — and the rule was removed because it stopped
  describing anything real: it refused Θεσσαλονίκη → Αθήνα to somebody living in
  Ξάνθη, which is exactly the journey a carpool is for, and it turned a profile
  field into a permission. `internal/api/request_handlers.go` carries the
  reasoning at the site.
- What the region is still for: naming the fuel price to use, and telling
  somebody where they are. Not gating.
- Users pick a home region at sign-up; can change it.
- The corridor this matters for is Αλεξανδρούπολη ↔ Κομοτηνή, two prefectures
  and the spine of a ΔΠΘ pilot (NEXT.md §2). It is asserted end to end in
  `internal/api/corridor_test.go` rather than left working by the absence of a
  check, because a well-meant re-addition of a home-region guard would close it
  again silently.

## Explicitly deferred (post-MVP)

- Driving detection (bluetooth car connect / activity recognition) to auto-prompt
  drivers "are you driving your Thursday route now?" — fragile on iOS, needs
  background permission review. The vault idea stays on the roadmap.
- Live location sharing during ride (design is in ARCHITECTURE.md, build later).
- Learned "typical routes" from movement patterns — replaced in MVP by explicit
  recurring route publishing (simpler, privacy-friendlier).
- Intermediate waypoints / multi-leg routes; partial-route pricing per segment.
- Web app, in-app payments, car model database, automatic fuel price feed.

## Success criteria (MVP)

- A driver can publish a recurring route in < 90 seconds.
- A passenger request along a served corridor produces a driver ask within 1 min.
- Match quality: suggested pickup within driver's radius, detour within budget,
  computed against real OSRM routes (not straight lines).
- Whole stack (API + Postgres + OSRM) idles under 2 GB RAM on one VPS.
