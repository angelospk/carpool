package match

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WaitingQuery is one trip, asked who has been waiting for it.
type WaitingQuery struct {
	OccurrenceID uuid.UUID
	// Now decides which offers still stand and which requests have run out of
	// time. From the caller's clock, for the same reason as GrossQuery.Now.
	Now time.Time
	// Limit bounds the shortlist. It is not only storm protection: everything
	// past it is left for the next wake or the reconciling sweep, so it has to
	// be read together with the ordering, which puts the longest wait first.
	Limit int
}

// waitingSQL is the gross filter run backwards: one occurrence, and the open
// requests it could answer.
//
// It is deliberately a *superset* of what Gross would match. The two are
// separate queries and the dangerous direction is being stricter here — a
// reverse query that misses a pair is silent, because the driver publishes,
// nobody is woken, and the request dies of old age exactly as it did before any
// of this existed. A pair this returns that Gross then rejects costs one
// indexed query and nothing else.
//
// So this carries the cheap, indexable, obviously-mirrored narrowing: status,
// windows, seats, luggage size, proximity, direction along the road, and who
// has already been asked. It deliberately leaves out the shared-distance floor,
// which is the finest-grained of the forward predicates and the one whose
// duplication would be most likely to drift. Gross remains the authority on
// whether a pair is really a match, and the containment property is tested
// rather than asserted from reading the two side by side.
//
// waitingFromWith is everything Waiting and DriveDemand share: one trip, and
// the requests it could answer. They differ in which requests still count as
// waiting and which earlier offers on this trip rule one out.
func waitingFromWith(waitingStatuses, waitingSpentAsks string) string {
	return `
WITH trip AS (
	SELECT o.id, o.route_geom, o.seats_free, o.luggage,
	       o.depart_earliest, o.depart_latest, o.drive_set_at, o.route_min, o.detour_min, o.drive_detour_min,
	       t.driver_id, d.notify_radius_km, du.gender AS driver_gender
	FROM trip_occurrences o
	JOIN route_templates t ON t.id = o.template_id
	JOIN driver_profiles d ON d.user_id = t.driver_id
	-- Checked once here rather than per candidate: there is one driver.
	JOIN users du ON du.id = t.driver_id AND du.status = 'active'
	WHERE o.id = $1
	  AND o.status = 'scheduled'
	  -- The mirror of the same clause in the forward filter. A seat comes back
	  -- whenever a booking is cancelled, including ten minutes after the car
	  -- left, and that wake offered the seat to everybody still waiting on the
	  -- corridor.
	  AND ` + DriveOpen("o", "$2") + `
	  AND NOT d.notifications_muted
)
SELECT rr.id
FROM ride_requests rr
JOIN trip ON true
-- The mirror of the driver check above. A suspended passenger's request stays
-- in the table until the cascade withdraws it, and a wake that arrives first
-- would otherwise hand a driver a real seat for somebody who cannot open the
-- app to agree a meeting point, answer a message, or say they are not coming.
JOIN users pu ON pu.id = rr.passenger_id AND pu.status = 'active'
WHERE rr.status IN (` + waitingStatuses + `)
  -- Read from the window rather than the status. A request whose time has
  -- passed is only marked expired when the sweeper next runs, and waking it in
  -- between would offer somebody a lift to a journey they have already made or
  -- given up on.
  AND rr.window_end > $2
  -- A drive is compared with when the car reaches the passenger, not when it
  -- leaves; the forward filter does that, and this side stays looser.
  AND (trip.drive_set_at IS NOT NULL OR (rr.window_start < trip.depart_latest
                                         AND rr.window_end > trip.depart_earliest))
  AND rr.passenger_id <> trip.driver_id
  AND rr.seats <= trip.seats_free - ` + spokenForTrip + `
  AND trip.luggage >= rr.luggage
  -- A repeat named the trip it wants; it is woken by that trip and no other.
  -- Excluding targeted requests altogether would be a false negative: one whose
  -- named trip was full when it was made has never been asked at all.
  AND (rr.kind = 'open' OR rr.target_occurrence_id = trip.id)
  AND NOT EXISTS (
        SELECT 1 FROM asks a
        WHERE a.request_id = rr.id AND a.occurrence_id = trip.id
          AND a.status IN (` + waitingSpentAsks + `))
  -- The mirrors of the forward filter: nobody is offered a departure they
  -- already have a seat on, and a passenger who wants women drivers only is
  -- not woken by anybody else.
  AND NOT EXISTS (
        SELECT 1 FROM bookings bk
        WHERE bk.occurrence_id = trip.id AND bk.passenger_id = rr.passenger_id)
  AND (NOT pu.women_only OR trip.driver_gender = 'female')
  AND NOT EXISTS (
        SELECT 1 FROM blocks b
        WHERE (b.blocker_id = trip.driver_id AND b.blocked_id = rr.passenger_id)
           OR (b.blocker_id = rr.passenger_id AND b.blocked_id = trip.driver_id))
  AND ST_DWithin(trip.route_geom::geography, rr.origin::geography,
                 trip.notify_radius_km * 1000)
  AND ST_DWithin(trip.route_geom::geography, rr.destination::geography,
                 trip.notify_radius_km * 1000)
  -- Same direction along the driver's line, which is what rejects the return
  -- leg and the road that merely crosses this one.
  AND ST_LineLocatePoint(trip.route_geom, ST_ClosestPoint(trip.route_geom, rr.origin))
    < ST_LineLocatePoint(trip.route_geom, ST_ClosestPoint(trip.route_geom, rr.destination))
`
}

// waitingFrom is the wake's filter: requests nobody is asking about yet, and
// never one already offered this trip.
var waitingFrom = waitingFromWith(`'open'`, `'pending','accepted','declined','expired','cancelled'`)

var waitingSQL = waitingFrom + `
-- Longest wait first, then a stable tie-break. With a limit in play this
-- decides who makes the shortlist at all, so it is fairness rather than
-- presentation: best-match-first would let somebody on an awkward corner of the
-- corridor be passed over for ever.
ORDER BY rr.created_at, rr.id
LIMIT $3
`

// spokenForTrip is the live-offer count against the trip CTE.
var spokenForTrip = SeatsSpokenFor("trip.id", "$2")

// Waiting returns the open requests a trip could answer, longest wait first.
//
// It is the other half of the matcher. Gross asks "who could take this
// passenger"; this asks "who is waiting for this trip", and without it a
// request that found nobody when it was made is never reconsidered: the perfect
// driver publishes an hour later and the two never meet.
//
// The result is a shortlist to wake, not a set of matches. Each id goes back
// through the ordinary ask machinery, which re-runs the forward filter and
// stays the authority on whether the pair really works.
func Waiting(ctx context.Context, pool *pgxpool.Pool, q WaitingQuery) ([]uuid.UUID, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = maxCandidates
	}

	rows, err := pool.Query(ctx, waitingSQL, q.OccurrenceID, q.Now, limit)
	if err != nil {
		return nil, fmt.Errorf("match: finding waiting requests: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("match: scanning waiting request: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExportedWaitingSQL is the query text, for plan assertions in tests.
func ExportedWaitingSQL() string { return waitingSQL }

// demandFrom is the nudge's filter. Somebody being asked about a seat — by
// this driver or another — is still waiting, and is exactly whom a pinned
// minute helps; only somebody this driver has already turned down, or let an
// offer lapse on, is not news to them.
var demandFrom = waitingFromWith(`'open','asking'`, `'declined','expired'`)

// DriveDemand counts the people a trip could pick up if it left at its
// earliest departure: everybody still waiting on its road, held to DriveReady. It is
// what the nudge tells a driver, and it is a count of everybody rather than of
// a shortlist, so a first page of people who are not ready cannot hide one who
// is.
//
// It runs on whatever it is given, so a caller holding a transaction counts
// through that transaction rather than asking the pool for a second
// connection while it holds a lock.
func DriveDemand(ctx context.Context, q Querier, occurrenceID uuid.UUID, now time.Time) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM (`+demandFrom+`
		  AND `+DriveReady("trip.depart_earliest", "trip.route_min",
		"ST_LineLocatePoint(trip.route_geom, ST_ClosestPoint(trip.route_geom, rr.origin))",
		"ST_Distance(trip.route_geom::geography, rr.origin::geography)",
		"GREATEST(rr.window_start, $2)", "rr.window_end", DriveLateBy("trip."))+`
		) ready`, occurrenceID, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("match: counting who a drive could take: %w", err)
	}
	return n, nil
}

// Querier is the one method DriveDemand needs: a pool or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
