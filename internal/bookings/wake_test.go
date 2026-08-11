package bookings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// wakesFor counts the queued "who is waiting for this trip?" messages against
// an occurrence.
func wakesFor(t *testing.T, w *world, occurrenceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'wake_waiting'
		  AND payload->>'occurrence_id' = $1::text`, occurrenceID).Scan(&n); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	return n
}

// A returned seat has to tell somebody, or the trip has room and the people who
// were turned away from it never hear.
//
// This is asserted at the site rather than only end to end, because the site is
// the part that is easy to lose: the matching works perfectly well in a test
// that queues the wake by hand, which is exactly what would keep passing after
// somebody deleted the call.
func TestCancellingABookingAsksWhoWasWaiting(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if n := wakesFor(t, w, w.occurrenceID); n != 0 {
		t.Fatalf("%d wakes before the cancellation, want none", n)
	}

	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if n := wakesFor(t, w, w.occurrenceID); n != 1 {
		t.Fatalf("%d wakes after the seat came back, want the trip to ask who was waiting", n)
	}
}

// Cancelling something already cancelled gives no seat back, so there is
// nothing to tell anyone about.
func TestCancellingTwiceAsksOnlyOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	_ = w.svc.Cancel(ctx, w.fromAsk, w.passengerID)

	if n := wakesFor(t, w, w.occurrenceID); n != 1 {
		t.Fatalf("%d wakes for one returned seat, want 1", n)
	}
}
