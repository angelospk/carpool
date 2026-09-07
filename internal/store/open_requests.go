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
	rows, err := pool.Query(ctx, `
		SELECT r.id,
		       coalesce(r.origin_label, ''), coalesce(r.destination_label, ''),
		       r.window_start, r.window_end, r.seats, r.status,
		       count(DISTINCT t.driver_id),
		       count(DISTINCT t.driver_id) FILTER (WHERE a.status IN ('declined','expired')),
		       coalesce(bool_or(a.status = 'pending' AND a.expires_at > $2), false),
		       r.created_at
		FROM ride_requests r
		LEFT JOIN asks a             ON a.request_id = r.id
		LEFT JOIN trip_occurrences o ON o.id = a.occurrence_id
		LEFT JOIN route_templates t  ON t.id = o.template_id
		WHERE r.passenger_id = $1
		  AND r.status IN ('open','asking')
		  AND r.window_end > $2
		GROUP BY r.id
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $3`, userID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: loading open requests: %w", err)
	}
	defer rows.Close()

	out := []OpenRequest{}
	for rows.Next() {
		var q OpenRequest
		if err := rows.Scan(&q.ID, &q.OriginLabel, &q.DestinationLabel,
			&q.WindowStart, &q.WindowEnd, &q.Seats, &q.Status,
			&q.DriversAsked, &q.DriversDeclined, &q.AskingNow, &q.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning an open request: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
