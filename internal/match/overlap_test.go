package match_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// askAbout puts a pending offer to the driver on the occurrence, made for an
// earlier request of the passenger's: the request under test is a second one.
func askAbout(t *testing.T, f *fixture, occurrenceID, passengerID uuid.UUID, expiresAt time.Time) {
	t.Helper()
	earlier := f.addRequest(t, withRequestPassenger(passengerID), withRequestStatus("asking"))
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 0, 0, ST_SetSRID(ST_MakePoint(25.82, 40.851), 4326),
		        ST_SetSRID(ST_MakePoint(25.98, 40.851), 4326), 2.00, 1, 'pending', $3)`,
		earlier, occurrenceID, expiresAt); err != nil {
		t.Fatalf("asking: %v", err)
	}
}

func retire(t *testing.T, f *fixture, occurrenceID uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE route_templates SET active = false
		 WHERE id = (SELECT template_id FROM trip_occurrences WHERE id = $1)`, occurrenceID); err != nil {
		t.Fatalf("retiring: %v", err)
	}
}

func offered(got []match.Candidate, occurrenceID uuid.UUID) bool {
	for _, c := range got {
		if c.OccurrenceID == occurrenceID {
			return true
		}
	}
	return false
}

var farFuture = time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)

// Two requests from one passenger must not both be put to the same driver:
// together they held every seat on the trip (QA 2026-10-16, B2).
func TestAPassengerAlreadyBeingAskedAboutATripIsNotOfferedItAgain(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, corridorWKT)
	passenger := passengerOf(t, f, f.addRequest(t))
	askAbout(t, f, occurrenceID, passenger, farFuture)

	if offered(gross(t, f, passenger), occurrenceID) {
		t.Fatal("a passenger with a live offer on this trip was offered it again")
	}
}

// A second departure at the same hour (the same driver published the route
// twice, or another driver) is the same morning: a passenger who already has
// a seat or a live offer then is not offered another (QA 2026-10-16, B3).
func TestAPassengerIsNotOfferedATripThatOverlapsOneTheyHave(t *testing.T) {
	for _, held := range []string{"booking", "ask"} {
		t.Run(held, func(t *testing.T) {
			f := setup(t)
			first := f.addOccurrence(t, corridorWKT)
			second := f.addOccurrence(t, corridorWKT)
			passenger := passengerOf(t, f, f.addRequest(t))
			if held == "booking" {
				book(t, f, first, passenger, "confirmed")
			} else {
				askAbout(t, f, first, passenger, farFuture)
			}

			if offered(gross(t, f, passenger), second) {
				t.Fatalf("a passenger with a %s at the same hour was offered a second trip", held)
			}
		})
	}
}

// What a passenger had and no longer has blocks nothing: a lapsed offer, a
// cancelled seat, or a seat on a trip that is over long before this one.
func TestWhatNoLongerHoldsThePassengerDoesNotBlockAnOverlappingTrip(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	cases := map[string]func(f *fixture, first, passenger uuid.UUID){
		"lapsed ask": func(f *fixture, first, passenger uuid.UUID) {
			askAbout(t, f, first, passenger, now.Add(-time.Minute))
		},
		"cancelled booking": func(f *fixture, first, passenger uuid.UUID) {
			book(t, f, first, passenger, "cancelled_by_passenger")
		},
	}
	for name, hold := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			first := f.addOccurrence(t, corridorWKT)
			second := f.addOccurrence(t, corridorWKT)
			passenger := passengerOf(t, f, f.addRequest(t))
			hold(f, first, passenger)

			q := query()
			q.PassengerID = passenger
			q.Now = now
			got, err := match.Gross(context.Background(), f.pool, q)
			if err != nil {
				t.Fatalf("Gross: %v", err)
			}
			if !offered(got, second) {
				t.Fatalf("a %s blocked an overlapping trip", name)
			}
		})
	}
}

func TestASeatOnAnEarlierTripDoesNotBlockALaterOne(t *testing.T) {
	f := setup(t)
	// 05:00–05:10 and a 20 minute drive: over by 05:30, before 07:00.
	early := f.addOccurrence(t, corridorWKT, withWindow(
		time.Date(2026, time.July, 30, 5, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 30, 5, 10, 0, 0, time.UTC),
	))
	later := f.addOccurrence(t, corridorWKT)
	passenger := passengerOf(t, f, f.addRequest(t))
	book(t, f, early, passenger, "confirmed")

	if !offered(gross(t, f, passenger), later) {
		t.Fatal("a seat on a trip that is over by then blocked a later trip")
	}
}

// A retired route keeps the departures somebody has a seat on, so that seat
// is honoured. It takes nobody new: the driver withdrew it, and after an edit
// the replacement is what carries the new seat count (QA 2026-10-16).
func TestARetiredRouteTakesNoNewPassengers(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, corridorWKT)
	retire(t, f, occurrenceID)

	got, err := match.Gross(context.Background(), f.pool, query())
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	if offered(got, occurrenceID) {
		t.Fatal("a departure of a retired route was offered")
	}
}

func TestARetiredRouteWakesNobody(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, corridorWKT)
	f.addRequest(t)
	retire(t, f, occurrenceID)

	if got := waiting(t, f, occurrenceID); len(got) != 0 {
		t.Fatalf("a retired route woke %d requests", len(got))
	}
}

// "Πρωί" ends at 10:00 and a driver leaving at 10:00 is in it. The comparison
// was strict, so the two touched and never met (QA 2026-10-16, U2).
func TestADepartureOnTheEdgeOfTheWindowMatches(t *testing.T) {
	cases := map[string][2]time.Time{
		"leaves as the window closes": {
			time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 9, 30, 0, 0, time.UTC),
		},
		"latest is when the window opens": {
			time.Date(2026, time.July, 30, 6, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 6, 30, 0, 0, time.UTC),
		},
	}
	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			occurrenceID := f.addOccurrence(t, corridorWKT, withWindow(w[0], w[1]))

			got, err := match.Gross(context.Background(), f.pool, query())
			if err != nil {
				t.Fatalf("Gross: %v", err)
			}
			if !offered(got, occurrenceID) {
				t.Fatal("gross: a departure on the edge of the window was not offered")
			}

			f.addRequest(t, withRequestWindow(
				time.Date(2026, time.July, 30, 6, 30, 0, 0, time.UTC),
				time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)))
			if got := waiting(t, f, occurrenceID); len(got) != 1 {
				t.Fatalf("waiting: a request on the edge of the window was not woken, got %d", len(got))
			}
		})
	}
}
