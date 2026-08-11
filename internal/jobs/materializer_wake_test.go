package jobs_test

import (
	"context"
	"testing"
)

func wakes(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&n); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	return n
}

// Every trip the materializer creates asks who has been waiting for it.
//
// This is the trigger MATCHING.md specified from the beginning and nothing
// implemented. It is asserted here, at the site, because the reverse matching
// works perfectly in a test that queues the wake by hand — which is precisely
// what would keep passing after somebody removed the call.
func TestMaterializingATripAsksWhoWasWaiting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if n := wakes(t, f); n != 0 {
		t.Fatalf("%d wakes before materializing, want none", n)
	}

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	created := len(f.serviceDates())
	if created == 0 {
		t.Fatal("the fixture materialized nothing, so this test proves nothing")
	}
	if n := wakes(t, f); n != created {
		t.Fatalf("%d wakes for %d trips, want one each", n, created)
	}
}

// A second pass that changes nothing wakes nobody. The conflict clause declines
// to touch a row that is already right, and a wake per trip per run would put
// every open request on the corridor back through the matcher every fifteen
// minutes for as long as it stayed open.
func TestAMaterializerPassThatChangesNothingWakesNobody(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	before := wakes(t, f)

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if after := wakes(t, f); after != before {
		t.Fatalf("%d wakes after an idle pass, want the %d from the first", after, before)
	}
}
