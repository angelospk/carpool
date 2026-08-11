package asks_test

import (
	"context"
	"errors"
	"testing"

	"carpool/internal/asks"
	"carpool/internal/outbox"
)

// A suspension stops somebody using the app. It has never stopped the app using
// them.
//
// The cascade cancels their departures and gives back their seats, and the
// middleware reads users.status on every request so their own next tap is
// refused. Neither of those reaches the matcher, which knows about seats,
// windows, corridors and blocks, and has never heard of an account status. So a
// suspended passenger's open request stayed in the queue: a driver publishing a
// trip along that corridor was woken, offered them, and had a real seat taken by
// somebody who cannot open the app to arrange a meeting point, answer a message
// or turn up.
//
// The driver has no way to see any of this. From their side it is an ordinary
// passenger who never replies.

func (f *fixture) suspend(t *testing.T, who string) {
	t.Helper()
	id := f.passengerID
	if who == "driver" {
		id = f.driverID
	}
	mustExec(t, f, `UPDATE users SET status = 'suspended' WHERE id = $1`, id)
}

func TestASuspendedPassengerIsNotOfferedToDrivers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	f.suspend(t, "passenger")

	tx, err := f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "published", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")

	f.drain(t)

	if n := f.asksFor(f.requestID); n != 0 {
		t.Fatalf("%d drivers were offered a suspended passenger, want none", n)
	}
}

// The same from the other side. Asking on behalf of a suspended person is the
// same mistake whichever direction the matcher ran in.
func TestAskNextWillNotApproachADriverOnBehalfOfASuspendedPassenger(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	f.suspend(t, "passenger")

	_, err := f.matcher().AskNext(ctx, f.requestID)
	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext error = %v, want ErrNoCandidates for a suspended passenger", err)
	}
	if n := f.asksFor(f.requestID); n != 0 {
		t.Fatalf("%d asks were written for a suspended passenger, want none", n)
	}
}

// A suspended driver's departures are cancelled by the cascade, so the forward
// filter already skips them. This pins that it is true, because the filter reads
// the occurrence's status rather than the driver's, and the two are only kept in
// step by the cascade.
func TestASuspendedDriverIsNotAskedAboutNewPassengers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	f.suspend(t, "driver")

	_, err := f.matcher().AskNext(ctx, f.requestID)
	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext error = %v, want ErrNoCandidates for a suspended driver", err)
	}
}

// The last door: an ask that was live when the suspension landed. The offer is
// already sitting on the driver's phone, and a tap on it must not turn into a
// booking with somebody who has been suspended in the meantime.
func TestADriverCannotAcceptASuspendedPassengersAsk(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.suspend(t, "passenger")

	if _, err := f.service().Accept(ctx, f.askID, f.driverID); err == nil {
		t.Fatal("the driver accepted a ride for a suspended passenger")
	}

	var bookings int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings`).Scan(&bookings), "counting bookings")
	if bookings != 0 {
		t.Fatalf("%d bookings for a suspended passenger, want none", bookings)
	}
	if got := f.seatsFree(); got != 3 {
		t.Fatalf("seats_free = %d, want 3: no seat should have been taken", got)
	}
}
