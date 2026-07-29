// Package match implements the two-stage matching described in MATCHING.md:
// an index-backed PostGIS gross filter, then a fine detour check against OSRM.
package match

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxCandidates caps how many candidates leave the gross stage. Every survivor
// costs OSRM calls in the fine stage, so this bound is what keeps fan-out sane.
const maxCandidates = 20

// Point is a WGS84 coordinate.
type Point struct {
	Lon float64
	Lat float64
}

// GrossQuery is a ride request as the gross filter sees it.
type GrossQuery struct {
	Origin      Point
	Destination Point
	WindowStart time.Time
	WindowEnd   time.Time
	Seats       int
	// RequestKm is the passenger's own trip length, used for the shared-distance
	// floor.
	RequestKm float64
	// PassengerID, when set, excludes occurrences whose driver has blocked the
	// passenger or is blocked by them.
	PassengerID uuid.UUID
}

// Candidate is an occurrence that survived the gross filter.
type Candidate struct {
	OccurrenceID uuid.UUID
	DriverID     uuid.UUID
	// FractionOrigin and FractionDest are positions along the driver's route in
	// [0,1]; the filter guarantees FractionOrigin < FractionDest.
	FractionOrigin float64
	FractionDest   float64
	SharedKm       float64
	RouteKm        float64
	RouteMin       int
	DetourBudget   int
	SeatsFree      int
}

// grossSQL is the stage-1 filter from MATCHING.md.
//
// Three predicates do the real work:
//
//   - ST_DWithin over route_geom::geography gives a metre-accurate proximity
//     test that the route_templates/trip_occurrences geography GIST indexes can
//     serve directly. (MATCHING.md's buffered functional index could not be
//     used by this predicate at all — see RISKS.md open decision 1.)
//   - the ordering check on ST_LineLocatePoint requires the pickup to lie
//     strictly before the dropoff along the driver's line, which is what
//     rejects reverse-direction and perpendicular-crossing routes that buffer
//     overlap alone would happily admit.
//   - the shared-distance floor rejects trivial overlaps: at least 1 km, and at
//     least 20% of the passenger's own trip.
//
// The two are kept separate even though the floor technically subsumes the
// ordering check today — when f_o > f_d the shared distance goes negative and
// fails the floor anyway. Mutation-testing confirmed that removing both admits
// reverse and crossing routes, while removing either alone does not. The
// explicit ordering predicate stays because it states the intent directly and
// survives any future reformulation of the floor.
const grossSQL = `
WITH cand AS (
	SELECT o.id                AS occurrence_id,
	       t.driver_id         AS driver_id,
	       o.route_geom        AS route_geom,
	       o.route_km          AS route_km,
	       o.route_min         AS route_min,
	       o.seats_free        AS seats_free,
	       d.notify_radius_km  AS notify_radius_km,
	       d.detour_budget_min AS detour_budget_min
	FROM trip_occurrences o
	JOIN route_templates  t ON t.id = o.template_id
	JOIN driver_profiles  d ON d.user_id = t.driver_id
	WHERE o.status = 'scheduled'
	  AND o.seats_free >= $5
	  AND o.depart_earliest < $4
	  AND o.depart_latest   > $3
	  AND NOT d.notifications_muted
	  AND ST_DWithin(o.route_geom::geography, $1::geography, d.notify_radius_km * 1000)
	  AND ST_DWithin(o.route_geom::geography, $2::geography, d.notify_radius_km * 1000)
	  AND ($7::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM blocks b
	        WHERE (b.blocker_id = t.driver_id AND b.blocked_id = $7::uuid)
	           OR (b.blocker_id = $7::uuid    AND b.blocked_id = t.driver_id)))
), located AS (
	SELECT occurrence_id, driver_id, route_km, route_min, seats_free, detour_budget_min,
	       ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, $1)) AS f_o,
	       ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, $2)) AS f_d
	FROM cand
)
SELECT occurrence_id, driver_id, f_o, f_d,
       (f_d - f_o) * route_km AS shared_km,
       route_km, route_min, detour_budget_min, seats_free
FROM located
WHERE f_o < f_d
  AND (f_d - f_o) * route_km >= GREATEST(1.0, 0.2 * $6)
ORDER BY (f_d - f_o) * route_km DESC, occurrence_id
LIMIT $8
`

// Gross runs the stage-1 filter and returns the surviving candidates, best
// overlap first.
func Gross(ctx context.Context, pool *pgxpool.Pool, q GrossQuery) ([]Candidate, error) {
	rows, err := pool.Query(ctx, grossSQL, grossArgs(q)...)
	if err != nil {
		return nil, fmt.Errorf("match: gross filter: %w", err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.OccurrenceID, &c.DriverID, &c.FractionOrigin, &c.FractionDest,
			&c.SharedKm, &c.RouteKm, &c.RouteMin, &c.DetourBudget, &c.SeatsFree); err != nil {
			return nil, fmt.Errorf("match: scanning candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("match: reading candidates: %w", err)
	}
	return out, nil
}

// ExplainGross returns the query plan for the gross filter as parsed JSON, so
// tests can assert that the geography index is used without depending on the
// textual plan format.
func ExplainGross(ctx context.Context, pool *pgxpool.Pool, q GrossQuery) (any, error) {
	var raw []byte
	err := pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+grossSQL, grossArgs(q)...).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("match: explaining gross filter: %w", err)
	}

	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("match: parsing plan: %w", err)
	}
	return plan, nil
}

func grossArgs(q GrossQuery) []any {
	var passenger *uuid.UUID
	if q.PassengerID != uuid.Nil {
		passenger = &q.PassengerID
	}
	return []any{
		pointWKT(q.Origin),      // $1
		pointWKT(q.Destination), // $2
		q.WindowStart,           // $3
		q.WindowEnd,             // $4
		q.Seats,                 // $5
		q.RequestKm,             // $6
		passenger,               // $7
		maxCandidates,           // $8
	}
}

// pointWKT renders a point as EWKT so PostGIS parses it with the right SRID.
func pointWKT(p Point) string {
	return fmt.Sprintf("SRID=4326;POINT(%v %v)", p.Lon, p.Lat)
}
