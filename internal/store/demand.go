package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// DemandFloor is how many different people must be waiting on a corridor before
// it is reported at all.
//
// A corridor is two settlement names, and in a region of villages "one person
// wants Πέπλος → Ορεστιάδα this week" is close to naming them. Polling makes it
// worse than a single disclosure: a count that moves from two to three tells a
// watcher somebody new started travelling, and a corridor appearing at all
// tells them it became busy this week. A count on its own protects nobody once
// it can be watched over time.
//
// Three, and the number is on the safe side on purpose. It is also the number
// that decides whether this feature says anything at all in a market with no
// users yet — which is the honest answer in that case, because a road one
// person wants is not yet evidence that publishing it will find passengers. A
// driver told nothing has lost nothing; a passenger identified cannot be
// un-identified.
//
// If this is ever lowered, docs/legal/privacy-el.md has to be revisited with
// it: what is disclosed to other users is a promise, not a setting.
const DemandFloor = 3

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
	ctx context.Context, pool *pgxpool.Pool, viewer uuid.UUID,
	now time.Time, within time.Duration, limit int,
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
		  -- Not your own. "Three people are looking for this road" with yourself
		  -- as one of the three is an encouragement built partly out of your own
		  -- demand, and it is the one figure on that screen a reader would have
		  -- to discount in their head. It also tells somebody with a single
		  -- request on a quiet corridor exactly which of the numbers is theirs.
		  AND r.passenger_id <> $4
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
		-- The privacy floor, applied after grouping and counted on the people
		-- who would actually be reported — the caller is already excluded
		-- above, so they cannot push a corridor over the line with themselves.
		HAVING count(DISTINCT r.passenger_id) >= $5
		-- Busiest first, then a stable tie-break. With a limit in play this
		-- decides what a driver is told about at all.
		ORDER BY count(DISTINCT r.passenger_id) DESC, r.origin_label, r.destination_label
		LIMIT $3`, now, now.Add(within), limit, viewer, DemandFloor)
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
