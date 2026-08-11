package match_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"carpool/internal/match"
)

// The reverse query must reach its requests through an index.
//
// It is the one query in the system that scans the open requests rather than
// the trips, and a sequential scan there grows with every person waiting
// anywhere in the country. It looked fine when it was written — five thousand
// open requests answer in eleven milliseconds — which is exactly why it is
// asserted rather than left to be noticed later, at fifty thousand, as a
// latency graph nobody can explain.
//
// The plain GIST indexes on origin and destination cannot serve this: the
// proximity test casts to geography for metre-accurate distances, and a cast is
// a different expression. Hence the expression indexes in 00030, and hence this
// test, which fails if somebody drops them as apparent duplicates of the two
// that were already there.
func TestWaitingReachesRequestsThroughAnIndex(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))

	// Enough dispersed requests that an index path is meaningful, plus some
	// genuinely on the corridor so the query has something to return.
	for i := range 2000 {
		lon := 25.0 + float64(i%200)*0.01
		lat := 40.4 + float64(i/200)*0.02
		f.addRequest(t, withRequestEndsAt(lon, lat, lon+0.15, lat))
	}
	for range 10 {
		f.addRequest(t)
	}
	if _, err := f.pool.Exec(ctx, `
		ANALYZE ride_requests; ANALYZE trip_occurrences; ANALYZE asks;
		ANALYZE route_templates; ANALYZE driver_profiles`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	var raw []byte
	if err := f.pool.QueryRow(ctx,
		`EXPLAIN (FORMAT JSON) `+match.ExportedWaitingSQL(),
		occurrenceID, time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC), 10,
	).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	var plan any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decoding the plan: %v", err)
	}

	// Either endpoint's index is enough: the planner picks whichever is more
	// selective, and pinning which one would be asserting on its arithmetic
	// rather than on the thing that matters.
	if !planUsesIndex(plan, "ride_requests_origin_geog") &&
		!planUsesIndex(plan, "ride_requests_dest_geog") {
		pretty, _ := json.MarshalIndent(plan, "", "  ")
		t.Fatalf("the reverse match scanned ride_requests rather than indexing into it; plan was:\n%s", pretty)
	}
}
