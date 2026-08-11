package asks_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/moderation"
)

// The gap a status check on its own leaves open.
//
// A moderator presses suspend while a driver taps accept. The accept read the
// account a moment before it changed and is holding a transaction open; the
// cascade sweeps past the asks and requests, sees nothing, and commits. Then the
// accept commits its booking. Nothing later revisits either row, so a suspended
// person keeps a seat on somebody's trip, and the only trace is a moderator
// wondering why the account they just suspended is on a trip tomorrow.
//
// What closes it is that both sides take the user row first: suspension takes it
// FOR UPDATE, matching takes it FOR SHARE, so one of the two waits and then sees
// what the other did. The test is a race rather than a barrier because there is
// no hook to stop at, so it is run enough times to catch a window that is only
// microseconds wide, and it asserts the outcome is one of exactly two consistent
// ones rather than asserting which.

func TestSuspensionAndAcceptCannotBothWin(t *testing.T) {
	for attempt := range 12 {
		f := newFixture(t)
		ctx := context.Background()

		mod := f.newModerator(t)

		var wg sync.WaitGroup
		wg.Add(2)
		var acceptErr error
		go func() {
			defer wg.Done()
			_, acceptErr = f.service().Accept(ctx, f.askID, f.driverID)
		}()
		go func() {
			defer wg.Done()
			if err := moderation.NewService(f.pool, f.clock).
				Suspend(ctx, f.passengerID, mod, "harassment"); err != nil {
				t.Errorf("Suspend: %v", err)
			}
		}()
		wg.Wait()

		var bookings int
		must(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM bookings WHERE passenger_id = $1`, f.passengerID).Scan(&bookings),
			"counting bookings")

		// Two consistent outcomes. Either the accept got in first and the
		// cascade cancelled the booking it found, or the suspension got in first
		// and the accept was refused. What must never be left behind is a live
		// booking for a suspended account.
		var live int
		must(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM bookings WHERE passenger_id = $1 AND status = 'confirmed'`,
			f.passengerID).Scan(&live), "counting live bookings")
		if live != 0 {
			t.Fatalf("attempt %d: a suspended passenger holds %d confirmed seats (accept err %v)",
				attempt, live, acceptErr)
		}

		var pending int
		must(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM asks a JOIN ride_requests rr ON rr.id = a.request_id
			 WHERE rr.passenger_id = $1 AND a.status = 'pending'`, f.passengerID).Scan(&pending),
			"counting live offers")
		if pending != 0 {
			t.Fatalf("attempt %d: %d offers about a suspended passenger are still live", attempt, pending)
		}
	}
}

// newModerator adds somebody who is allowed to suspend, since the fixture only
// has the two people in the ride.
func (f *fixture) newModerator(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	must(t, f.pool.QueryRow(context.Background(), `
		INSERT INTO users (date_of_birth, phone, name, home_region_id, is_moderator)
		VALUES (DATE '1990-01-01', '+306900009999', 'Επόπτης',
		        (SELECT home_region_id FROM users WHERE id = $1), true)
		RETURNING id`, f.driverID).Scan(&id), "moderator")
	return id
}
