package asks_test

import (
	"context"
	"errors"
	"testing"

	"carpool/internal/asks"
)

// A weekend request is never put to a driver by the matcher: the driver says
// yes first, from the publish screen, so nobody is asked a question the
// passenger has not yet seen the answer to (specs/WEEKEND.md).
//
// The same request, with a car passing that the matcher would otherwise ask,
// is the control: it is asked until it carries a plan's id.
func TestTheMatcherLeavesWeekendRequestsAlone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open' WHERE id = $1`, f.requestID)

	var planID string
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO weekend_plans (passenger_id, origin, destination, depart_dow, depart_time)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        5, '15:00')
		RETURNING id::text`, f.passengerID).Scan(&planID), "plan")
	mustExec(t, f, `UPDATE ride_requests SET weekend_plan_id = $2, weekend_of = '2026-07-31', weekend_leg = 'out'
		WHERE id = $1`, f.requestID, planID)

	if _, err := f.matcher().AskNext(ctx, f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext = %v, want ErrNoCandidates for a weekend request", err)
	}
	var pending int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM asks`).Scan(&pending), "asks")
	if pending != 0 {
		t.Errorf("%d asks made for a weekend request", pending)
	}

	// The same request without the plan is asked, so the answer above is
	// the plan's doing and not an empty road.
	mustExec(t, f, `UPDATE ride_requests SET weekend_plan_id = NULL, weekend_of = NULL, weekend_leg = NULL
		WHERE id = $1`, f.requestID)
	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("AskNext for the control request: %v", err)
	}
}

// «Άλλα δρομολόγια» lists the departures the matcher would try next. For a
// weekend request that is none: the matcher never tries it.
func TestAlternativesAreNoneForAWeekendRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open' WHERE id = $1`, f.requestID)

	got, err := f.service().WithRouter(straightLineRouter{}).Alternatives(ctx, f.requestID, f.passengerID, 5)
	if err != nil || len(got) == 0 {
		t.Fatalf("control: Alternatives = %v, %v; want a car for the ordinary request", got, err)
	}

	var planID string
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO weekend_plans (passenger_id, origin, destination, depart_dow, depart_time)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        5, '15:00')
		RETURNING id::text`, f.passengerID).Scan(&planID), "plan")
	mustExec(t, f, `UPDATE ride_requests SET weekend_plan_id = $2, weekend_of = '2026-07-31', weekend_leg = 'out'
		WHERE id = $1`, f.requestID, planID)

	got, err = f.service().WithRouter(straightLineRouter{}).Alternatives(ctx, f.requestID, f.passengerID, 5)
	if err != nil {
		t.Fatalf("Alternatives: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d alternatives listed for a weekend request, want none", len(got))
	}
}
