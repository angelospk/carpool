package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// Nothing may put somebody in a car that has gone.
//
// The matcher no longer offers a departure that is due to leave, which is where
// this starts, but a matcher check cannot hold a rule for half an hour. An ask
// is written at one moment and answered at another, and between those two the
// car leaves. So the offer is given a deadline it cannot outlive, and the accept
// path refuses one that somehow did.

// The deadline. An offer that outlives the departure is an offer the driver
// cannot honour, and thirty minutes of a passenger's request is held against it
// while the answer could only ever have been no.
func TestAnOfferNeverOutlivesTheCarItIsFor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	// Leaving in ten minutes, which is well inside the ordinary half hour.
	departs := f.clock.Now().Add(10 * time.Minute)
	mustExec(t, f, `UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		departs, departs.Add(30*time.Minute))
	mustExec(t, f, `UPDATE ride_requests SET window_start = $1, window_end = $2`,
		f.clock.Now(), departs.Add(time.Hour))

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	var expiresAt time.Time
	must(t, f.pool.QueryRow(ctx,
		`SELECT expires_at FROM asks WHERE id = $1`, askID).Scan(&expiresAt), "reading the deadline")
	if !expiresAt.Equal(departs) {
		t.Fatalf("the offer runs to %s for a car leaving at %s", expiresAt, departs)
	}
}

// The ordinary case, so the deadline above cannot be satisfied by making every
// offer expire immediately.
func TestAnOfferOnATripTomorrowGetsTheWholeHalfHour(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	var expiresAt time.Time
	must(t, f.pool.QueryRow(ctx,
		`SELECT expires_at FROM asks WHERE id = $1`, askID).Scan(&expiresAt), "reading the deadline")
	if want := f.clock.Now().Add(asks.DefaultTTL); !expiresAt.Equal(want) {
		t.Fatalf("the offer runs to %s, want the full %s to %s", expiresAt, asks.DefaultTTL, want)
	}
}

// The backstop, seeded by hand.
//
// It has to be seeded by hand: with the deadline above in place the ordinary
// path cannot produce this row, and a test that went through the matcher would
// pass with this check deleted — the expiry check above it would refuse first,
// and the refusal would look identical. What is left is the row nothing rules
// out: one written before the deadline existed, or corrected by hand.
func TestAcceptWillNotPutSomebodyInACarThatHasLeft(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Past the departure, with an offer that is still live.
	mustExec(t, f, `UPDATE asks SET expires_at = $1 WHERE id = $2`,
		f.clock.Now().Add(72*time.Hour), f.askID)
	f.clock.Advance(17 * time.Hour) // the fixture trip leaves at 04:30 the next day

	seatsBefore := f.seatsFree()
	bookingID, err := f.service().Accept(ctx, f.askID, f.driverID)

	if !errors.Is(err, asks.ErrTripDeparted) {
		t.Fatalf("Accept: %v, want ErrTripDeparted", err)
	}
	if bookingID != uuid.Nil {
		t.Fatalf("a booking %s was made on a trip that had already left", bookingID)
	}
	if got := f.seatsFree(); got != seatsBefore {
		t.Fatalf("seats went from %d to %d on a trip that had already left", seatsBefore, got)
	}
	var bookings int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE occurrence_id = $1`, f.occurrenceID).Scan(&bookings), "counting bookings")
	if bookings != 0 {
		t.Fatalf("%d bookings on a trip that had already left", bookings)
	}
}
