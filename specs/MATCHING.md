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
2. New occurrence materialized → match against open requests (via outbox event).
3. Ask expired/declined → advance to next candidate.

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
    AND o.seats_free >= :seats
    AND o.depart_earliest < :window_end
    AND o.depart_latest  > :window_start
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

Output: candidate list with pickup/dropoff snap points, capped at top ~20 by
straight-line quality score.

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
