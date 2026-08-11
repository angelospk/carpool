package asks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// Blocking somebody has to reach the offer already sitting on their phone.
//
// The matcher will not introduce two people who have blocked each other, and
// that was the whole of it. But an ask is written at one moment and answered at
// another: the offer goes out, one of them blocks the other, and half an hour
// later the driver taps accept. Blocking writes a row and nothing else, so
// nothing between that tap and the booking ever looked again.
//
// That is the one outcome blocking exists to prevent. The two of them get a
// seat in the same car, each other's meeting point, and a chat channel.
//
// It is refused as ErrAskNotPending rather than as anything that names a block,
// for the same reason the app never shows one: whoever is on the other side
// learns that the offer went away, and nothing more.

func TestAcceptRefusesWhenThePassengerHasBlockedTheDriver(t *testing.T) {
	f := newFixture(t)
	assertBlockStopsAccept(t, f, f.passengerID, f.driverID)
}

// The other direction. A driver who blocked somebody may not be handed them by
// an offer that predates the block either.
func TestAcceptRefusesWhenTheDriverHasBlockedThePassenger(t *testing.T) {
	f := newFixture(t)
	assertBlockStopsAccept(t, f, f.driverID, f.passengerID)
}

func assertBlockStopsAccept(t *testing.T, f *fixture, blocker, blocked uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	mustExec(t, f, `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blocker, blocked)

	seatsBefore := f.seatsFree()
	bookingID, err := f.service().Accept(ctx, f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskNotPending) {
		t.Fatalf("Accept: %v, want ErrAskNotPending", err)
	}
	if bookingID != uuid.Nil {
		t.Fatalf("booking %s puts two people who blocked each other in one car", bookingID)
	}
	if got := f.seatsFree(); got != seatsBefore {
		t.Fatalf("seats went from %d to %d for a booking that must not exist", seatsBefore, got)
	}
	var bookings int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE occurrence_id = $1`, f.occurrenceID).Scan(&bookings), "counting bookings")
	if bookings != 0 {
		t.Fatalf("%d bookings between two people who blocked each other", bookings)
	}
}

// And the ordinary pair still ride together, so the check above cannot be
// satisfied by refusing everybody.
func TestAcceptStillWorksWhenNobodyHasBlockedAnybody(t *testing.T) {
	f := newFixture(t)

	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
}
