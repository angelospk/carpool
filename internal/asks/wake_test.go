package asks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/dispatch"
	"carpool/internal/outbox"
	"carpool/internal/push"
)

// silentSender stands in for the push provider. What reaches a phone is the
// dispatcher's business and is tested there; here the question is whether the
// ask gets written at all.
type silentSender struct{}

func (silentSender) Send(context.Context, push.Notification) error { return nil }

// drain runs the dispatcher until it has nothing left, so a wake and the
// ask_next messages it fans out both land in one call.
func (f *fixture) drain(t *testing.T) {
	t.Helper()
	svc := f.matcher()
	router := dispatch.NewRouter(f.pool, f.clock, silentSender{},
		func(ctx context.Context, requestID uuid.UUID) error {
			_, err := svc.AskNext(ctx, requestID)
			// Nobody to ask is an answer, not a failure. Returning it would
			// retry the message five more times and then dead-letter it.
			if errors.Is(err, asks.ErrNoCandidates) {
				return nil
			}
			return err
		})

	d := outbox.NewDispatcher(f.pool, f.clock, router)
	for range 5 {
		n, err := d.DispatchOnce(context.Background())
		if err != nil {
			t.Fatalf("DispatchOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("the dispatcher never settled")
}

func (f *fixture) asksFor(requestID uuid.UUID) int {
	f.t.Helper()
	var n int
	must(f.t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM asks WHERE request_id = $1`, requestID).Scan(&n), "asks for request")
	return n
}

// The whole point of the exercise, end to end.
//
// Somebody asks for a lift and nobody can take them, so their request sits
// open. Until now that was the end of it: the matcher only ever ran from the
// passenger's side, and a driver publishing the right trip an hour later was
// never introduced to them. The request would expire with a car going the same
// way, the same day, with a seat free.
func TestAWaitingRequestIsOfferedATripPublishedAfterwards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The request has been round the houses and found nobody.
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	mustExec(t, f, `UPDATE trip_occurrences SET status = 'cancelled'`)

	if n := f.asksFor(f.requestID); n != 0 {
		t.Fatalf("%d asks before the trip exists, want none", n)
	}

	// A driver publishes a trip along that corridor.
	mustExec(t, f, `UPDATE trip_occurrences SET status = 'scheduled'`)
	tx, err := f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "published", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")

	f.drain(t)

	if n := f.asksFor(f.requestID); n != 1 {
		t.Fatalf("%d asks after the trip was published, want the waiting passenger to have been offered it", n)
	}
	var status string
	must(t, f.pool.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1`, f.requestID).Scan(&status), "request status")
	if status != "asking" {
		t.Fatalf("request is %q, want it to be waiting on an answer", status)
	}
}

// A wake for a trip that has since been cancelled is finished business, not a
// failure. Retrying cannot bring the trip back, and dead-lettering would report
// a fault where there was none.
func TestAWakeForACancelledTripQuietlyDoesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	mustExec(t, f, `UPDATE trip_occurrences SET status = 'cancelled'`)

	tx, err := f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "published", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")

	f.drain(t)

	if n := f.asksFor(f.requestID); n != 0 {
		t.Fatalf("%d asks for a cancelled trip, want none", n)
	}
	var unsettled int
	must(t, f.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		WHERE kind = 'wake_waiting' AND done_at IS NULL`).Scan(&unsettled),
		"undelivered wakes")
	if unsettled != 0 {
		t.Fatalf("%d wakes left unfinished, want the cancelled trip to settle the message", unsettled)
	}
}

// Declining frees the seat that offer was holding, and the next person waiting
// gets it.
//
// The seat cap makes this necessary rather than merely nice. With only one
// offer allowed on a one-seat trip, everybody behind the first is turned away,
// and if nothing wakes them when that offer ends the queue never moves: the
// trip has a free seat, several people want it, and none of them is ever asked.
func TestDecliningAnOfferPassesTheSeatToTheNextInLine(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 1`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	second := f.otherPassenger("+306900000003", 1)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	must(t, err, "first AskNext")
	if n := f.asksFor(second); n != 0 {
		t.Fatalf("%d asks for the second passenger while the seat was spoken for, want none", n)
	}

	must(t, f.matcher().Decline(ctx, askID, f.driverID), "Decline")
	f.drain(t)

	if n := f.asksFor(second); n != 1 {
		t.Fatalf("%d asks for the next in line after the seat was given up, want 1", n)
	}
}

// Cancelling a booking gives the seat back, and the trip is offered to whoever
// was told there was no room on it.
func TestReturningASeatOffersTheTripToSomebodyWaiting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 0`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	// Nobody can be offered a full trip.
	tx, err := f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "published", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")
	f.drain(t)
	if n := f.asksFor(f.requestID); n != 0 {
		t.Fatalf("%d asks on a full trip, want none", n)
	}

	// A seat comes back.
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 1`)
	tx, err = f.pool.Begin(ctx)
	must(t, err, "begin")
	must(t, outbox.Wake(ctx, tx, f.occurrenceID, "seat_returned", f.clock.Now()), "wake")
	must(t, tx.Commit(ctx), "commit")
	f.drain(t)

	if n := f.asksFor(f.requestID); n != 1 {
		t.Fatalf("%d asks after the seat came back, want the waiting passenger to be offered it", n)
	}
}
