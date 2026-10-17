package api_test

import (
	"net/http"
	"testing"
	"time"
)

type phoneTrip struct {
	Driver     map[string]any `json:"driver"`
	Passengers []map[string]any
}

func (h *harness) phoneView(t *testing.T, occ, token string) phoneTrip {
	t.Helper()
	rec := h.do(http.MethodGet, "/v1/trips/"+occ, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET trip: %d %s", rec.Code, rec.Body.String())
	}
	var tr struct {
		Driver     map[string]any   `json:"driver"`
		Passengers []map[string]any `json:"passengers"`
	}
	h.decode(rec, &tr)
	return phoneTrip{Driver: tr.Driver, Passengers: tr.Passengers}
}

func passengerNamed(t *testing.T, tr phoneTrip, name string) map[string]any {
	t.Helper()
	for _, p := range tr.Passengers {
		if p["name"] == name {
			return p
		}
	}
	t.Fatalf("no passenger %q", name)
	return nil
}

// leavesIn moves the fixture trip (a 30-minute departure window, 20 minutes on
// the road) so it starts d after the harness clock. The clock itself stays put:
// moving it would expire the tokens the fixture signed.
func (h *harness) leavesIn(d time.Duration) {
	start := h.clock.Now().Add(d)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		start, start.Add(30*time.Minute))
}

// The call window is two hours before the earliest departure to two hours
// after the latest one plus the road, so 2h51m after the start is outside.
func TestPhoneIsGivenBothWaysInsideTheWindow(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	h.leavesIn(time.Hour)

	if got := passengerNamed(t, h.phoneView(t, occ.String(), driver), "Επιβάτης")["phone"]; got != "+306900000032" {
		t.Errorf("driver sees passenger phone %v", got)
	}
	if got := h.phoneView(t, occ.String(), passenger).Driver["phone"]; got != "+306900000031" {
		t.Errorf("passenger sees driver phone %v", got)
	}
}

func TestPhoneIsAbsentOutsideTheWindow(t *testing.T) {
	cases := map[string]time.Duration{
		"before": 2*time.Hour + time.Minute,
		"after":  -(2*time.Hour + 51*time.Minute),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			driver, passenger, occ := h.bookedTrip(t)
			h.leavesIn(in)

			if _, ok := passengerNamed(t, h.phoneView(t, occ.String(), driver), "Επιβάτης")["phone"]; ok {
				t.Error("driver is given the passenger's phone outside the window")
			}
			if _, ok := h.phoneView(t, occ.String(), passenger).Driver["phone"]; ok {
				t.Error("passenger is given the driver's phone outside the window")
			}
		})
	}
}

func TestACoPassengerNeverSeesAnotherPassengersPhone(t *testing.T) {
	h := newHarness(t)
	_, passenger, occ := h.bookedTrip(t)
	h.secondPassenger(t)
	h.leavesIn(time.Hour)

	tr := h.phoneView(t, occ.String(), passenger)
	for _, p := range tr.Passengers {
		if _, ok := p["phone"]; ok {
			t.Errorf("passenger %v carries a phone for a co-passenger", p["name"])
		}
	}
	if _, ok := tr.Driver["phone"]; !ok {
		t.Error("the driver's phone should still reach a passenger with a seat")
	}
}

func TestACancelledBookingGetsNoPhone(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	h.leavesIn(time.Hour)
	h.mustExec(`UPDATE bookings SET status = 'cancelled_by_passenger'`)

	if len(h.phoneView(t, occ.String(), driver).Passengers) != 0 {
		t.Error("a cancelled booking is still listed to the driver")
	}
	// The passenger is no longer on the trip at all.
	if rec := h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger); rec.Code == http.StatusOK {
		t.Errorf("cancelled passenger reads the trip: %s", rec.Body.String())
	}
}
