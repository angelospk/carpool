package asks_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// nextWeek adds another occurrence of the same route, seven days on, and
// returns it. "Ξανά" has to have somewhere to land.
func (f *fixture) nextWeek(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	must(t, f.pool.QueryRow(context.Background(), `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		SELECT template_id, vehicle_id, '2026-08-06', '2026-08-06T04:30:00Z', '2026-08-06T05:00:00Z',
			3, 3, 1, route_geom, route_km, route_min, pricing_mode
		FROM trip_occurrences WHERE id = $1
		RETURNING id`, f.occurrenceID).Scan(&id), "next week")
	return id
}

// completedBooking is the ride they already took, which is what "Ξανά" repeats.
func (f *fixture) completedBooking(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	bookingID, err := f.service().Accept(ctx, f.askID, f.driverID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE bookings SET status = 'completed' WHERE id = $1`, bookingID); err != nil {
		t.Fatalf("completing the booking: %v", err)
	}
	return bookingID
}

// One tap, and the driver they already rode with is the one asked — not
// whoever the matcher happens to like best this week.
func TestAgainAsksTheSameDriver(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	next := f.nextWeek(t)
	bookingID := f.completedBooking(t)

	requestID, askID, err := f.matcher().Again(ctx, bookingID, f.passengerID)
	if err != nil {
		t.Fatalf("Again: %v", err)
	}

	var occurrenceID uuid.UUID
	var kind string
	var target *uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		SELECT a.occurrence_id, r.kind, r.target_occurrence_id
		FROM asks a JOIN ride_requests r ON r.id = a.request_id
		WHERE a.id = $1`, askID).Scan(&occurrenceID, &kind, &target), "reading the ask")

	if occurrenceID != next {
		t.Errorf("the ask went to occurrence %v, want next week's %v", occurrenceID, next)
	}
	if kind != "targeted" {
		t.Errorf("request kind = %q, want targeted: this is a return to one driver, not a search", kind)
	}
	if target == nil || *target != next {
		t.Errorf("target_occurrence_id = %v, want %v", target, next)
	}
	if requestID == uuid.Nil {
		t.Error("no request was recorded")
	}
}

// A targeted request must not wander. If that driver's next trip is full, the
// answer is no — silently matching a stranger would not be the thing the
// passenger tapped.
func TestAgainDoesNotFallBackToAnotherDriver(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	next := f.nextWeek(t)
	bookingID := f.completedBooking(t)
	f.addSecondDriver(t)

	if _, err := f.pool.Exec(ctx,
		`UPDATE trip_occurrences SET seats_free = 0 WHERE id = $1`, next); err != nil {
		t.Fatalf("filling next week: %v", err)
	}

	_, _, err := f.matcher().Again(ctx, bookingID, f.passengerID)

	if err == nil {
		t.Fatal("Again found someone else to ask; the passenger asked for one driver")
	}
}

func TestOnlyThePassengerCanRepeatTheirRide(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.nextWeek(t)
	bookingID := f.completedBooking(t)

	if _, _, err := f.matcher().Again(ctx, bookingID, f.driverID); err == nil {
		t.Fatal("the driver repeated their passenger's ride")
	}
}
