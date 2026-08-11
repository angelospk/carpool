package account_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// wakes counts the queued "who is waiting for this trip?" messages.
//
// Asserted at the site rather than only end to end, because the site is the
// easy part to lose: the reverse matching works perfectly in a test that queues
// the wake by hand, which is exactly what would keep passing after somebody
// removed the call.
func wakes(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&n); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	return n
}

// Somebody closing their account gives their seats back, and those trips may be
// what another passenger has been waiting on.
func TestDeletingAnAccountAsksWhoWasWaitingForTheFreedSeats(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if n := wakes(t, w.pool); n != 0 {
		t.Fatalf("%d wakes before the erasure, want none", n)
	}

	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := wakes(t, w.pool); n == 0 {
		t.Fatal("no wakes after deletion released the seats, want the freed trips to ask who was waiting")
	}
}
