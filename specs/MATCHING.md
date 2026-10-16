# Matching Algorithm

Two-stage design modeled on BlaBlaCar Zen's published architecture
(PostGIS gross filter → routing-engine fine filter), corrected per Codex review:
the gross stage must be **direction- and order-aware**, and the fine stage must
compute **incremental detour against the driver's current itinerary**.

## Inputs

- A `ride_request` R: origin `Ro`, destination `Rd`, time window, seats.
- Candidate set: `trip_occurrences` O (status scheduled, seats_free ≥ R.seats,
  departure window overlapping R's window) joined to their template geometry.

Matching triggers:
1. New request created → match against existing occurrences.
2. A trip becoming available → match against open requests (via outbox event).
3. Ask expired/declined → advance to next candidate.

Trigger 2 was specified here from the beginning and was **not implemented until
2026-08-11**: the ask flow had three entry points and all three were on the
passenger's side, so an open request that found nobody was never reconsidered.
See "Reverse direction" below for what it means in practice, since "a trip
becoming available" turned out to be seven events rather than one.

## Stage 1 — gross filter (pure SQL, index-backed)

For each candidate occurrence with route line `L` (driver's template geometry):

```sql
WITH cand AS (
  SELECT o.id, t.route_geom, t.route_km, t.route_min, d.notify_radius_km,
         d.detour_budget_min
  FROM trip_occurrences o
  JOIN route_templates t ON t.id = o.template_id
  JOIN driver_profiles d ON d.user_id = t.driver_id
  WHERE o.status = 'scheduled'
    AND t.active                       -- a retired route takes nobody new
    AND o.seats_free >= :seats
    AND o.depart_earliest <= :window_end   -- inclusive: a 06–10 window
    AND o.depart_latest   >= :window_start --   takes a 10:00 departure
    AND NOT d.notifications_muted
)
SELECT id,
  ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, :origin))  AS f_o,
  ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, :dest))    AS f_d
FROM cand
WHERE
  -- proximity, metre-accurate, GIST-backed (3 km superset buffer index;
  -- per-driver radius applied precisely here):
      ST_DWithin(route_geom::geography, :origin::geography, notify_radius_km * 1000)
  AND ST_DWithin(route_geom::geography, :dest::geography,   notify_radius_km * 1000)
  -- direction/order: pickup must come strictly before dropoff along the line:
  AND ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, :origin))
    < ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, :dest))
  -- minimum shared fraction of the route (rejects trivial overlaps/crossings):
  AND (f_d - f_o) * route_km >= GREATEST(1.0, 0.2 * :request_km)
```

Notes:
- `ST_LineLocatePoint` returns the fractional position (0..1) along the line —
  the order check kills reverse-direction and perpendicular-crossing routes that
  buffer overlap alone would admit.
- The shared-fraction floor (≥ 20 % of the passenger's own trip length, min 1 km)
  is a tunable heuristic; revisit with real data.
- Region admissibility was already enforced at request/route creation time.
- One passenger, one trip per hour: an occurrence is skipped when it overlaps
  (departure window plus driving time) another occurrence where the passenger
  holds a confirmed booking or a pending, unexpired ask. Without it two requests
  from one person held every seat on a trip, and a route published twice could
  seat the same person twice at the same hour (QA 2026-10-16).

Output: candidate list with pickup/dropoff snap points, capped at top ~20 by
straight-line quality score.

## Reverse direction — who is waiting for this trip

`match.Waiting` takes one occurrence and returns the open requests it could
answer, longest wait first. It exists so trigger 2 has something to run.

It is **not** a mirror of stage 1. The contract is containment: everything the
gross filter matches, `Waiting` must return, and `Waiting` may return more. The
reverse query being *stricter* fails silently and is indistinguishable from
having no trigger at all, while being looser costs one indexed query. So it
carries the cheap indexable narrowing — status, windows, seats, luggage size,
proximity, direction along the road, prior asks, blocks, self-match — and leaves
the shared-distance floor to stage 1, which stays the authority on whether a
pair really matches.

A wake produces a shortlist of request ids; each goes back through `AskNext`,
which re-runs stage 1 and stage 2. There is one matching engine with two
triggers, not two engines.

"A trip becoming available" is seven events, all enqueuing `wake_waiting` inside
the transaction that caused them:

| Event | Where |
| --- | --- |
| Materialized or retimed | `jobs.Materializer.upsert` |
| Booking cancelled | `bookings` |
| Standing arrangement ended | `standing` |
| Passenger suspended | `moderation` |
| Account closed | `account` |
| Offer declined or lapsed | `asks.requeue` |
| Request with a live ask cancelled | `api.handleCancelRequest` |

The last is a consequence of the ask cap below rather than of a seat physically
returning, and it is what keeps the queue moving on a one-seat trip.

An hourly reconciler (`jobs.Reconciler`) re-queues open, in-window requests
regardless. It is the safety net for the ways an occurrence becomes eligible
that have no event at all — a driver unmuting notifications, widening their
notify radius, coming off a suspension — and for whatever the wake's shortlist
cap left behind.

### Ask admission

A trip is never offered more seats than it has left: the sum of party sizes
across live offers must not exceed `seats_free`. Enforced twice — subtracted in
the gross filter so a saturated trip never reaches OSRM, and rechecked under the
occurrence lock, which is what decides a race.

A pending ask deliberately holds no seat in `seats_free` itself; a driver who
never answers must not strand the trip. The cap is what stops the same seat
being promised to everybody waiting on the corridor.

## Stage 2 — fine filter (OSRM, per candidate)

For each surviving candidate, compute the **incremental detour**:

1. Build the driver's *current* itinerary: origin → [existing accepted
   pickups/dropoffs in stop order] → destination. (`itinerary_version` on the
   occurrence tells us if it changed since candidates were ranked.)
2. Find the best insertion of (pickup Ro', dropoff Rd') into that stop sequence,
   preserving: pickup-before-dropoff for every passenger, seat capacity on every
   leg. With MVP cap of ≤ 2 concurrent passenger parties the insertion search is
   tiny (≤ ~6 position pairs) — brute-force all valid insertions via OSRM
   `/route/v1` calls (one call per insertion candidate, waypoints in order).
3. `detour_min = new_itinerary_min − current_itinerary_min`.
4. Keep candidates where `detour_min ≤ driver.detour_budget_min` (and detour_km
   sane). Pickup/dropoff points snapped via OSRM `/nearest`.

OSRM insertion calls are the expensive step — that's why stage 1 must cut hard
and why candidates are capped. Cache `(occurrence, itinerary_version, Ro, Rd)`
→ detour results.

## Ranking

Score candidates (lower = better), then ask drivers sequentially:

```
score = detour_min
      + 0.5 × walk_km(Ro → pickup_point) × 12        # walking penalty (min-equivalent)
      + 0.5 × walk_km(Rd → dropoff_point) × 12
      − 2 × driver_rating_bonus                      # small nudge, bounded
```

Sequential ask flow, TTL, and transactional accept live in ARCHITECTURE.md.

## Multi-passenger constraints (MVP simplification)

- MVP: an occurrence holds at most **2 concurrent passenger parties**; the
  insertion algorithm above stays trivial. Raising the cap is a config change but
  the insertion search cost grows — revisit post-MVP (this is a top risk).
- Every accepted booking bumps `itinerary_version`; pending asks computed against
  a stale version are re-verified at accept time (recompute detour inside the
  accept transaction; if now over budget, reject gracefully and re-ask).

## Suggested price (computed at ask time, frozen at accept)

```
trip_km   = OSRM distance Ro' → Rd' along driver's route
cost      = fuel_consumption/100 × fuel_price(region) × trip_km × wear_factor
per_pax   = floor(cost / (offered_seats + 1), 0.5€)            # every seat, +driver
if trip_km ≤ free_below_km or pricing_mode != 'formula': per_pax = 0
```

## Performance envelope

- Region-scoped data: thousands of routes, not millions. BlaBlaCar's load tests
  (150 k trips: 0.6–2.5 s gross match with the same index strategy) are two
  orders of magnitude above our target load — headroom is ample.
- Watchpoints: `route_templates_buffer_gist` index actually being used
  (EXPLAIN in tests), OSRM call fan-out per request (cap + cache), matcher
  triggered per event, not by polling scans.

## Future optimizations (explicitly not MVP)

- H3 hex bucketing of route cells for candidate prefiltering (the vault spec's
  idea) — only if gross-match SQL becomes the bottleneck.
- Materialized candidate views refreshed periodically (BlaBlaCar-style) — only if
  per-request matching gets slow.
- Learned typical-routes from movement patterns.
