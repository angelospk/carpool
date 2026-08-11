package standing_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// wakes counts the queued "who is waiting for this trip?" messages.
//
// Asserted at the site rather than only end to end, because the site is the
// easy part to lose: the reverse matching works perfectly in a test that queues
// the wake by hand, which is exactly what would keep passing after somebody
// removed the call.
func wakes(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&n); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	return n
}

// Ending a standing arrangement gives back every future week at once, and each
// of those is a trip with room on it again.
func TestCancellingAnArrangementAsksWhoWasWaiting(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	before := wakes(t, w.pool)

	if err := w.svc.Cancel(ctx, id, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if n := wakes(t, w.pool); n <= before {
		t.Fatalf("%d wakes after the weeks were released, want more than the %d before", n, before)
	}
}
