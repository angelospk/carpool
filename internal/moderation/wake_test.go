package moderation_test

import (
	"context"
	"testing"
)

// wakes counts the queued "who is waiting for this trip?" messages.
//
// Asserted at the site rather than only end to end, because the site is the
// easy part to lose: the reverse matching works perfectly in a test that queues
// the wake by hand, which is exactly what would keep passing after somebody
// removed the call.
func (w *world) wakes(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&n); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	return n
}

// Suspending somebody takes them off the trips they had booked, and those seats
// are now free for whoever was told there was no room.
func TestSuspendingSomebodyAsksWhoWasWaitingForTheirSeats(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if n := w.wakes(t); n != 0 {
		t.Fatalf("%d wakes before the suspension, want none", n)
	}

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "spam"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	if n := w.wakes(t); n == 0 {
		t.Fatal("no wakes after the suspension freed their seats, want the trips to ask who was waiting")
	}
}
