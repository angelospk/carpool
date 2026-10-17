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

// QA 2026-10-17 T F3, U F5: a car passes and nobody is asked for a reason that
// is not the detour. The passenger was told "nobody has been asked yet", or
// even "no driver on this road". Each reason is now said.
func TestARequestSaysWhyTheCarThatPassesWasNotAsked(t *testing.T) {
	missed := func(t *testing.T, f *fixture) (sql.NullString, sql.NullFloat64, sql.NullFloat64) {
		t.Helper()
		if _, err := f.matcher().AskNext(context.Background(), f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
			t.Fatalf("AskNext = %v, want ErrNoCandidates", err)
		}
		var reason sql.NullString
		var need, limit sql.NullFloat64
		must(t, f.pool.QueryRow(context.Background(),
			`SELECT missed_reason, missed_need, missed_limit FROM ride_requests WHERE id = $1`,
			f.requestID).Scan(&reason, &need, &limit), "missed")
		return reason, need, limit
	}
	open := func(t *testing.T) *fixture {
		f := newFixture(t)
		mustExec(t, f, `DELETE FROM asks`)
		mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
		return f
	}

	t.Run("full", func(t *testing.T) {
		f := open(t)
		mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 0`)
		if r, _, _ := missed(t, f); r.String != "full" {
			t.Fatalf("missed_reason = %v, want full", r)
		}
	})
	t.Run("party cap", func(t *testing.T) {
		f := open(t)
		mustExec(t, f, `UPDATE trip_occurrences SET seats_total = 4, seats_free = 4`)
		svc := f.service()
		for _, phone := range []string{"+306900000041", "+306900000042"} {
			_, _, ask := f.placedAsk(phone, 1, onRoadFrom, onRoadTo)
			if _, err := svc.Accept(context.Background(), ask, f.driverID); err != nil {
				t.Fatalf("accept: %v", err)
			}
		}
		if r, _, _ := missed(t, f); r.String != "party_cap" {
			t.Fatalf("missed_reason = %v, want party_cap", r)
		}
	})
	t.Run("luggage", func(t *testing.T) {
		f := open(t)
		mustExec(t, f, `UPDATE ride_requests SET luggage = 'bulky', luggage_count = 1`)
		mustExec(t, f, `UPDATE trip_occurrences SET luggage = 'suitcase'`)
		if r, _, _ := missed(t, f); r.String != "luggage" {
			t.Fatalf("missed_reason = %v, want luggage", r)
		}
	})
	t.Run("off the corridor", func(t *testing.T) {
		f := open(t)
		// About 7.8 km north of the road; the driver stops within 2.
		mustExec(t, f, `UPDATE ride_requests SET origin = ST_SetSRID(ST_MakePoint(25.82, 40.92), 4326)`)
		r, need, limit := missed(t, f)
		if r.String != "off_corridor" {
			t.Fatalf("missed_reason = %v, want off_corridor", r)
		}
		if need.Float64 < 7 || need.Float64 > 9 || limit.Float64 != 2 {
			t.Errorf("need %v km, limit %v km; want about 7.8 and 2", need, limit)
		}
	})
	t.Run("nobody near", func(t *testing.T) {
		f := open(t)
		mustExec(t, f, `UPDATE ride_requests SET origin = ST_SetSRID(ST_MakePoint(25.82, 41.10), 4326)`)
		if r, _, _ := missed(t, f); r.Valid {
			t.Fatalf("missed_reason = %v for a car 28 km away, want none", r)
		}
	})
}
