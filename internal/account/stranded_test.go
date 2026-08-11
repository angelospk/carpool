package account_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Closing a driver's account has to reach the people holding seats in their car.
//
// Deleting an account cancels every departure of theirs that has not left, and
// stopped there. The bookings in those departures stayed 'confirmed': the
// passenger's app went on showing a trip with a meeting point and a time, for a
// driver who no longer exists, and nothing told them otherwise. The first they
// would have known is standing at the kerb.
//
// The suspension path does all of this — cancels each booking, sends each
// passenger a push, and puts their request back in the queue — because a
// suspension and a closure are the same act from the passenger's side. This is
// the same three things on the path nobody had walked.

func (w *world) bookingStatus(t *testing.T) string {
	t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM bookings WHERE occurrence_id = $1`, w.occurrenceID).Scan(&s); err != nil {
		t.Fatalf("reading the booking: %v", err)
	}
	return s
}

func (w *world) theRequest(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(),
		`SELECT request_id FROM bookings WHERE occurrence_id = $1`, w.occurrenceID).Scan(&id); err != nil {
		t.Fatalf("finding the request: %v", err)
	}
	return id
}

func TestClosingADriversAccountTellsThePeopleHoldingSeats(t *testing.T) {
	w := newWorld(t)

	if err := w.svc.Delete(context.Background(), w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := w.bookingStatus(t); got != "cancelled_by_driver" {
		t.Fatalf("the booking is %q after the driver closed their account, want cancelled_by_driver", got)
	}

	var pushes int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'push' AND payload->>'event' = 'booking_cancelled'
		  AND payload->>'recipient_id' = $1::text`, w.passengerID).Scan(&pushes); err != nil {
		t.Fatalf("counting pushes: %v", err)
	}
	if pushes != 1 {
		t.Fatalf("%d pushes to somebody whose ride was called off, want 1", pushes)
	}
}

// And they are handed back to the matcher, for the same reason as every other
// cancelled departure: the request that found this driver still says exactly
// where they are going and when.
func TestClosingADriversAccountLooksForAnotherDriverForItsPassengers(t *testing.T) {
	w := newWorld(t)
	requestID := w.theRequest(t)

	if err := w.svc.Delete(context.Background(), w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var status string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, requestID).Scan(&status); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if status != "open" {
		t.Fatalf("the passenger's request is %q after their driver left, want open", status)
	}

	var queued int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'ask_next' AND payload->>'request_id' = $1::text`, requestID).Scan(&queued); err != nil {
		t.Fatalf("counting queued asks: %v", err)
	}
	if queued != 1 {
		t.Fatalf("%d asks queued for a passenger whose driver left, want 1", queued)
	}
}
