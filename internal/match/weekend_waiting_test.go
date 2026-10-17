package match_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// makeWeekend turns an ordinary request into one a weekend plan made, which the
// matcher never asks anybody about.
func (f *fixture) makeWeekend(t *testing.T, requestID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var planID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO weekend_plans (passenger_id, origin, destination, depart_dow, depart_time)
		SELECT passenger_id, origin, destination, 5, '15:00' FROM ride_requests WHERE id = $1
		RETURNING id`, requestID).Scan(&planID); err != nil {
		t.Fatalf("seeding weekend plan: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE ride_requests SET weekend_plan_id = $2, weekend_of = '2026-07-31', weekend_leg = 'out'
		WHERE id = $1`, requestID, planID); err != nil {
		t.Fatalf("marking request as weekend: %v", err)
	}
}

// A weekend request is never asked about, so waking for it would only spend one
// of the wake's few slots on a request AskNext then skips, and tell a driver
// there is somebody waiting who cannot be taken.
func TestWaitingLeavesWeekendRequestsOut(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))

	// Older than the ordinary ones, so with a limit it would take the slot.
	var weekendReqs []uuid.UUID
	for i := 0; i < 3; i++ {
		id := f.addRequest(t, withCreatedAt(time.Date(2026, time.July, 29, 8, 0, 0, 0, time.UTC)))
		f.makeWeekend(t, id)
		weekendReqs = append(weekendReqs, id)
	}
	ordinary := f.addRequest(t)

	q := waitingQuery(occ)
	q.Limit = 1
	got, err := match.Waiting(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(got) != 1 || got[0] != ordinary {
		t.Fatalf("Waiting = %v, want only the ordinary request %s (weekend: %v)", got, ordinary, weekendReqs)
	}
	for _, id := range weekendReqs {
		if slices.Contains(waiting(t, f, occ), id) {
			t.Errorf("weekend request %s was woken", id)
		}
	}
}

func TestDriveDemandDoesNotCountWeekendRequests(t *testing.T) {
	f := setup(t)
	occ := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85),
		withWindow(driveDeparts, driveDeparts.Add(30*time.Minute)))
	now := driveDeparts.Add(-10 * time.Minute)

	ready := func(o *requestOpts) {
		o.windowStart = now
		o.windowEnd = driveDeparts.Add(2 * time.Hour)
		o.createdAt = now
	}
	f.makeWeekend(t, f.addRequest(t, ready))
	n, err := match.DriveDemand(context.Background(), f.pool, occ, now)
	if err != nil || n != 0 {
		t.Fatalf("DriveDemand = %d, %v; a weekend request was counted as demand to nudge a driver with", n, err)
	}
	f.addRequest(t, ready)
	if n, err = match.DriveDemand(context.Background(), f.pool, occ, now); err != nil || n != 1 {
		t.Fatalf("DriveDemand = %d, %v; want only the ordinary request", n, err)
	}
}
