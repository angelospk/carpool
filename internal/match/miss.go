package match

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OffCorridorKm is how far a road may pass from the passenger and still be
// worth telling them about: «περνά 8 χλμ από το σημείο σου».
const OffCorridorKm = 10.0

// Miss is why a car that passes was not a candidate at all. Reason is one of
// full, party_cap, luggage and off_corridor; for off_corridor NeedKm is how far
// the road passes from the farther of the passenger's two points and LimitKm the
// distance its driver stops at.
type Miss struct {
	Reason          string
	NeedKm, LimitKm float64
}

// missSQL looks at the departures the gross filter would consider if it did
// not ask about room or about the driver's radius, and says which of those
// two turned each one away. Same window, people, direction, overlap floor
// and DriveReady as grossSQL; the parameters are grossArgs.
var missSQL = `
WITH near AS (
	SELECT ST_DWithin(o.route_geom::geography, $1::geography, d.notify_radius_km * 1000)
	   AND ST_DWithin(o.route_geom::geography, $2::geography, d.notify_radius_km * 1000) AS in_radius,
	       o.seats_free - ` + SeatsSpokenFor("o.id", "$13") + ` >= $5 AS seats_ok,
	       o.luggage >= $9::luggage_size AND o.luggage_capacity - COALESCE((
	           SELECT sum(b.luggage_count) FROM bookings b
	            WHERE b.occurrence_id = o.id AND b.status IN ('confirmed', 'completed')), 0) >= $10 AS luggage_ok,
	       (SELECT count(*) FROM bookings b
	         WHERE b.occurrence_id = o.id AND b.status IN ('confirmed', 'completed')) AS parties,
	       GREATEST(ST_Distance(o.route_geom::geography, $1::geography),
	                ST_Distance(o.route_geom::geography, $2::geography)) / 1000 AS dist_km,
	       d.notify_radius_km::float8 AS radius_km
	FROM trip_occurrences o
	JOIN route_templates  t ON t.id = o.template_id
	JOIN driver_profiles  d ON d.user_id = t.driver_id
	JOIN users du ON du.id = t.driver_id AND du.status = 'active'
	` + DriveCar("o", "$13") + `
	WHERE o.status = 'scheduled'
	  AND t.active
	  AND (o.drive_set_at IS NOT NULL OR (o.depart_earliest <= $4 AND o.depart_latest >= $3))
	  AND ` + DriveOpen("o", "$13") + `
	  -- A drive the passenger cannot be at the road for, or has stopped
	  -- waiting by, is not a car that passes them.
	  AND (o.drive_set_at IS NULL OR ` + DriveReady(driveFromAt, "o.route_min",
	"(ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, $1)) - "+driveFromF+")",
	"ST_Distance(o.route_geom::geography, $1::geography)", "GREATEST($3, $13)", "$4", DriveLateBy("o.")) + `)
	  -- Not a car the passenger is on, or one overlapping a trip they hold:
	  -- the matcher would not have offered it with room to spare.
	  AND ($7::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM bookings bk WHERE bk.occurrence_id = o.id AND bk.passenger_id = $7::uuid))
	  AND ($7::uuid IS NULL OR NOT ` + PassengerHolds("o", "$7::uuid", "$13", "NULL") + `)
	  -- The overlap is worth an offer, as in the gross filter.
	  AND (ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, $2))
	     - ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, $1))) * o.route_km >= GREATEST(1.0, 0.2 * $6)
	  AND NOT d.notifications_muted
	  AND ST_DWithin(o.route_geom::geography, $1::geography, ` + fmt.Sprint(OffCorridorKm*1000) + `)
	  AND ST_DWithin(o.route_geom::geography, $2::geography, ` + fmt.Sprint(OffCorridorKm*1000) + `)
	  AND ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, $1))
	    < ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, $2))
	  AND ($7::uuid IS NULL OR t.driver_id <> $7::uuid)
	  AND ($7::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM blocks b
	        WHERE (b.blocker_id = t.driver_id AND b.blocked_id = $7::uuid)
	           OR (b.blocker_id = $7::uuid    AND b.blocked_id = t.driver_id)))
	  AND ($7::uuid IS NULL OR du.gender = 'female' OR NOT EXISTS (
	        SELECT 1 FROM users pu WHERE pu.id = $7::uuid AND pu.women_only))
	  AND (t.group_id IS NULL OR ($7::uuid IS NOT NULL AND EXISTS (
	        SELECT 1 FROM group_members gm
	        WHERE gm.group_id = t.group_id AND gm.user_id = $7::uuid AND gm.left_at IS NULL)))
	  AND ($11::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM asks a WHERE a.request_id = $11::uuid AND a.occurrence_id = o.id))
	  AND ($12::uuid IS NULL OR o.id = $12::uuid)
	  -- $8 is the gross filter's limit; unused here.
	  AND $8::int IS NOT NULL
)
SELECT coalesce(bool_or(in_radius AND seats_ok AND luggage_ok AND parties >= ` + fmt.Sprint(MaxConcurrentParties) + `), false),
       coalesce(bool_or(in_radius AND NOT seats_ok), false),
       coalesce(bool_or(in_radius AND seats_ok AND NOT luggage_ok), false),
       min(dist_km) FILTER (WHERE NOT in_radius),
       (array_agg(radius_km ORDER BY dist_km) FILTER (WHERE NOT in_radius))[1]
FROM near`

// WhyMissed says why no departure was a candidate for q although one passes,
// or nil when none passes at all. The nearest miss wins: a car that only
// lacks a party slot over one that is full, over one whose boot is, over one
// whose road passes too far away.
func WhyMissed(ctx context.Context, pool *pgxpool.Pool, q GrossQuery) (*Miss, error) {
	var partyCap, full, luggage bool
	var offKm, radius *float64
	if err := pool.QueryRow(ctx, missSQL, grossArgs(q)...).Scan(&partyCap, &full, &luggage, &offKm, &radius); err != nil {
		return nil, fmt.Errorf("match: why nobody was asked: %w", err)
	}
	switch {
	case partyCap:
		return &Miss{Reason: "party_cap"}, nil
	case full:
		return &Miss{Reason: "full"}, nil
	case luggage:
		return &Miss{Reason: "luggage"}, nil
	case offKm != nil && radius != nil:
		return &Miss{Reason: "off_corridor", NeedKm: *offKm, LimitKm: *radius}, nil
	}
	return nil, nil
}
