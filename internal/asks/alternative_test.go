package asks_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// sibling posts the same journey on another day as an alternative of root,
// with a pending ask on occurrenceID when that is not nil.
func (f *fixture) sibling(t *testing.T, root, occurrenceID uuid.UUID) (requestID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	status := "open"
	if occurrenceID != uuid.Nil {
		status = "asking"
	}
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind,
			status, alternative_of)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-31T04:00:00Z', '2026-07-31T06:00:00Z', 1, 'open', $2, $3)
		RETURNING id`, f.passengerID, status, root).Scan(&requestID), "sibling")
	if occurrenceID != uuid.Nil {
		mustExec(t, f, `
			INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
				suggested_price_eur, itinerary_version, status, expires_at)
			VALUES ($1, $2, 5, 2,
				ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
				2.50, 0, 'pending', $3)`, requestID, occurrenceID, f.clock.Now().Add(asks.DefaultTTL))
	}
	return requestID
}

func (f *fixture) requestStatus(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, id).Scan(&s), "request status")
	return s
}

// One journey offered on three days is carried once: a driver taking it on
// one of them withdraws the other two, their offers included, and the car
// that offer was put to is free for whoever waits behind it.
func TestAcceptingOneDayCancelsTheOtherDays(t *testing.T) {
	f := newFixture(t)
	other := f.anotherDeparture(t, 1)
	asked := f.sibling(t, f.requestID, other)
	waiting := f.sibling(t, f.requestID, uuid.Nil)
	unrelated, _ := f.newAsk("", f.passengerID)
	mustExec(t, f, `UPDATE asks SET status = 'expired' WHERE request_id = $1`, unrelated)
	mustExec(t, f, `DELETE FROM outbox`)

	svc := f.service()
	if _, err := svc.Accept(context.Background(), f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	for name, id := range map[string]uuid.UUID{"asked": asked, "waiting": waiting} {
		if got := f.requestStatus(t, id); got != "cancelled" {
			t.Errorf("the %s other day is %q, want cancelled", name, got)
		}
	}
	if got := f.requestStatus(t, unrelated); got == "cancelled" {
		t.Errorf("a separate journey was cancelled too")
	}
	var pending, wakes int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM asks WHERE request_id = $1 AND status = 'pending'`, asked).Scan(&pending), "asks")
	if pending != 0 {
		t.Errorf("the other day's offer is still pending")
	}
	must(t, f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox WHERE kind = 'wake_waiting' AND payload->>'occurrence_id' = $1`,
		other.String()).Scan(&wakes), "wakes")
	if wakes != 1 {
		t.Errorf("%d wakes for the car the withdrawn offer held, want 1", wakes)
	}
}

// Accepted on an extra day, the first day goes too: the days are one group,
// whichever of them the driver takes.
func TestAcceptingAnExtraDayCancelsTheFirst(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `UPDATE asks SET status = 'cancelled' WHERE id = $1`, f.askID)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open' WHERE id = $1`, f.requestID)
	other := f.anotherDeparture(t, 1)
	asked := f.sibling(t, f.requestID, other)
	third := f.sibling(t, f.requestID, uuid.Nil)
	var askID uuid.UUID
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT id FROM asks WHERE request_id = $1`, asked).Scan(&askID), "ask")

	svc := f.service()
	if _, err := svc.Accept(context.Background(), askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if got := f.requestStatus(t, f.requestID); got != "cancelled" {
		t.Errorf("the first day is %q, want cancelled", got)
	}
	if got := f.requestStatus(t, third); got != "cancelled" {
		t.Errorf("the third day is %q, want cancelled", got)
	}
}
