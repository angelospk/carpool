package match_test

import (
	"context"
	"testing"
	"time"

	"carpool/internal/match"
)

// A trip stops being a candidate once it is due to leave.
//
// The filter compared the departure window against the *request's* window, and
// a request's window is written once, when somebody asks for a ride. Asking is
// sequential and a driver has half an hour to answer, so a request that names a
// morning is still being matched hours into that morning, against a window that
// has not moved. Everything inside it that has already gone stayed eligible.
//
// What that looks like from the kerb: a passenger is offered, and a driver is
// asked about, a seat in a car that left twenty minutes ago.
//
// The boundary is depart_earliest and not depart_latest, because that is what
// the rest of the app already means by departed: it is the moment after which a
// pair may confirm the ride and rate each other, and the moment after which a
// driver may no longer call the trip off. A second definition here would be the
// kind of drift that produces a trip that has left for one rule and not for
// another. It costs the case of somebody joining a driver who is running late,
// which needs a real departure time to do safely rather than the near end of a
// window. Recorded in RISKS.md.

func TestGrossWillNotOfferATripThatIsDueToLeave(t *testing.T) {
	f := setup(t)
	// Departure window 07:00–08:00, which the default query spans.
	f.addOccurrence(t, corridorWKT)

	q := query()
	q.Now = time.Date(2026, time.July, 30, 7, 30, 0, 0, time.UTC)

	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a trip due to leave at 07:00 is still being offered at 07:30: %+v", got)
	}
}

// The other side of the line, so that the clause above cannot be satisfied by
// refusing everything.
func TestGrossStillOffersATripThatHasNotSetOff(t *testing.T) {
	f := setup(t)
	want := f.addOccurrence(t, corridorWKT)

	q := query()
	q.Now = time.Date(2026, time.July, 30, 6, 30, 0, 0, time.UTC)

	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if len(got) != 1 || got[0].OccurrenceID != want {
		t.Fatalf("a driver who has not set off yet was dropped: %+v", got)
	}
}

// The same line, on the path that runs the other way: a seat coming back on a
// trip wakes the people still waiting on that corridor. A passenger cancelling
// ten minutes after the car left releases a seat in it, and everybody waiting
// was then offered that seat.
func TestWaitingWillNotWakeAnyoneForATripThatIsDueToLeave(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t)

	q := waitingQuery(occurrenceID)
	q.Now = time.Date(2026, time.July, 30, 7, 30, 0, 0, time.UTC)

	got, err := match.Waiting(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	for _, id := range got {
		if id == requestID {
			t.Fatalf("a trip due to leave at 07:00 woke somebody at 07:30")
		}
	}
}
