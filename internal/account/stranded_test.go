package account_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/notices"
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

	// Queued, addressed to them, and in a shape the dispatcher can actually
	// deliver. The last of those is not pedantry: for months every one of these
	// was queued in a shape the dispatcher could not read, and a test that
	// counted rows saw nothing wrong while nobody was told anything.
	var raw []byte
	if err := w.pool.QueryRow(context.Background(), `
		SELECT payload FROM outbox
		WHERE kind = 'push' AND payload->>'recipient_id' = $1::text`,
		w.passengerID).Scan(&raw); err != nil {
		t.Fatalf("finding the push to somebody whose ride was called off: %v", err)
	}

	var p notices.Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("the queued notice is not a notice: %v", err)
	}
	if p.Event != notices.BookingCancelledForPassenger {
		t.Errorf("they are told %q", p.Event)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("the queued notice cannot be delivered: %v", err)
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

	// The closure records that the seat was lost; the dispatcher reopens the
	// request and asks the next driver, where the passenger's rows can be taken
	// in the ordinary order. What it does with each state the passenger could
	// have moved to is asserted where that decision is made.
	var queued int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'reopen_request' AND payload->>'request_id' = $1::text`, requestID).Scan(&queued); err != nil {
		t.Fatalf("counting queued reopens: %v", err)
	}
	if queued != 1 {
		t.Fatalf("%d recovery events for a passenger whose driver left, want 1", queued)
	}
}
