package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/jobs"
)

// Nothing ever finished a ride request.
//
// Two places in the code already reason about a sweeper that marks a request
// expired once its window has passed. The reverse matcher says so, and the
// reconciler says so in the comment explaining why it reads the window rather
// than the status. There was no such sweeper. Requests sat 'open' until the
// retention janitor removed them two years later, and 'expired' was a state the
// schema allowed and the system could not reach.
//
// Neither filter would match one, so this is not somebody being offered a lift
// to last Tuesday. What it costs is that the status stops meaning anything: the
// account export shows a journey from last spring as still looking for a
// driver, and every future screen listing "your requests" would have to
// rediscover the window rule instead of reading the column.

func expiryFixture(t *testing.T) (*fixture, uuid.UUID) {
	t.Helper()
	f := newFixture(t)

	var passenger uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', '+306900007777', 'Επιβάτης',
		        (SELECT id FROM regions LIMIT 1)) RETURNING id`).Scan(&passenger); err != nil {
		t.Fatalf("seeding the passenger: %v", err)
	}
	return f, passenger
}

func (f *fixture) addRequestWindow(t *testing.T, passenger uuid.UUID, status string, end time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        $2::timestamptz - interval '2 hours', $2, 'open', $3)
		RETURNING id`, passenger, end, status).Scan(&id); err != nil {
		t.Fatalf("seeding a %s request: %v", status, err)
	}
	return id
}

func (f *fixture) requestStatus(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	return s
}

func TestARequestWhoseTimeHasPassedIsExpired(t *testing.T) {
	f, passenger := expiryFixture(t)
	ctx := context.Background()
	id := f.addRequestWindow(t, passenger, "open", f.clock.Now().Add(-time.Minute))

	n, err := jobs.NewRequestSweeper(f.pool, f.clock).Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired %d requests, want 1", n)
	}
	if got := f.requestStatus(t, id); got != "expired" {
		t.Fatalf("the request is %q, want expired", got)
	}
}

func TestARequestStillInItsWindowIsLeftAlone(t *testing.T) {
	f, passenger := expiryFixture(t)
	id := f.addRequestWindow(t, passenger, "open", f.clock.Now().Add(time.Hour))

	if _, err := jobs.NewRequestSweeper(f.pool, f.clock).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := f.requestStatus(t, id); got != "open" {
		t.Fatalf("the request is %q, want open: its window has not passed", got)
	}
}

// A request with a driver still deciding is not swept. The half hour an ask
// lives is short, and it ends by putting the request back to 'open' — where the
// next pass finds it. Taking it out from under a live offer would let a driver
// accept a ride whose request had been marked finished.
func TestARequestWithADriverDecidingIsLeftAlone(t *testing.T) {
	f, passenger := expiryFixture(t)
	id := f.addRequestWindow(t, passenger, "asking", f.clock.Now().Add(-time.Minute))

	if _, err := jobs.NewRequestSweeper(f.pool, f.clock).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := f.requestStatus(t, id); got != "asking" {
		t.Fatalf("the request is %q, want it left while a driver is deciding", got)
	}
}

// Settled ones stay settled. A matched request that ran last week must not be
// rewritten into "expired" — the ride happened.
func TestASettledRequestIsNotRewritten(t *testing.T) {
	f, passenger := expiryFixture(t)
	for _, status := range []string{"matched", "cancelled", "expired"} {
		id := f.addRequestWindow(t, passenger, status, f.clock.Now().Add(-time.Hour))
		if _, err := jobs.NewRequestSweeper(f.pool, f.clock).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := f.requestStatus(t, id); got != status {
			t.Fatalf("a %s request became %q", status, got)
		}
	}
}
