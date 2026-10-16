package asks_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"carpool/internal/asks"
)

// A car passes, and the detour to the passenger is more than its driver
// takes. The passenger was told only "nobody has been asked yet" (QA
// 2026-10-16, U3). The request now says by how much it missed, with no name:
// the driver agreed to nothing.
func TestARequestSaysByHowMuchTheDetourMissed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	// A kilometre off the road, and a driver who will not leave it.
	mustExec(t, f, `UPDATE ride_requests SET status = 'open',
		origin = ST_SetSRID(ST_MakePoint(25.82, 40.86), 4326)`)
	mustExec(t, f, `UPDATE trip_occurrences SET detour_min = 0`)

	if _, err := f.matcher().AskNext(ctx, f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext = %v, want ErrNoCandidates", err)
	}
	var reason sql.NullString
	var need, limit sql.NullFloat64
	must(t, f.pool.QueryRow(ctx, `SELECT missed_reason, missed_need, missed_limit FROM ride_requests WHERE id = $1`,
		f.requestID).Scan(&reason, &need, &limit), "missed")
	if reason.String != "detour_min" || !(need.Float64 > 0) || limit.Float64 != 0 {
		t.Fatalf("missed = %v %v %v, want detour_min over a limit of 0", reason, need, limit)
	}

	// The driver takes more now, and is asked: the miss is no longer true.
	mustExec(t, f, `UPDATE trip_occurrences SET detour_min = 30`)
	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("AskNext: %v", err)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT missed_reason FROM ride_requests WHERE id = $1`,
		f.requestID).Scan(&reason), "missed after")
	if reason.Valid {
		t.Fatalf("missed_reason = %q after a driver was asked", reason.String)
	}
}

// A driver who takes no stops at all is a different sentence from one whose
// limit is too short.
func TestARequestSaysTheDriverTakesNoStops(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	mustExec(t, f, `UPDATE trip_occurrences SET allow_stops = false`)

	if _, err := f.matcher().AskNext(ctx, f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext = %v, want ErrNoCandidates", err)
	}
	var reason sql.NullString
	must(t, f.pool.QueryRow(ctx, `SELECT missed_reason FROM ride_requests WHERE id = $1`,
		f.requestID).Scan(&reason), "missed")
	if reason.String != "no_stops" {
		t.Fatalf("missed_reason = %v, want no_stops", reason)
	}
}
