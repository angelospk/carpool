package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/match"
)

// SupplyFloor is the fewest distinct drivers for which supply says anything.
// The answer is a yes at or above it and "unknown" below, never a number: in a
// region of villages a road with one car on it is one person's commute, and an
// exact count moves by one when one driver is taken out of it — which is what
// a caller who can block somebody, or join a group, could arrange on purpose.
//
// A suppression rule, not an anonymity guarantee. Somebody who knows the area
// and asks every day can still see a road cross the floor.
const SupplyFloor = DemandFloor

// Supply says whether at least SupplyFloor different drivers have a departure
// between from and to, going from origin towards destination, with a seat
// this passenger could be asked about.
//
// The matcher's rules decide who counts — seats already spoken for by a live
// ask, a departure the passenger is already on, a paused or inactive driver,
// women drivers only, group routes, the passenger's own routes, a shared
// stretch of at least a kilometre — with one exception on purpose: a block the
// passenger made does not take a driver out. A block made by the driver does.
// The passenger controls the first, and a count they can move by blocking
// somebody tells them whether that somebody drives this road.
//
// A possibility, not a promise: the detour and the seat under a lock are only
// decided once somebody asks.
func Supply(
	ctx context.Context, pool *pgxpool.Pool, passengerID uuid.UUID,
	originLon, originLat, destLon, destLat float64, from, to, now time.Time,
) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `
		WITH p AS (
			SELECT ST_SetSRID(ST_MakePoint($2, $3), 4326) AS o,
			       ST_SetSRID(ST_MakePoint($4, $5), 4326) AS d
		)
		SELECT count(DISTINCT t.driver_id)
		FROM trip_occurrences o
		JOIN route_templates t  ON t.id = o.template_id
		JOIN driver_profiles dp ON dp.user_id = t.driver_id
		JOIN users du           ON du.id = t.driver_id AND du.status = 'active'
		CROSS JOIN p
		WHERE o.status = 'scheduled'
		  AND o.seats_free - `+match.SeatsSpokenFor("o.id", "$8")+` > 0
		  AND NOT EXISTS (SELECT 1 FROM bookings bk
		                  WHERE bk.occurrence_id = o.id AND bk.passenger_id = $1)
		  AND o.depart_earliest >= $6 AND o.depart_earliest < $7
		  AND o.depart_earliest > $8
		  AND NOT dp.notifications_muted
		  AND t.driver_id <> $1
		  AND NOT EXISTS (SELECT 1 FROM blocks b
		                  WHERE b.blocker_id = t.driver_id AND b.blocked_id = $1)
		  AND (du.gender = 'female' OR NOT EXISTS (
		        SELECT 1 FROM users pu WHERE pu.id = $1 AND pu.women_only))
		  AND (t.group_id IS NULL OR EXISTS (
		        SELECT 1 FROM group_members gm
		        WHERE gm.group_id = t.group_id AND gm.user_id = $1 AND gm.left_at IS NULL))
		  AND ST_DWithin(o.route_geom::geography, p.o::geography, dp.notify_radius_km * 1000)
		  AND ST_DWithin(o.route_geom::geography, p.d::geography, dp.notify_radius_km * 1000)
		  AND (ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, p.d))
		     - ST_LineLocatePoint(o.route_geom, ST_ClosestPoint(o.route_geom, p.o))) * o.route_km >= 1.0`,
		passengerID, originLon, originLat, destLon, destLat, from, to, now).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: counting supply: %w", err)
	}
	return n >= SupplyFloor, nil
}
