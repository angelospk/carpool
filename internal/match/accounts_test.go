package match_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// Both filters read the account, not only the row.
//
// These belong here rather than with the ask lifecycle, because the checks
// above them — the one in AskNext and the one holding both user rows inside the
// transaction — catch a suspended person first, and a test written up there
// passes with the SQL below deleted. That was measured: the whole suspension
// suite stayed green with both joins removed.
//
// So this is the layer where the joins are the subject. What they are for is
// the day the cascade is short a case: a state added later, a partial failure,
// a row corrected by hand. The matcher then declines to introduce two people
// rather than trusting that somebody else remembered.

func suspend(t *testing.T, f *fixture, userID uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET status = 'suspended' WHERE id = $1`, userID); err != nil {
		t.Fatalf("suspending: %v", err)
	}
}

func TestGrossSkipsASuspendedDriver(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT)
	suspend(t, f, f.driverID)

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a suspended driver is still being offered rides: %+v", got)
	}
}

// Deleting anonymises rather than removes, so the trip and the template survive
// with a driver nobody can reach. 'active' is what excludes it; anything
// spelled as "not suspended" would let this one straight through.
func TestGrossSkipsADriverWhoseAccountIsClosed(t *testing.T) {
	f := setup(t)
	f.addOccurrence(t, corridorWKT)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET status = 'deleted' WHERE id = $1`, f.driverID); err != nil {
		t.Fatalf("closing the account: %v", err)
	}

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a closed account is still being offered rides: %+v", got)
	}
}

func TestWaitingSkipsASuspendedPassenger(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t)

	var passengerID uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT passenger_id FROM ride_requests WHERE id = $1`, requestID).Scan(&passengerID); err != nil {
		t.Fatalf("finding the passenger: %v", err)
	}
	suspend(t, f, passengerID)

	if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
		t.Fatalf("a driver is being offered a suspended passenger (%s)", requestID)
	}
}

func TestWaitingSkipsASuspendedDriver(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	f.addRequest(t)
	suspend(t, f, f.driverID)

	if got := waiting(t, f, occurrenceID); len(got) != 0 {
		t.Fatalf("a suspended driver was woken about %d waiting passengers", len(got))
	}
}
