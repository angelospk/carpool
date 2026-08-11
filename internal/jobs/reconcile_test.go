package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/clock"
	"carpool/internal/jobs"
)

// openRequest inserts somebody still looking for a lift.
func openRequest(t *testing.T, f *fixture, status string, windowEnd time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var passengerID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', '+3069' || lpad((floor(random()*100000000))::bigint::text, 8, '0'),
		        'Passenger', (SELECT id FROM regions LIMIT 1))
		RETURNING id`).Scan(&passengerID); err != nil {
		t.Fatalf("seeding passenger: %v", err)
	}

	var requestID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
			seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			$2, $3, 1, 'open', $4)
		RETURNING id`, passengerID, windowEnd.Add(-2*time.Hour), windowEnd, status).Scan(&requestID); err != nil {
		t.Fatalf("seeding request: %v", err)
	}
	return requestID
}

func queuedAsks(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&n); err != nil {
		t.Fatalf("counting queued asks: %v", err)
	}
	return n
}

// The safety net. Whatever the wakes missed, this finds.
func TestReconcilerQueuesAnotherLookAtWaitingRequests(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	c := clock.NewFake(now)

	openRequest(t, f, "open", now.Add(6*time.Hour))
	openRequest(t, f, "open", now.Add(8*time.Hour))

	n, err := jobs.NewReconciler(f.pool, c).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Fatalf("queued %d, want both waiting requests", n)
	}
	if got := queuedAsks(t, f); got != 2 {
		t.Fatalf("%d ask_next messages, want 2", got)
	}
}

// Somebody already in conversation with a driver is left alone, and so is
// somebody whose journey has been and gone.
func TestReconcilerLeavesSettledAndLapsedRequestsAlone(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	c := clock.NewFake(now)

	openRequest(t, f, "asking", now.Add(6*time.Hour))
	openRequest(t, f, "matched", now.Add(6*time.Hour))
	// Still marked open because nothing has swept it yet, but the window has
	// closed: the journey is in the past.
	openRequest(t, f, "open", now.Add(-time.Hour))

	n, err := jobs.NewReconciler(f.pool, c).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 0 {
		t.Fatalf("queued %d, want none of these disturbed", n)
	}
}

// Two passes inside one interval agree rather than doubling the work; an hour
// later is a genuinely new look.
func TestReconcilerDoesNotQueueTheSameRequestTwiceInOnePass(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	c := clock.NewFake(now)
	ctx := context.Background()

	openRequest(t, f, "open", now.Add(24*time.Hour))

	if _, err := jobs.NewReconciler(f.pool, c).Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	c.Advance(time.Minute)
	if _, err := jobs.NewReconciler(f.pool, c).Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := queuedAsks(t, f); got != 1 {
		t.Fatalf("%d ask_next messages after two passes in one interval, want 1", got)
	}

	c.Advance(jobs.ReconcileInterval)
	if _, err := jobs.NewReconciler(f.pool, c).Run(ctx); err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if got := queuedAsks(t, f); got != 2 {
		t.Fatalf("%d ask_next messages after the next interval, want a fresh look", got)
	}
}
