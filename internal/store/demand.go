package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Corridor is a road people are waiting on, and how many of them.
//
// A corridor and a count leave this file and nothing else: no passenger id, no
// name, no departure time, no coordinates. That is not caution for its own
// sake. Everywhere else in this codebase a request is shown to the person who
// made it or to a driver who has been asked about it specifically; this is the
// one place where one person's stated journey is put in front of somebody who
// is not part of it, on the strength of nothing but their being signed in.
//
// So the struct is the boundary. A field added here reaches every user, and the
// test in demand_test.go fails when one appears.
type Corridor struct {
	OriginLabel      string
	DestinationLabel string
	// People counts people rather than requests. Somebody who asks for the same
	// road on Monday and again on Tuesday is one person looking for it, and
	// counting them twice would tell a driver the road is busier than it is —
	// which is the one lie this feature must not tell, because the whole point
	// of it is to be a reason to publish.
	People int
}

// Demand lists the corridors people are waiting on, busiest first.
//
// It exists because a driver who has published nothing has no reason to publish
// anything, and the app never gave them one. "Three people are looking for
// Αλεξανδρούπολη → Ορεστιάδα this week" is the strongest argument available for
// publishing a route, and the rows it is built from were already in the table.
//
// Grouped on the labels rather than on geometry: a request's labels are the
// settlement names the passenger picked from the places list, with no meeting
// point appended, so they are already town-level and already the words a driver
// would recognise. Grouping on coordinates would need a gazetteer in SQL that
// does not exist here, and would be finer-grained than anybody wants to read.
//
// now and within come from the caller for the same reason as everywhere else in
// this package: the period is a product decision the API states, and a test has
// to be able to put the clock where it needs it.
func Demand(
	ctx context.Context, pool *pgxpool.Pool, now time.Time, within time.Duration, limit int,
) ([]Corridor, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		SELECT r.origin_label, r.destination_label, count(DISTINCT r.passenger_id)
		FROM ride_requests r
		-- The same check the matcher makes before waking anybody: a suspended
		-- account's request stays in the table until the cascade withdraws it,
		-- and advertising it would send a driver to publish for a journey
		-- nobody can be taken on.
		JOIN users pu ON pu.id = r.passenger_id AND pu.status = 'active'
		WHERE r.status IN ('open','asking')
		  -- Read from the window rather than the status, because a request whose
		  -- time has passed is only marked expired when the sweeper next runs.
		  AND r.window_end   > $1
		  AND r.window_start < $2
		  -- A request from before labels were recorded is real demand and
		  -- unreportable: a line reading " → Ορεστιάδα" is worse than one fewer
		  -- line.
		  AND coalesce(r.origin_label, '') <> ''
		  AND coalesce(r.destination_label, '') <> ''
		GROUP BY r.origin_label, r.destination_label
		-- Busiest first, then a stable tie-break. With a limit in play this
		-- decides what a driver is told about at all.
		ORDER BY count(DISTINCT r.passenger_id) DESC, r.origin_label, r.destination_label
		LIMIT $3`, now, now.Add(within), limit)
	if err != nil {
		return nil, fmt.Errorf("store: loading demand: %w", err)
	}
	defer rows.Close()

	out := []Corridor{}
	for rows.Next() {
		var c Corridor
		if err := rows.Scan(&c.OriginLabel, &c.DestinationLabel, &c.People); err != nil {
			return nil, fmt.Errorf("store: scanning a corridor: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
