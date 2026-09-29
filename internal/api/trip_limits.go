package api

import (
	"math"
	"slices"
	"strconv"
	"time"

	"carpool/internal/domain"
	"carpool/internal/jobs"
)

// Limits a route or a request has to be inside before anything is stored or
// routed. Each of these used to get through: some to a CHECK constraint that
// answered "the route was rejected" without saying which field, some straight
// into the table as something that could never happen — a one-off in the past,
// published with a 201 and never materialized.

const (
	// minTripKm is the shortest trip, end to end in a straight line, worth
	// sharing a car for. Below it the two ends are the same place.
	minTripKm = 1.0
	// maxCarSeats and maxPartySeats match the columns' CHECKs: a car offers up
	// to eight, a party asks for up to four.
	maxCarSeats   = 8
	maxPartySeats = 4
)

// limitError is a refusal with a code the app can branch on and a sentence a
// person can act on.
type limitError struct{ code, message string }

// straightKm is the great-circle distance between two points.
func straightKm(lon1, lat1, lon2, lat2 float64) float64 {
	const earthKm = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthKm * math.Asin(math.Sqrt(a))
}

// seatsProblem checks a party or car size. Absent is the caller's default;
// present and out of range is an error, never quietly replaced — a stepper
// that sent 0 used to publish three seats nobody offered.
func seatsProblem(seats *int, fallback, most int) (int, *limitError) {
	if seats == nil {
		return fallback, nil
	}
	if *seats < 1 || *seats > most {
		return 0, &limitError{"invalid_seats", "seats must be between 1 and " + strconv.Itoa(most)}
	}
	return *seats, nil
}

func distanceProblem(lon1, lat1, lon2, lat2 float64) *limitError {
	if straightKm(lon1, lat1, lon2, lat2) < minTripKm {
		return &limitError{"too_short", "the two ends are the same place; a trip has to be at least a kilometre"}
	}
	return nil
}

// scheduleProblem checks a parsed schedule against the clock, in Athens.
func (d Deps) scheduleProblem(s domain.Schedule) *limitError {
	earliest := s.DepartEarliest.Hour*60 + s.DepartEarliest.Minute
	latest := s.DepartLatest.Hour*60 + s.DepartLatest.Minute
	if latest <= earliest {
		return &limitError{"invalid_window",
			"the latest departure has to be after the earliest, on the same day; a window across midnight is not supported"}
	}
	if s.Kind != domain.ScheduleOneOff || s.OneOffDate == nil {
		return nil
	}

	now := d.now().In(d.location())
	today := domain.DateIn(now, d.location())
	date := *s.OneOffDate
	switch {
	case date.Before(today),
		// Earliest, not latest: a departure counts as gone from its earliest
		// time everywhere else, so a window already under way would publish a
		// trip nobody can be matched to.
		date == today && earliest <= now.Hour()*60+now.Minute():
		return &limitError{"oneoff_in_past", "that departure has already gone by"}
	case date.After(today.AddDays(jobs.HorizonDays - 1)):
		return &limitError{"oneoff_too_far",
			"a one-off trip can be published up to three weeks ahead"}
	}
	return nil
}

// windowProblem checks a passenger's window against the clock. It has to end
// in the future and fit in the stretch the materializer keeps planned: a
// request for next year matches nothing for eleven months and says so to
// nobody.
func (d Deps) windowProblem(start, end time.Time) *limitError {
	now := d.now()
	if !end.After(now) {
		return &limitError{"window_in_past", "that time has already gone by"}
	}
	horizon := now.AddDate(0, 0, jobs.HorizonDays)
	if end.After(horizon) {
		return &limitError{"window_too_far", "a request can look up to three weeks ahead"}
	}
	return nil
}

func (d Deps) location() *time.Location {
	if d.Location != nil {
		return d.Location
	}
	return time.UTC
}

// uniqueSorted returns the weekdays once each, Monday first.
func uniqueSorted(days []int) []int {
	out := slices.Clone(days)
	slices.Sort(out)
	return slices.Compact(out)
}
