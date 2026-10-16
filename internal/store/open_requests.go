package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenRequest is a ride somebody has asked for and is still waiting on, with
// how far the search for a driver has got.
//
// The waiting is the point. Asking is sequential — one driver at a time, each
// with an expiry — so a passenger who has posted a request sees nothing happen
// for minutes at a stretch. Without these counts the app can only show a
// pulsing dot, which says "something is happening" for as long as nothing is.
type OpenRequest struct {
	ID               uuid.UUID
	OriginLabel      string
	DestinationLabel string
	WindowStart      time.Time
	WindowEnd        time.Time
	Seats            int
	Status           string

	// DriversAsked counts people, not attempts. Two occurrences of the same
	// driver's route are two asks and one driver, and "ρωτήθηκαν 2 οδηγοί"
	// would be a lie about how wide the search has been.
	DriversAsked int
	// DriversDeclined is those of them who said no or let the ask lapse.
	DriversDeclined int
	// AskingNow is whether somebody is looking at it this minute. A pending ask
	// whose expiry has passed is not: it counts as neither live nor answered
	// until the sweeper moves it on, and claiming otherwise would leave the
	// screen saying "we are asking someone" at a driver who stopped reading.
	AskingNow bool
	// DriversOnCorridor counts drivers with a published route that could ever
	// serve this corridor, so the app can tell "nobody drives this road" apart
	// from "nobody has been asked yet".
	//
	// It deliberately ignores dates, seats, luggage, blocks and
	// notifications_muted, unlike the matcher in waiting.go: the question is
	// whether anybody drives this road, not whether they can take this
	// particular trip.
	DriversOnCorridor int
	// DriversUntried is those drivers on the corridor who have not yet said no
	// to this request or let its ask lapse. Zero, with somebody having passed,
	// is "everybody on this road has passed": DriversDeclined cannot say it,
	// because a driver who passed and then retired is in that count and not in
	// the corridor's.
	DriversUntried int
	// Stranded is a request that had a seat and lost it to the driver: its
	// latest booking was cancelled from the driver's side, and it is back in
	// the queue. The passenger needs to hear that the search restarted on its
	// own, not find a request looking exactly like one never answered.
	Stranded  bool
	CreatedAt time.Time
}

// OpenRequests lists what the caller is still waiting on, newest first.
//
// Matched, cancelled and expired requests are left out: each of them has
// somewhere better to be — a trip, or nothing at all — and a list of things
// still in flight is only useful if everything in it is still in flight.
func OpenRequests(
	ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, now time.Time, limit int,
) ([]OpenRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// One `now` for the whole query, passed in rather than taken from the
	// database, so that a window closing and an ask expiring cannot be judged
	// against two different instants — and so a test can put the clock
	// wherever it needs it.
	// The requests are chosen first, in their own CTE, and only then joined to
	// asks and to the corridor count.
	//
	// Written flat, the corridor lateral ran once per qualifying request before
	// the LIMIT was applied — and nothing caps how many open requests one
	// account can hold, while the screen showing this polls every thirty
	// seconds. That is open-requests x route-templates of spatial work for a
	// list that only ever displays `limit` rows. Narrowing first bounds it to
	// the rows actually being returned.
	rows, err := pool.Query(ctx, `
		WITH mine AS (
			SELECT r.id, r.origin_label, r.destination_label, r.origin, r.destination,
			       r.window_start, r.window_end, r.seats, r.status,
			       r.passenger_id, r.created_at
			FROM ride_requests r
			WHERE r.passenger_id = $1
			  AND r.status IN ('open','asking')
			  AND r.window_end > $2
			ORDER BY r.created_at DESC, r.id DESC
			LIMIT $3
		)
		SELECT r.id,
		       coalesce(r.origin_label, ''), coalesce(r.destination_label, ''),
		       r.window_start, r.window_end, r.seats, r.status,
		       count(DISTINCT t.driver_id),
		       count(DISTINCT t.driver_id) FILTER (WHERE a.status IN ('declined','expired')),
		       coalesce(bool_or(a.status = 'pending' AND a.expires_at > $2), false),
		       corridor.drivers, corridor.untried,
		       coalesce((SELECT lb.status = 'cancelled_by_driver' FROM bookings lb
		                 WHERE lb.request_id = r.id
		                 ORDER BY lb.created_at DESC, lb.id DESC LIMIT 1), false),
		       r.created_at
		FROM mine r
		LEFT JOIN asks a             ON a.request_id = r.id
		LEFT JOIN trip_occurrences o ON o.id = a.occurrence_id
		LEFT JOIN route_templates t  ON t.id = o.template_id
		-- One row per request: whether anybody drives this road at all. The
		-- corridor join is deliberately wider than the matcher in waiting.go —
		-- no dates, seats, luggage, blocks or muted checks — because a driver
		-- who exists but cannot take this trip is still somebody to wait for.
		-- ct rather than t: the outer query already has route_templates as t for
		-- the asks join. Postgres resolves the inner alias, but a reader who
		-- misses the shadowing reads this as counting the drivers already asked.
		LEFT JOIN LATERAL (
			SELECT count(DISTINCT ct.driver_id) AS drivers,
			       count(DISTINCT ct.driver_id) FILTER (WHERE NOT EXISTS (
			         SELECT 1 FROM asks pa
			         JOIN trip_occurrences po ON po.id = pa.occurrence_id
			         JOIN route_templates pt  ON pt.id = po.template_id
			         WHERE pa.request_id = r.id AND pa.status IN ('declined','expired')
			           AND pt.driver_id = ct.driver_id)) AS untried
			FROM route_templates ct
			JOIN driver_profiles d ON d.user_id = ct.driver_id
			JOIN users du ON du.id = ct.driver_id AND du.status = 'active'
			-- A schedule that can still produce a departure. The active column is
			-- the retire flag and nothing sets it when a schedule merely runs
			-- out, so a one-off whose day has gone and a recurring route past
			-- its effective_until both stay active for ever. Counting those
			-- suppressed "nobody drives this road" on the strength of a route
			-- that will never run again, which is the one thing that sentence
			-- exists to say. A template with no schedule row at all — what a
			-- half-finished publish leaves behind — cannot run either.
			--
			-- Compared as a plain date against the caller's instant. The
			-- schedule's dates are Athens wall dates and this is not, so the
			-- comparison can be a day out for a couple of hours around
			-- midnight; for "is there anybody at all on this road" that cannot
			-- change the answer, and threading a location through to here for
			-- it would be machinery bought with no one to use it.
			JOIN route_schedules cs ON cs.template_id = ct.id
			  AND ((cs.kind = 'oneoff' AND cs.oneoff_date >= $2::date)
			    OR (cs.kind = 'recurring'
			        AND (cs.effective_until IS NULL OR cs.effective_until >= $2::date)))
			WHERE ct.active
			  AND ct.driver_id <> r.passenger_id
			  -- A route offered to a group the passenger is not in does not
			  -- drive this road for them, and counting it would hide the
			  -- sentence that says nobody does.
			  AND (ct.group_id IS NULL OR EXISTS (
			        SELECT 1 FROM group_members gm
			        WHERE gm.group_id = ct.group_id AND gm.user_id = r.passenger_id AND gm.left_at IS NULL))
			  AND ST_DWithin(ct.route_geom::geography, r.origin::geography,
			                 d.notify_radius_km * 1000)
			  AND ST_DWithin(ct.route_geom::geography, r.destination::geography,
			                 d.notify_radius_km * 1000)
			  AND ST_LineLocatePoint(ct.route_geom, ST_ClosestPoint(ct.route_geom, r.origin))
			    < ST_LineLocatePoint(ct.route_geom, ST_ClosestPoint(ct.route_geom, r.destination))
		) corridor ON true
		GROUP BY r.id, r.origin_label, r.destination_label,
		         r.window_start, r.window_end, r.seats, r.status,
		         r.created_at, corridor.drivers, corridor.untried
		ORDER BY r.created_at DESC, r.id DESC`, userID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: loading open requests: %w", err)
	}
	defer rows.Close()

	out := []OpenRequest{}
	for rows.Next() {
		var q OpenRequest
		if err := rows.Scan(&q.ID, &q.OriginLabel, &q.DestinationLabel,
			&q.WindowStart, &q.WindowEnd, &q.Seats, &q.Status,
			&q.DriversAsked, &q.DriversDeclined, &q.AskingNow, &q.DriversOnCorridor, &q.DriversUntried, &q.Stranded, &q.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning an open request: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
