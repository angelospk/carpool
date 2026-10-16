package asks_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Two requests from one passenger for the same hour, matched at the same
// moment. The stage-1 filter skips a trip that overlaps one the passenger is
// already being asked about, but both searches read before either writes, so
// the filter alone lets both through. createAsk holds the passenger's row, and
// rechecking under it is what leaves at most one live offer (QA 2026-10-16,
// Codex review).
func TestTwoRequestsFromOnePassengerCannotBothBeOfferedTheSameHour(t *testing.T) {
	for attempt := range 12 {
		f := newFixture(t)
		ctx := context.Background()
		mustExec(t, f, `DELETE FROM asks`)
		mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
		// The same route twice, the same morning: two trips a passenger could
		// each be offered on their own.
		mustExec(t, f, `
			WITH tpl AS (
				INSERT INTO route_templates
				SELECT (jsonb_populate_record(t, jsonb_build_object('id', gen_random_uuid()))).*
				  FROM route_templates t
				RETURNING id)
			INSERT INTO trip_occurrences
			SELECT (jsonb_populate_record(o, jsonb_build_object(
			          'id', gen_random_uuid(), 'template_id', (SELECT id FROM tpl)))).*
			  FROM trip_occurrences o`)

		var second uuid.UUID
		must(t, f.pool.QueryRow(ctx, `
			INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
			SELECT passenger_id, origin, destination, window_start, window_end, seats, kind, 'open'
			  FROM ride_requests WHERE id = $1
			RETURNING id`, f.requestID).Scan(&second), "second request")

		var wg sync.WaitGroup
		for _, id := range []uuid.UUID{f.requestID, second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = f.matcher().AskNext(ctx, id)
			}()
		}
		wg.Wait()

		var live int
		must(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM asks a JOIN ride_requests rr ON rr.id = a.request_id
			 WHERE rr.passenger_id = $1 AND a.status = 'pending'`, f.passengerID).Scan(&live),
			"counting live offers")
		if live == 0 {
			t.Fatalf("attempt %d: neither request was offered to anybody", attempt)
		}
		if live > 1 {
			t.Fatalf("attempt %d: one passenger holds %d live offers for the same hour", attempt, live)
		}
	}
}
