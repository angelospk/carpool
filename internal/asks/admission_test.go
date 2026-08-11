package asks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
)

// otherPassenger adds a second person wanting the same journey, with a request
// open and no ask against it.
func (f *fixture) otherPassenger(phone string, seats int) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	var passengerID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Other', (SELECT id FROM regions LIMIT 1))
		RETURNING id`, phone).Scan(&passengerID), "other passenger")

	var requestID uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T04:00:00Z', '2026-07-30T06:00:00Z', $2, 'open', 'open')
		RETURNING id`, passengerID, seats).Scan(&requestID), "other request")

	return requestID
}

func (f *fixture) liveAsksOn(occurrenceID uuid.UUID) int {
	f.t.Helper()
	var n int
	must(f.t, f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM asks
		WHERE occurrence_id = $1 AND status = 'pending' AND expires_at > $2`,
		occurrenceID, f.clock.Now()).Scan(&n), "live asks")
	return n
}

// A driver is never asked about more seats than they have left.
//
// Without this, one newly published trip wakes every waiting request on the
// corridor at once and the driver gets a push for each. On a single-seat trip
// all but one of those offers is a promise nobody can keep: the first person to
// accept takes the seat and everyone else is told it went, having been told
// moments earlier that it was theirs.
//
// The bound is the seats rather than a fixed number of asks, because a fixed
// number is either too small for a full car or too large for the last seat. A
// three-seat trip can honestly carry three conversations at once; serialising
// them behind a thirty-minute answer window would take an hour and a half to
// fill a car leaving in two.
func TestAskNextWillNotOfferMoreSeatsThanRemain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 1`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	second := f.otherPassenger("+306900000003", 1)

	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("first AskNext: %v", err)
	}

	_, err := f.matcher().AskNext(ctx, second)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("second AskNext: %v, want the one free seat to be spoken for", err)
	}
	if n := f.liveAsksOn(f.occurrenceID); n != 1 {
		t.Fatalf("%d live asks on a one-seat trip, want 1", n)
	}
}

// The bound counts seats, not asks: a trip with room for three can be asked
// about by three people at once.
func TestAskNextOffersEveryRemainingSeatAtOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 3`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	second := f.otherPassenger("+306900000003", 1)
	third := f.otherPassenger("+306900000004", 1)

	for _, id := range []uuid.UUID{f.requestID, second, third} {
		if _, err := f.matcher().AskNext(ctx, id); err != nil {
			t.Fatalf("AskNext for %s: %v", id, err)
		}
	}

	if n := f.liveAsksOn(f.occurrenceID); n != 3 {
		t.Fatalf("%d live asks on a three-seat trip, want 3", n)
	}
}

// A party of two takes two of the three seats, so only one is left to offer.
func TestAskNextCountsThePartySizeAgainstTheSeats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 3`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open', seats = 2`)
	second := f.otherPassenger("+306900000003", 2)

	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("first AskNext: %v", err)
	}

	_, err := f.matcher().AskNext(ctx, second)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("second AskNext: %v, want two of three seats to be spoken for", err)
	}
}

// The seat check has to hold under concurrency, because that is the only way
// the wake path will ever exercise it: one published trip enqueues a wake per
// waiting request and the dispatcher runs them at once, deliberately unordered.
//
// The filter each worker ran saw the seat free. Only the recount under the
// occurrence lock can decide between them.
func TestConcurrentAsksCannotBothTakeTheLastSeat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 1`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	second := f.otherPassenger("+306900000003", 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []uuid.UUID{f.requestID, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.matcher().AskNext(ctx, id)
		}()
	}
	wg.Wait()

	var offered int
	for _, err := range errs {
		switch {
		case err == nil:
			offered++
		case errors.Is(err, asks.ErrNoCandidates):
		default:
			t.Fatalf("AskNext: %v, want either an offer or no candidates", err)
		}
	}
	if offered != 1 {
		t.Fatalf("%d of two racing requests were offered the one free seat, want 1", offered)
	}
	if n := f.liveAsksOn(f.occurrenceID); n != 1 {
		t.Fatalf("%d live asks on a one-seat trip, want 1", n)
	}
}

// An expired offer releases its seats. Nothing sweeps at the moment it lapses,
// so the check has to read the expiry rather than the status alone, or a trip
// stays unaskable until the sweeper next runs.
func TestAskNextReleasesTheSeatsOfALapsedOffer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 1`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	second := f.otherPassenger("+306900000003", 1)

	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("first AskNext: %v", err)
	}
	f.clock.Advance(asks.DefaultTTL + time.Minute)

	if _, err := f.matcher().AskNext(ctx, second); err != nil {
		t.Fatalf("second AskNext: %v, want the lapsed offer to free the seat", err)
	}
}
