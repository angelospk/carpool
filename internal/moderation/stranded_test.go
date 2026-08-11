package moderation_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The passengers of a suspended driver need another driver, not just the news.
//
// Suspending somebody calls off every departure of theirs and sends each
// passenger a push, and there it stopped. Their ride_request stays at 'matched'
// for ever, and nothing reads that state looking for work, so the request that
// already says where they are going and when is never shown to another driver.
//
// The suspension path already does this for the people who were only being
// asked. It did not do it for the people who had actually been given a seat,
// which is the group with the most to lose: they had a ride, and now they have
// a notification.

func (w *world) requestBehind(bookingID uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(),
		`SELECT request_id FROM bookings WHERE id = $1`, bookingID).Scan(&id); err != nil {
		w.t.Fatalf("finding the request behind the booking: %v", err)
	}
	return id
}

func (w *world) requestStatus(id uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, id).Scan(&s); err != nil {
		w.t.Fatalf("reading the request: %v", err)
	}
	return s
}

func (w *world) asksQueuedFor(id uuid.UUID) int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'ask_next' AND payload->>'request_id' = $1::text`, id).Scan(&n); err != nil {
		w.t.Fatalf("counting queued asks: %v", err)
	}
	return n
}

func TestSuspendingADriverFindsTheirPassengersSomebodyElse(t *testing.T) {
	w := newWorld(t)
	requestID := w.requestBehind(w.tomorrowBooking)

	w.suspend()

	if got := w.requestStatus(requestID); got != "open" {
		t.Fatalf("the passenger's request is %q after their driver was suspended, want open", got)
	}
	if n := w.asksQueuedFor(requestID); n != 1 {
		t.Fatalf("%d asks queued for a passenger whose driver was suspended, want 1", n)
	}
}

// The ride that already happened is not reopened. Nobody is owed another lift
// to a journey they have made.
func TestSuspendingADriverLeavesAJourneyAlreadyMade(t *testing.T) {
	w := newWorld(t)
	requestID := w.requestBehind(w.pastBooking)

	w.suspend()

	if got := w.requestStatus(requestID); got == "open" {
		t.Fatalf("a completed ride was put back in the queue")
	}
	if n := w.asksQueuedFor(requestID); n != 0 {
		t.Fatalf("%d asks queued for a ride that already happened", n)
	}
}
