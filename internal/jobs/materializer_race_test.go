package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// An ask committed while a run waits for the departure's row. The run's
// statement had started before the ask existed, and Postgres rechecks only
// the locked row after the wait, not the asks under it: the departure was
// retimed under a driver who was reading the old time (review, 2026-10-17).
func TestARunWaitingForADepartureSeesTheAskThatHeldIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	var occurrenceID uuid.UUID
	var before time.Time
	if err := f.pool.QueryRow(ctx,
		`SELECT id, depart_earliest FROM trip_occurrences WHERE service_date = '2026-07-13'`).
		Scan(&occurrenceID, &before); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE route_schedules SET depart_earliest = make_time(9,0,0), depart_latest = make_time(9,30,0)
		WHERE template_id = $1`, f.templateID); err != nil {
		t.Fatalf("changing the departure window: %v", err)
	}

	// What createAsk does: the departure first, then the ask.
	asker, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = asker.Rollback(ctx) }()
	if _, err := asker.Exec(ctx, `SELECT 1 FROM trip_occurrences WHERE id = $1 FOR UPDATE`, occurrenceID); err != nil {
		t.Fatalf("locking the departure: %v", err)
	}
	var requestID uuid.UUID
	if err := asker.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326), ST_SetSRID(ST_MakePoint(25.98, 40.85), 4326),
		        now(), now() + interval '2 hours', 'open', 'asking')
		RETURNING id`, f.passengerID).Scan(&requestID); err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := asker.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(25.98, 40.85), 4326), 2.50, 0, 'pending', now() + interval '30 minutes')`,
		requestID, occurrenceID); err != nil {
		t.Fatalf("ask: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- f.materializer().Run(ctx) }()
	waitForLockWait(t, f, done)
	if err := asker.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	var after time.Time
	if err := f.pool.QueryRow(ctx,
		`SELECT depart_earliest FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&after); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("a departure with a pending ask was retimed from %v to %v", before.UTC(), after.UTC())
	}
}

// waitForLockWait returns once a session is waiting on a lock, which is the
// run waiting for the departure.
func waitForLockWait(t *testing.T, f *fixture, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("the run finished without waiting: %v", err)
		default:
		}
		var n int
		if err := f.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			 WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run never waited for the departure")
}
