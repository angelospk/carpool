package standing_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/notices"
)

// Each of the four things that can happen to a standing arrangement has to
// reach the person who did not do it, in a shape that can be delivered.
//
// None of them ever was. The notices went into the outbox describing an event,
// the dispatcher read a payload shape nothing wrote, and every one of them was
// dead-lettered as addressed to nobody. A driver was never told somebody wanted
// to travel with them every week, and a passenger was never told the answer.
func TestEachTurnOfAnArrangementReachesTheOtherPerson(t *testing.T) {
	t.Run("asking reaches the driver", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.svc.Repeat(context.Background(), w.booking, w.passengerID); err != nil {
			t.Fatalf("Repeat: %v", err)
		}
		assertNotice(t, w, notices.ArrangementRequested, w.driverID)
	})

	t.Run("approval reaches the passenger who asked", func(t *testing.T) {
		w := newWorld(t)
		id := w.repeat(t)
		if err := w.svc.Approve(context.Background(), id, w.driverID); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		assertNotice(t, w, notices.ArrangementApproved, w.passengerID)
	})

	t.Run("a refusal reaches the passenger who asked", func(t *testing.T) {
		w := newWorld(t)
		id := w.repeat(t)
		if err := w.svc.Decline(context.Background(), id, w.driverID); err != nil {
			t.Fatalf("Decline: %v", err)
		}
		assertNotice(t, w, notices.ArrangementDeclined, w.passengerID)
	})

	t.Run("ending it reaches whoever did not end it", func(t *testing.T) {
		w := newWorld(t)
		id := w.repeat(t)
		if err := w.svc.Approve(context.Background(), id, w.driverID); err != nil {
			t.Fatalf("Approve: %v", err)
		}
		if err := w.svc.Cancel(context.Background(), id, w.passengerID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		assertNotice(t, w, notices.ArrangementCancelled, w.driverID)
	})
}

// repeat makes the arrangement the case under test then acts on.
func (w *world) repeat(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := w.svc.Repeat(context.Background(), w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	return id
}

func assertNotice(t *testing.T, w *world, want notices.Event, to uuid.UUID) {
	t.Helper()

	var raw []byte
	if err := w.pool.QueryRow(context.Background(),
		`SELECT payload FROM outbox WHERE kind = 'push' ORDER BY id DESC LIMIT 1`).Scan(&raw); err != nil {
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
