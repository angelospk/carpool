package bookings_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/notices"
)

// What a cancellation says, and which of the two people it is said to.
//
// Counting the queued rows is not enough, and for months that was all anybody
// counted: every one of these notices was queued in a shape the dispatcher
// could not read, so nobody was ever told their ride was off, and every test
// went on passing. What has to hold is that the right person is named and that
// what reaches them is true from where they are standing — a passenger has lost
// a ride, a driver has a seat back, and one sentence cannot be both.
func TestACancellationTellsTheOtherSideWhatHappenedToThem(t *testing.T) {
	t.Run("a passenger giving back a seat tells the driver", func(t *testing.T) {
		w := newWorld(t)
		if err := w.svc.Cancel(context.Background(), w.fromAsk, w.passengerID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		assertNotice(t, w.pool, notices.BookingCancelledForDriver, w.driverID)
	})

	t.Run("a driver dropping a passenger tells the passenger", func(t *testing.T) {
		w := newWorld(t)
		if err := w.svc.Cancel(context.Background(), w.fromAsk, w.driverID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		assertNotice(t, w.pool, notices.BookingCancelledForPassenger, w.passengerID)
	})

	t.Run("calling off the whole departure tells everyone aboard", func(t *testing.T) {
		w := newWorld(t)
		if err := w.svc.CancelOccurrence(context.Background(), w.occurrenceID, w.driverID); err != nil {
			t.Fatalf("CancelOccurrence: %v", err)
		}
		assertNotice(t, w.pool, notices.BookingCancelledForPassenger, w.passengerID)
	})
}

// assertNotice reads the one queued push and checks it is addressed, worded and
// shaped for delivery.
func assertNotice(t *testing.T, pool *pgxpool.Pool, want notices.Event, to uuid.UUID) {
	t.Helper()

	var raw []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT payload FROM outbox WHERE kind = 'push'`).Scan(&raw); err != nil {
		t.Fatalf("finding the queued notice: %v", err)
	}
	var p notices.Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("the queued notice is not a notice: %v", err)
	}
	if p.Event != want {
		t.Errorf("the notice says %q, want %q", p.Event, want)
	}
	if p.RecipientID != to {
		t.Errorf("it is addressed to %s, want %s", p.RecipientID, to)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("it could not be delivered: %v", err)
	}
}
