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

// bothSidesSeePhone reports whether the driver is shown the passenger's phone
// and the passenger the driver's, which must always agree.
func (h *harness) bothSidesSeePhone(t *testing.T, occ, driver, passenger string) bool {
	t.Helper()
	_, driverSees := passengerNamed(t, h.phoneView(t, occ, driver), "Επιβάτης")["phone"]
	_, passengerSees := h.phoneView(t, occ, passenger).Driver["phone"]
	if driverSees != passengerSees {
		t.Fatalf("driver sees phone %v but passenger sees phone %v", driverSees, passengerSees)
	}
	return driverSees
}

// The window opens two hours before the earliest departure and closes two
// hours after the latest departure plus the road (20 minutes in the fixture,
// 30 minutes between earliest and latest). Each edge is tried on both sides,
// a second apart, so a window that drops the road, or measures from the wrong
// departure, fails.
func TestPhoneWindowEdgesAreExact(t *testing.T) {
	const road = 20 * time.Minute
	const spread = 30 * time.Minute
	cases := []struct {
		name  string
		leave time.Duration
		want  bool
	}{
		{"opens at earliest minus 2h", 2 * time.Hour, true},
		{"not yet a second before", 2*time.Hour + time.Second, false},
		{"closes at latest plus road plus 2h", -(spread + road + 2*time.Hour), true},
		{"over a second after", -(spread + road + 2*time.Hour + time.Second), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			driver, passenger, occ := h.bookedTrip(t)
			var routeMin int
			h.mustQuery(`SELECT route_min FROM trip_occurrences`, &routeMin)
			if time.Duration(routeMin)*time.Minute != road {
				t.Fatalf("fixture road is %d minutes, test assumes %v", routeMin, road)
			}
			h.leavesIn(c.leave)

			if got := h.bothSidesSeePhone(t, occ.String(), driver, passenger); got != c.want {
				t.Errorf("phone given = %v, want %v", got, c.want)
			}
		})
	}
}

// call_until travels with each phone so the app can stop offering the call
// when the window closes, without waiting for another fetch.
func TestPhoneComesWithTheEndOfTheWindow(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	h.leavesIn(time.Hour)

	var latest time.Time
	h.mustQuery(`SELECT depart_latest FROM trip_occurrences`, &latest)
	want := latest.Add(20*time.Minute + 2*time.Hour)

	check := func(who string, person map[string]any) {
		t.Helper()
		raw, ok := person["call_until"].(string)
		if !ok {
			t.Fatalf("%s: no call_until beside the phone: %v", who, person)
		}
		got, err := time.Parse(time.RFC3339, raw)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s: call_until = %q, want %v", who, raw, want)
		}
	}
	check("driver sees", passengerNamed(t, h.phoneView(t, occ.String(), driver), "Επιβάτης"))
	check("passenger sees", h.phoneView(t, occ.String(), passenger).Driver)
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

			p := passengerNamed(t, h.phoneView(t, occ.String(), driver), "Επιβάτης")
			if _, ok := p["phone"]; ok {
				t.Error("driver is given the passenger's phone outside the window")
			}
			if _, ok := p["call_until"]; ok {
				t.Error("driver is given a call_until with no phone")
			}
			if _, ok := h.phoneView(t, occ.String(), passenger).Driver["phone"]; ok {
				t.Error("passenger is given the driver's phone outside the window")
			}
		})
	}
}

// Only a booking still 'confirmed' earns the call: a completed one is a trip
// that is over, whatever the clock says.
func TestACompletedBookingGetsNoPhone(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	h.leavesIn(time.Hour)
	h.mustExec(`UPDATE bookings SET status = 'completed'`)

	p := passengerNamed(t, h.phoneView(t, occ.String(), driver), "Επιβάτης")
	if _, ok := p["phone"]; ok {
		t.Error("driver is given the phone of a completed booking")
	}
	if _, ok := h.phoneView(t, occ.String(), passenger).Driver["phone"]; ok {
		t.Error("passenger is given the driver's phone on a completed booking")
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
