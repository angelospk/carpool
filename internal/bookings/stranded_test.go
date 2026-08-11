package bookings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// A driver calling off a departure has to hand its passengers back to the
// matcher, not just tell them it is off.
//
// Cancelling a departure cancels its bookings and sends each passenger a push,
// and there it stopped. Their ride_request stays at 'matched' for ever: nothing
// reads that state looking for work, so the request that already says where
// they are going, when, with how many seats and how much luggage is never shown
// to another driver. The app told them their ride was cancelled and then did
// nothing, while a driver on the same corridor an hour later was never asked.
//
// This is the same hole that was fixed for a suspension, one layer down. It is
// worth fixing at both because a driver calling off their own trip is the
// ordinary case and a suspension is the rare one.
//
// Reopening is what they already agreed to: a request is standing permission to
// be matched inside its window, which is how it got its first driver.

func requestFor(t *testing.T, w *world, bookingID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(),
		`SELECT request_id FROM bookings WHERE id = $1`, bookingID).Scan(&id); err != nil {
		t.Fatalf("finding the request behind the booking: %v", err)
	}
	return id
}

func requestStatus(t *testing.T, w *world, requestID uuid.UUID) string {
	t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, requestID).Scan(&s); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	return s
}

func asksQueuedFor(t *testing.T, w *world, requestID uuid.UUID) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'ask_next' AND payload->>'request_id' = $1::text`, requestID).Scan(&n); err != nil {
		t.Fatalf("counting queued asks: %v", err)
	}
	return n
}

func TestCallingOffADepartureLooksForAnotherDriverForItsPassengers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if err := w.svc.CancelOccurrence(ctx, w.occurrenceID, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	if got := requestStatus(t, w, requestID); got != "open" {
		t.Fatalf("the passenger's request is %q after their driver called the trip off, want open", got)
	}
	if n := asksQueuedFor(t, w, requestID); n != 1 {
		t.Fatalf("%d asks queued for a passenger whose ride was called off, want 1", n)
	}
}

// A booking made from a standing arrangement has no request behind it: the
// arrangement is the standing permission, and it survives one cancelled week.
// Nothing to reopen, and nothing that falls over trying.
func TestCallingOffADepartureLeavesAStandingArrangementAlone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.CancelOccurrence(ctx, w.secondOccurrence, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	var queued int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&queued); err != nil {
		t.Fatalf("counting queued asks: %v", err)
	}
	if queued != 0 {
		t.Fatalf("%d asks queued for a booking that never came from a request", queued)
	}
}

// Somebody whose window has since closed is not put back in the queue. There is
// no ride left to find them, and the sweeper is about to mark the request
// expired anyway.
func TestCallingOffADepartureLeavesAPassengerWhoseTimeHasPassed(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if _, err := w.pool.Exec(ctx, `
		UPDATE ride_requests SET window_start = $2::timestamptz - interval '3 hours',
		                         window_end   = $2::timestamptz - interval '1 hour'
		 WHERE id = $1`, requestID, testNow); err != nil {
		t.Fatalf("moving the window into the past: %v", err)
	}

	if err := w.svc.CancelOccurrence(ctx, w.occurrenceID, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	if got := requestStatus(t, w, requestID); got == "open" {
		t.Fatalf("a request whose window closed was put back in the queue")
	}
	if n := asksQueuedFor(t, w, requestID); n != 0 {
		t.Fatalf("%d asks queued for a journey whose time has passed", n)
	}
}
