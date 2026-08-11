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

	"carpool/internal/domain"
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
	// Luggage is what the passenger is bringing. A driver who said they will
	// take a cabin bag is not asked about a bicycle: they would say no, and
	// both of them would have spent the exchange finding that out.
	Luggage domain.Luggage
	// LuggageCount is how many items of that size, which is the other half of
	// the question. One maximum size cannot say whether three suitcases fit in a
	// boot that takes one.
	LuggageCount int
	// RequestKm is the passenger's own trip length, used for the shared-distance
	// floor.
	RequestKm float64
	// PassengerID, when set, excludes occurrences whose driver has blocked the
	// passenger or is blocked by them, and the passenger's own trips: a driver
	// who also posted a request is not a candidate to carry themselves.
	PassengerID uuid.UUID
	// RequestID, when set, excludes occurrences this request has already been
	// offered to.
	//
	// It belongs here rather than in the caller because the exclusion has to
	// happen before the candidate limit. Filtering afterwards lets a full page
	// of spent occurrences hide every fresh one behind it, and the passenger is
	// told nobody can take them while a driver who was never approached sits one
	// row below the cut.
	RequestID uuid.UUID
	// Target, when set, restricts the search to one occurrence. A repeat names
	// the trip it wants, and offering anything else answers a question the
	// passenger did not ask.
	//
	// Same reason as RequestID for being in the query: applied afterwards, a
	// targeted request whose occurrence ranks twenty-first finds nothing.
	Target *uuid.UUID
	// Now decides which offers still stand. It comes from the caller's clock
	// rather than the database's so that the filter and the code that acts on it
	// agree about the time, which is also what lets a test move it.
	Now time.Time
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
	// Budget is what this trip's driver agreed to, read from the occurrence
	// rather than the profile: the profile is only what fills in a new route.
	Budget    Budget
	SeatsFree int
	// ItineraryVersion is the occurrence's plan version at the time of the
	// query. An ask records it so the accept path can detect that the plan moved.
	ItineraryVersion int
}

// SeatsSpokenFor is the SQL expression for how many seats on an occurrence are
// already promised to somebody: the party sizes behind every live ask.
//
// A pending ask is not a booking and holds no seat in the occurrence's own
// count, which is right — a driver who never answers should not strand the
// trip. But it is an offer, and offering the same seat to four people means
// three of them are told it is theirs and then told it went.
//
// The seats are read through ride_requests because that is where a party size
// lives; asks record the offer, not its size.
//
// Expiry is read rather than trusted to the status. Nothing runs at the instant
// an offer lapses, so an occurrence whose sweeper has not come round yet would
// otherwise stay unaskable for as long as the sweep interval.
//
// It takes its parameters as strings so that the same expression serves the
// candidate filter, which scans many occurrences, and the admission check,
// which locks one. Two hand-written copies of this rule would be free to drift,
// and drift here means either a silent storm or a trip nobody can be asked
// about.
func SeatsSpokenFor(occurrence, now string) string {
	return `COALESCE((
		SELECT sum(rr.seats)
		FROM asks a
		JOIN ride_requests rr ON rr.id = a.request_id
		WHERE a.occurrence_id = ` + occurrence + `
		  AND a.status = 'pending'
		  AND a.expires_at > ` + now + `), 0)`
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
var grossSQL = `
WITH cand AS (
	SELECT o.id                AS occurrence_id,
	       t.driver_id         AS driver_id,
	       o.route_geom        AS route_geom,
	       o.route_km          AS route_km,
	       o.route_min         AS route_min,
	       o.seats_free        AS seats_free,
	       o.itinerary_version AS itinerary_version,
	       d.notify_radius_km  AS notify_radius_km,
	       o.allow_stops       AS allow_stops,
	       o.detour_km         AS detour_km,
	       o.detour_min        AS detour_min
	FROM trip_occurrences o
	JOIN route_templates  t ON t.id = o.template_id
	JOIN driver_profiles  d ON d.user_id = t.driver_id
	-- Somebody who is suspended or gone is not a driver anybody can be sent to.
	-- The suspension cascade already calls off their departures, so in an
	-- unbroken system this changes nothing; it is here because the cascade is a
	-- list of the things somebody thought of, and a matcher that reads the
	-- occurrence's status but never the person's has no way of noticing when
	-- that list is short. 'active' rather than <> 'suspended', so a state added
	-- later is excluded until somebody decides otherwise.
	JOIN users du ON du.id = t.driver_id AND du.status = 'active'
	WHERE o.status = 'scheduled'
	  AND o.seats_free - ` + SeatsSpokenFor("o.id", "$13") + ` >= $5
	  -- Excluded rather than ranked down. An ask a driver will certainly
	  -- refuse costs them a notification and the passenger a wait, and the
	  -- enum's own order is what "will certainly refuse" means.
	  AND o.luggage >= $9::luggage_size
	  -- And enough places for that many of them. Advisory only: what a search
	  -- shows can be out of date by the time somebody acts on it, and the
	  -- booking recomputes this under the occurrence lock. Here it stops the
	  -- driver being asked about a boot that is already full.
	  AND o.luggage_capacity - COALESCE((
	        SELECT sum(b.luggage_count) FROM bookings b
	        WHERE b.occurrence_id = o.id AND b.status IN ('confirmed', 'completed')
	      ), 0) >= $10
	  AND o.depart_earliest < $4
	  AND o.depart_latest   > $3
	  -- And it has not gone yet. The two lines above compare the departure
	  -- against the *request's* window, which is written once and never moves:
	  -- one driver is asked at a time with half an hour to answer, so a request
	  -- naming a morning is still being matched well into that morning, and
	  -- every departure inside its window that had already left stayed a
	  -- candidate. depart_earliest is what the rest of the app already means by
	  -- departed — the moment a pair may confirm the ride, and the moment the
	  -- driver may no longer call it off — so it is what is used here.
	  AND o.depart_earliest > $13
	  AND NOT d.notifications_muted
	  AND ST_DWithin(o.route_geom::geography, $1::geography, d.notify_radius_km * 1000)
	  AND ST_DWithin(o.route_geom::geography, $2::geography, d.notify_radius_km * 1000)
	  AND ($7::uuid IS NULL OR t.driver_id <> $7::uuid)
	  AND ($7::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM blocks b
	        WHERE (b.blocker_id = t.driver_id AND b.blocked_id = $7::uuid)
	           OR (b.blocker_id = $7::uuid    AND b.blocked_id = t.driver_id)))
	  -- Both of these are inside the filter rather than applied to its results,
	  -- so that a spent or unwanted occurrence never occupies a place under the
	  -- LIMIT that a reachable one needed.
	  AND ($11::uuid IS NULL OR NOT EXISTS (
	        SELECT 1 FROM asks a
	        WHERE a.request_id = $11::uuid AND a.occurrence_id = o.id))
	  AND ($12::uuid IS NULL OR o.id = $12::uuid)
), located AS (
	SELECT occurrence_id, driver_id, route_km, route_min, seats_free, itinerary_version,
	       allow_stops, detour_km, detour_min,
	       ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, $1)) AS f_o,
	       ST_LineLocatePoint(route_geom, ST_ClosestPoint(route_geom, $2)) AS f_d
	FROM cand
)
SELECT occurrence_id, driver_id, f_o, f_d,
       (f_d - f_o) * route_km AS shared_km,
       route_km, route_min, allow_stops, detour_km, detour_min, seats_free, itinerary_version
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
			&c.SharedKm, &c.RouteKm, &c.RouteMin, &c.Budget.AllowStops, &c.Budget.Km, &c.Budget.Min, &c.SeatsFree,
			&c.ItineraryVersion); err != nil {
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
	var request *uuid.UUID
	if q.RequestID != uuid.Nil {
		request = &q.RequestID
	}
	return []any{
		pointWKT(q.Origin),       // $1
		pointWKT(q.Destination),  // $2
		q.WindowStart,            // $3
		q.WindowEnd,              // $4
		q.Seats,                  // $5
		q.RequestKm,              // $6
		passenger,                // $7
		maxCandidates,            // $8
		luggageOrNone(q.Luggage), // $9
		q.LuggageCount,           // $10
		request,                  // $11
		q.Target,                 // $12
		q.Now,                    // $13
	}
}

// luggageOrNone defaults an unset size to the smallest.
//
// A caller that forgets it is asking on behalf of somebody with a bag they can
// hold, which is what most people have. The alternative — treating unset as
// "bulky" — would quietly exclude every driver from a request that simply did
// not mention luggage.
func luggageOrNone(l domain.Luggage) string {
	if !l.Valid() {
		return string(domain.LuggageNone)
	}
	return string(l)
}

// pointWKT renders a point as EWKT so PostGIS parses it with the right SRID.
func pointWKT(p Point) string {
	return fmt.Sprintf("SRID=4326;POINT(%v %v)", p.Lon, p.Lat)
}
