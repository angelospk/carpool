package asks_test

import (
	"context"
	"errors"
	"testing"

	"carpool/internal/account"
	"carpool/internal/asks"
	"carpool/internal/outbox"
)

// Closing an account is the same shape of problem as being suspended, and it is
// worth its own test rather than a note saying it is covered: deletion
// anonymises the row rather than removing it, so every request, ask and trip
// that pointed at a person still points at one. What changes is the status.
//
// That is why every check reads status = 'active' rather than <> 'suspended'.
// The second spelling passes every suspension test and lets a deleted account
// straight back into the matcher, which is the more serious of the two: there is
// nobody left to answer at all.
func TestNobodyIsMatchedWithAClosedAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A live offer with them in it, and a request of theirs still looking.
	askID := f.askID
	requestID := f.requestID

	if err := account.NewService(f.pool, f.clock).Delete(ctx, f.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var status string
	must(t, f.pool.QueryRow(ctx,
		`SELECT status FROM users WHERE id = $1`, f.passengerID).Scan(&status), "reading the account")
	if status == "active" {
		t.Fatalf("a closed account is still %q", status)
	}

	// The offer is off the driver's phone, and a stale tap on it does nothing.
	if got := f.askStatus(askID); got != "cancelled" {
		t.Fatalf("the offer is %q after the account closed, want cancelled", got)
	}
	if _, err := f.service().Accept(ctx, askID, f.driverID); err == nil {
		t.Fatal("a driver accepted a ride for a closed account")
	}

	// And nothing new is made for them either, from either direction.
	mustExec(t, f, `UPDATE ride_requests SET status = 'open' WHERE id = $1`, requestID)
	if _, err := f.matcher().AskNext(ctx, requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext error = %v, want ErrNoCandidates for a closed account", err)
	}

	mustExec(t, f, `DELETE FROM asks`)
	tx, err := f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "published", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")
	f.drain(t)

	var offers int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM asks WHERE request_id = $1`, requestID).Scan(&offers), "counting offers")
	if offers != 0 {
		t.Fatalf("%d drivers were offered a closed account, want none", offers)
	}

	var bookings int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookings), "counting bookings")
	if bookings != 0 {
		t.Fatalf("%d bookings involve a closed account, want none", bookings)
	}
}
