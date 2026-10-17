package api_test

import (
	"net/http"
	"testing"
	"time"
)

type seatsAndRepeats struct {
	Seats   *int   `json:"seats"`
	Repeats bool   `json:"repeats"`
	ID      string `json:"id"`
}

// QA 2026-10-17 F4: a price is per seat, so a passenger with two seats needs
// to know how many they hold to read what they pay in all.
func TestEachOwnTripListCarriesTheCallersSeats(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	h.mustExec(`UPDATE bookings SET seats = 2`)

	var up struct{ Trips []seatsAndRepeats }
	h.decode(h.do(http.MethodGet, "/v1/trips/upcoming", nil, passenger), &up)
	if len(up.Trips) != 1 || up.Trips[0].Seats == nil || *up.Trips[0].Seats != 2 {
		t.Errorf("upcoming for the passenger: %+v, want seats 2", up.Trips)
	}
	// The driver has no booking of their own.
	h.decode(h.do(http.MethodGet, "/v1/trips/upcoming", nil, driver), &up)
	if len(up.Trips) != 1 || up.Trips[0].Seats != nil {
		t.Errorf("upcoming for the driver: %+v, want no seats", up.Trips)
	}

	var trip seatsAndRepeats
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &trip)
	if trip.Seats == nil || *trip.Seats != 2 {
		t.Errorf("trip for the passenger: seats %v, want 2", trip.Seats)
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, driver), &trip)
	if trip.Seats != nil {
		t.Errorf("trip for the driver: seats %v, want none", *trip.Seats)
	}

	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $1`, h.clock.Now().Add(-3*time.Hour))
	var past struct{ Trips []seatsAndRepeats }
	h.decode(h.do(http.MethodGet, "/v1/trips/past", nil, passenger), &past)
	if len(past.Trips) != 1 || past.Trips[0].Seats == nil || *past.Trips[0].Seats != 2 {
		t.Errorf("past: %+v, want seats 2", past.Trips)
	}
}

// QA 2026-10-17 F7: a one-off route does not repeat, so the trip must not
// offer to.
func TestATripSaysWhetherItsRouteRepeats(t *testing.T) {
	h := newHarness(t)
	_, passenger, occ := h.bookedTrip(t)

	var trip seatsAndRepeats
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &trip)
	if trip.Repeats {
		t.Errorf("a route with no schedule repeats")
	}

	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, oneoff_date, depart_earliest, depart_latest)
		SELECT template_id, 'oneoff', '2026-07-30', '07:30', '08:00' FROM trip_occurrences`)
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &trip)
	if trip.Repeats {
		t.Errorf("a one-off route repeats")
	}

	h.mustExec(`UPDATE route_schedules SET kind = 'recurring', oneoff_date = NULL, weekdays = '{4}'`)
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &trip)
	if !trip.Repeats {
		t.Errorf("a recurring route does not repeat")
	}
}
