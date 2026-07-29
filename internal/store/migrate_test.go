package store_test

import (
	"context"
	"sync"
	"testing"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

func TestMigrateCreatesTheSchema(t *testing.T) {
	pool := testdb.Fresh(t)
	ctx := context.Background()

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{
		"regions", "users", "driver_profiles", "otp_codes", "refresh_tokens",
		"push_tokens", "route_templates", "route_schedules", "trip_occurrences",
		"ride_requests", "asks", "bookings", "ratings", "trip_messages",
		"blocks", "reports", "outbox",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			                WHERE table_schema = 'public' AND table_name = $1)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("checking for %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %q was not created", table)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testdb.Fresh(t)
	ctx := context.Background()

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate should be a no-op, got: %v", err)
	}
}

// Every API instance runs migrations at boot, so two starting at once must not
// race into a duplicate-object error.
func TestConcurrentMigratorsBothSucceed(t *testing.T) {
	pool := testdb.Fresh(t)
	ctx := context.Background()

	const runners = 4
	errs := make([]error, runners)
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release together to maximise contention
			errs[i] = store.Migrate(ctx, pool)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("runner %d failed: %v", i, err)
		}
	}

	// Every migration must be recorded exactly once, however many ran at the
	// same time: a duplicate row means two runners applied the same file.
	var applied, distinct int
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT version_id) FROM goose_db_version WHERE version_id > 0`).
		Scan(&applied, &distinct); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	want := len(store.MigrationVersions())
	if applied != want || distinct != want {
		t.Fatalf("%d migration rows (%d distinct), want %d of each", applied, distinct, want)
	}
}
