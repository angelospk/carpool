package match_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/match"
)

func passengerOf(t *testing.T, f *fixture, requestID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT passenger_id FROM ride_requests WHERE id = $1`, requestID).Scan(&id); err != nil {
		t.Fatalf("finding the passenger: %v", err)
	}
	return id
}

// book gives the passenger a seat through an earlier request of theirs, which
// has since been matched: the request under test is a second one.
func book(t *testing.T, f *fixture, occurrenceID, passengerID uuid.UUID, status string) {
	t.Helper()
	earlier := f.addRequest(t, withRequestPassenger(passengerID), withRequestStatus("matched"))
	if _, err := f.pool.Exec(context.Background(), `
		WITH ask AS (
			INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			                  suggested_price_eur, itinerary_version, status, expires_at)
			VALUES ($4, $1, 0, 0, ST_SetSRID(ST_MakePoint(25.82, 40.851), 4326),
			        ST_SetSRID(ST_MakePoint(25.98, 40.851), 4326), 2.00, 1, 'accepted', now())
			RETURNING id)
		INSERT INTO bookings (occurrence_id, request_id, ask_id, passenger_id, seats, price_eur,
		                      pickup_point, dropoff_point, status)
		SELECT $1, $4, ask.id, $2, 1, 2.00, ST_SetSRID(ST_MakePoint(25.82, 40.851), 4326),
		       ST_SetSRID(ST_MakePoint(25.98, 40.851), 4326), $3
		FROM ask`,
		occurrenceID, passengerID, status, earlier); err != nil {
		t.Fatalf("booking: %v", err)
	}
}

func womenOnly(t *testing.T, f *fixture, passengerID uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET gender = 'female', women_only = true WHERE id = $1`, passengerID); err != nil {
		t.Fatalf("setting women_only: %v", err)
	}
}

func driverGender(t *testing.T, f *fixture, gender string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET gender = $2 WHERE id = $1`, f.driverID, gender); err != nil {
		t.Fatalf("setting gender: %v", err)
	}
}

func gross(t *testing.T, f *fixture, passengerID uuid.UUID) []match.Candidate {
	t.Helper()
	q := query()
	q.PassengerID = passengerID
	got, err := match.Gross(context.Background(), f.pool, q)
	if err != nil {
		t.Fatalf("Gross: %v", err)
	}
	return got
}

// A passenger who already has a seat on a departure is not offered it again,
// whatever became of that booking: the table allows one per person per trip,
// and the driver was being asked something they could not accept.
func TestNobodyIsOfferedATripTheyAreAlreadyOn(t *testing.T) {
	for _, status := range []string{"confirmed", "cancelled_by_passenger"} {
		t.Run(status, func(t *testing.T) {
			f := setup(t)
			occurrenceID := f.addOccurrence(t, corridorWKT)
			requestID := f.addRequest(t)
			passenger := passengerOf(t, f, requestID)
			book(t, f, occurrenceID, passenger, status)

			if got := gross(t, f, passenger); len(got) != 0 {
				t.Fatalf("offered a trip they already have a seat on: %+v", got)
			}
			if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
				t.Fatalf("woken by a trip they already have a seat on")
			}
		})
	}
}

func TestWomenOnlyIsOfferedWomenDriversOnly(t *testing.T) {
	for _, tc := range []struct {
		driver string
		want   bool
	}{{"", false}, {"male", false}, {"other", false}, {"female", true}} {
		t.Run("driver="+tc.driver, func(t *testing.T) {
			f := setup(t)
			occurrenceID := f.addOccurrence(t, corridorWKT)
			requestID := f.addRequest(t)
			passenger := passengerOf(t, f, requestID)
			womenOnly(t, f, passenger)
			if tc.driver != "" {
				driverGender(t, f, tc.driver)
			}

			if got := len(gross(t, f, passenger)) == 1; got != tc.want {
				t.Fatalf("gross offered=%v, want %v", got, tc.want)
			}
			if got := slices.Contains(waiting(t, f, occurrenceID), requestID); got != tc.want {
				t.Fatalf("waiting woke=%v, want %v", got, tc.want)
			}
		})
	}
}
