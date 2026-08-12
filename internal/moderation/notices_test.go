package moderation_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/notices"
)

// A suspension reaches both sides of the app, and they are not the same news.
//
// Suspending a driver strands their passengers; suspending a passenger hands a
// seat back to their driver. One event name covered both until it was noticed
// that no sentence is true for both of them — and, more to the point, that
// neither sentence was ever delivered at all: the notices were queued in a
// shape the dispatcher could not read and dead-lettered to the last one.
func TestASuspensionTellsEachSideTheirOwnHalfOfIt(t *testing.T) {
	t.Run("suspending the driver strands the passenger", func(t *testing.T) {
		w := newWorld(t)
		if err := w.svc.Suspend(context.Background(), w.driver, w.moderator, "ό,τι να 'ναι"); err != nil {
			t.Fatalf("Suspend: %v", err)
		}
		assertNotice(t, w, notices.BookingCancelledForPassenger, w.passenger)
	})

	t.Run("suspending the passenger gives the driver their seat back", func(t *testing.T) {
		w := newWorld(t)
		if err := w.svc.Suspend(context.Background(), w.passenger, w.moderator, "ό,τι να 'ναι"); err != nil {
			t.Fatalf("Suspend: %v", err)
		}
		assertNotice(t, w, notices.BookingCancelledForDriver, w.driver)
	})
}

func assertNotice(t *testing.T, w *world, want notices.Event, to uuid.UUID) {
	t.Helper()

	var raw []byte
	if err := w.pool.QueryRow(context.Background(),
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
