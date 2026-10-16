package asks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/asks"
	"carpool/internal/match"
)

// Two requests from one passenger for the same hour, matched at the same
// moment. The stage-1 filter skips a trip that overlaps one the passenger is
// already being asked about, but both searches read before either writes, so
// the filter alone lets both through. createAsk holds the passenger's row, and
// rechecking under it is what leaves at most one live offer (QA 2026-10-16,
// Codex review).
func TestTwoRequestsFromOnePassengerCannotBothBeOfferedTheSameHour(t *testing.T) {
	for attempt := range 12 {
		f := newFixture(t)
		ctx := context.Background()
		mustExec(t, f, `DELETE FROM asks`)
		mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
		// The same route twice, the same morning: two trips a passenger could
		// each be offered on their own.
		mustExec(t, f, `
			WITH tpl AS (
				INSERT INTO route_templates
				SELECT (jsonb_populate_record(t, jsonb_build_object('id', gen_random_uuid()))).*
				  FROM route_templates t
				RETURNING id)
			INSERT INTO trip_occurrences
			SELECT (jsonb_populate_record(o, jsonb_build_object(
			          'id', gen_random_uuid(), 'template_id', (SELECT id FROM tpl)))).*
			  FROM trip_occurrences o`)

		var second uuid.UUID
		must(t, f.pool.QueryRow(ctx, `
			INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
			SELECT passenger_id, origin, destination, window_start, window_end, seats, kind, 'open'
			  FROM ride_requests WHERE id = $1
			RETURNING id`, f.requestID).Scan(&second), "second request")

		var wg sync.WaitGroup
		for _, id := range []uuid.UUID{f.requestID, second} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = f.matcher().AskNext(ctx, id)
			}()
		}
		wg.Wait()

		var live int
		must(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM asks a JOIN ride_requests rr ON rr.id = a.request_id
			 WHERE rr.passenger_id = $1 AND a.status = 'pending'`, f.passengerID).Scan(&live),
			"counting live offers")
		if live == 0 {
			t.Fatalf("attempt %d: neither request was offered to anybody", attempt)
		}
		if live > 1 {
			t.Fatalf("attempt %d: one passenger holds %d live offers for the same hour", attempt, live)
		}
	}
}

// sameHourElsewhereSQL gives the passenger a confirmed seat on another trip at
// the same hour as the fixture's: the same route published a second time.
const sameHourElsewhereSQL = `
		WITH tpl AS (
			INSERT INTO route_templates
			SELECT (jsonb_populate_record(t, jsonb_build_object('id', gen_random_uuid()))).*
			  FROM route_templates t
			RETURNING id),
		occ AS (
			INSERT INTO trip_occurrences
			SELECT (jsonb_populate_record(o, jsonb_build_object(
			          'id', gen_random_uuid(), 'template_id', (SELECT id FROM tpl)))).*
			  FROM trip_occurrences o
			RETURNING id),
		req AS (
			INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
			SELECT passenger_id, origin, destination, window_start, window_end, seats, kind, 'matched'
			  FROM ride_requests
			RETURNING id, passenger_id, origin, destination),
		ask AS (
			INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			                  suggested_price_eur, itinerary_version, status, expires_at)
			SELECT req.id, occ.id, 0, 0, req.origin, req.destination, 2.00, 1, 'accepted', now()
			  FROM req, occ
			RETURNING id, request_id, occurrence_id)
		INSERT INTO bookings (occurrence_id, request_id, ask_id, passenger_id, seats, price_eur, pickup_point, dropoff_point)
		SELECT ask.occurrence_id, req.id, ask.id, req.passenger_id, 1, 2.00, req.origin, req.destination
		  FROM ask, req`

func (f *fixture) sameHourElsewhere(t *testing.T) {
	t.Helper()
	mustExec(t, f, sameHourElsewhereSQL)
}

// The other half of the race above, from the driver's side: an offer that was
// made before the passenger got a seat elsewhere for the same hour cannot be
// accepted after it (Codex review, round 2).
func TestAnOfferCannotBeAcceptedOnceThePassengerRidesElsewhereThatHour(t *testing.T) {
	f := newFixture(t)
	f.sameHourElsewhere(t)

	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); err == nil {
		t.Fatal("a driver accepted a passenger who already has a seat at that hour")
	}
	var seats int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM bookings WHERE occurrence_id = $1`, f.occurrenceID).Scan(&seats),
		"counting bookings")
	if seats != 0 {
		t.Fatalf("%d bookings on the trip, want 0", seats)
	}
}

// holdPassenger takes the passenger lock in a transaction of the test's own,
// standing for another request of the same passenger mid-admission.
func holdPassenger(t *testing.T, f *fixture) pgx.Tx {
	t.Helper()
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), match.PassengerLockSQL, f.passengerID); err != nil {
		t.Fatalf("taking the passenger lock: %v", err)
	}
	return tx
}

// blockedOn waits until n sessions in this test's database are waiting for a
// lock, and fails if done closes first: the call under test was meant to be
// waiting for a lock the test holds, and this asks the database rather than
// trusting a sleep.
func blockedOn(t *testing.T, f *fixture, n int, done <-chan struct{}, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("%s finished while the lock was held: it does not take it", what)
		default:
		}
		var waiting int
		must(t, f.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			 WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting),
			"reading lock waits")
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never waited for the lock", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Deterministic versions of the races. The test holds the passenger lock,
// starts the call, checks it waits, gives the passenger a seat at the same
// hour in that same transaction and commits. A call that takes the lock and
// rechecks after it must then refuse (Codex review).
func TestAcceptWaitsForThePassengerLockAndThenSeesTheSeatElsewhere(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tx := holdPassenger(t, f)

	done := make(chan struct{})
	var err error
	go func() { defer close(done); _, err = f.service().Accept(ctx, f.askID, f.driverID) }()
	blockedOn(t, f, 1, done, "Accept")

	if _, e := tx.Exec(ctx, sameHourElsewhereSQL); e != nil {
		t.Fatalf("seeding the other seat: %v", e)
	}
	if e := tx.Commit(ctx); e != nil {
		t.Fatalf("commit: %v", e)
	}
	<-done
	if err == nil {
		t.Fatal("Accept gave a second seat at the same hour")
	}
}

func TestAskNextWaitsForThePassengerLockAndThenSeesTheSeatElsewhere(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	tx := holdPassenger(t, f)

	done := make(chan struct{})
	var askErr error
	go func() { defer close(done); _, askErr = f.matcher().AskNext(ctx, f.requestID) }()
	blockedOn(t, f, 1, done, "AskNext")

	if _, e := tx.Exec(ctx, sameHourElsewhereSQL); e != nil {
		t.Fatalf("seeding the other seat: %v", e)
	}
	if e := tx.Commit(ctx); e != nil {
		t.Fatalf("commit: %v", e)
	}
	<-done
	if !errors.Is(askErr, asks.ErrNoCandidates) {
		t.Fatalf("AskNext: %v, want ErrNoCandidates", askErr)
	}
	var live int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM asks WHERE status = 'pending'`).Scan(&live),
		"counting live offers")
	if live != 0 {
		t.Fatalf("%d offers made for a passenger who has a seat at that hour", live)
	}
}

// A route retired while createAsk waited for its departure's lock: the read
// after the lock must see it (Codex review).
func TestAnOfferIsNotMadeOnARouteRetiredWhileWaitingForItsDeparture(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `SELECT 1 FROM trip_occurrences WHERE id = $1 FOR UPDATE`, f.occurrenceID); err != nil {
		t.Fatalf("locking the departure: %v", err)
	}

	done := make(chan struct{})
	var askErr error
	go func() { defer close(done); _, askErr = f.matcher().AskNext(ctx, f.requestID) }()
	blockedOn(t, f, 1, done, "AskNext")

	if _, err := tx.Exec(ctx, `UPDATE route_templates SET active = false`); err != nil {
		t.Fatalf("retiring: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	<-done
	if !errors.Is(askErr, asks.ErrNoCandidates) {
		t.Fatalf("AskNext: %v, want ErrNoCandidates", askErr)
	}
	var live int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM asks WHERE status = 'pending'`).Scan(&live),
		"counting live offers")
	if live != 0 {
		t.Fatalf("%d offers made on a route retired before the offer was written", live)
	}
}

// The expiry race itself. Accept passed its expiry check and waits for the
// passenger; the offer then lapses and the passenger's other request is
// matched, and waits too. Whichever goes first, the passenger ends with one
// seat or one live offer for that hour, never both (Codex review).
func TestAnOfferLapsingWhileAcceptedLeavesOneSeatOrOneOffer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Another trip at the same hour, nobody on it yet: what the other request
	// would be offered once the first offer lapses.
	mustExec(t, f, `
		WITH tpl AS (
			INSERT INTO route_templates
			SELECT (jsonb_populate_record(t, jsonb_build_object('id', gen_random_uuid()))).*
			  FROM route_templates t
			RETURNING id)
		INSERT INTO trip_occurrences
		SELECT (jsonb_populate_record(o, jsonb_build_object(
		          'id', gen_random_uuid(), 'template_id', (SELECT id FROM tpl)))).*
		  FROM trip_occurrences o`)
	var second uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
		SELECT passenger_id, origin, destination, window_start, window_end, seats, kind, 'open'
		  FROM ride_requests WHERE id = $1
		RETURNING id`, f.requestID).Scan(&second), "second request")

	tx := holdPassenger(t, f)
	accepted := make(chan struct{})
	go func() { defer close(accepted); _, _ = f.service().Accept(ctx, f.askID, f.driverID) }()
	blockedOn(t, f, 1, accepted, "Accept")

	f.clock.Advance(asks.DefaultTTL + time.Minute)
	asked := make(chan struct{})
	go func() { defer close(asked); _, _ = f.matcher().AskNext(ctx, second) }()
	blockedOn(t, f, 2, asked, "AskNext")

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	<-accepted
	<-asked

	var seats, live int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE passenger_id = $1 AND status = 'confirmed'`,
		f.passengerID).Scan(&seats), "counting seats")
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM asks a JOIN ride_requests rr ON rr.id = a.request_id
		 WHERE rr.passenger_id = $1 AND a.status = 'pending' AND a.expires_at > $2`,
		f.passengerID, f.clock.Now()).Scan(&live), "counting live offers")
	if seats+live > 1 {
		t.Fatalf("the passenger holds %d seats and %d live offers for the same hour", seats, live)
	}
}
