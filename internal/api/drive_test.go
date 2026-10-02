package api_test

import (
	"net/http"
	"testing"
	"time"

	"carpool/internal/api"
	"carpool/internal/jobs"
)

// The driver's trip screen pins the departure, and the trip then says so to
// the driver and to each passenger: the minute, and for a passenger when the
// car reaches their own pickup.
func TestADriverPinsTheMinuteAndTheTripShowsIt(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000501", "Οδηγός")
	driver, _ := h.signUp("+306900000501", "Οδηγός")
	opens := h.clock.Now().Add(10 * time.Minute)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1`,
		occ, opens, opens.Add(30*time.Minute))
	departs := opens.Add(-5 * time.Minute)

	rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/drive", map[string]any{
		"departs_at": departs.Format(time.RFC3339), "detour_min": 3,
	}, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("drive: %d %s", rec.Code, rec.Body.String())
	}

	var trip struct {
		DepartEarliest    time.Time  `json:"depart_earliest"`
		DriveSetAt        *time.Time `json:"drive_set_at"`
		ScheduledEarliest *time.Time `json:"scheduled_earliest"`
		DetourMin         int        `json:"detour_min"`
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ, nil, driver), &trip)
	if !trip.DepartEarliest.Equal(departs) || trip.DriveSetAt == nil ||
		trip.ScheduledEarliest == nil || !trip.ScheduledEarliest.Equal(opens) || trip.DetourMin != 3 {
		t.Fatalf("trip after the drive: %+v", trip)
	}
}

func TestDriveRefusalsSayWhy(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000502", "Οδηγός")
	driver, _ := h.signUp("+306900000502", "Οδηγός")
	stranger, _ := h.signUp("+306900000503", "Κάποιος")
	opens := h.clock.Now().Add(10 * time.Minute)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1`,
		occ, opens, opens.Add(30*time.Minute))

	cases := []struct {
		name   string
		token  string
		body   map[string]any
		status int
		code   string
	}{
		{"a stranger", stranger, map[string]any{"departs_at": opens.Format(time.RFC3339)}, 404, "not_found"},
		{"no time", driver, map[string]any{}, 400, "invalid_request"},
		{"an hour from now", driver, map[string]any{"departs_at": opens.Add(time.Hour).Format(time.RFC3339)}, 422, "bad_departure"},
		{"a long detour", driver, map[string]any{"departs_at": opens.Format(time.RFC3339), "detour_min": 45}, 422, "bad_detour"},
	}
	for _, c := range cases {
		rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/drive", c.body, c.token)
		if rec.Code != c.status || h.errorCode(rec) != c.code {
			t.Errorf("%s: %d %q, want %d %q", c.name, rec.Code, h.errorCode(rec), c.status, c.code)
		}
	}
}

func TestADriverTurnsTheNudgeOnForARoute(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000504", "Οδηγός")
	driver, _ := h.signUp("+306900000504", "Οδηγός")
	other, _ := h.signUp("+306900000505", "Κάποιος")
	var route string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1`, &route, occ)

	if rec := h.do(http.MethodPut, "/v1/me/routes/"+route+"/nudge", map[string]any{"on": true}, other); rec.Code != http.StatusNotFound {
		t.Errorf("somebody else's route: %d", rec.Code)
	}
	if rec := h.do(http.MethodPut, "/v1/me/routes/"+route+"/nudge", map[string]any{"on": true}, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("nudge on: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Routes []struct {
			ID    string `json:"id"`
			Nudge bool   `json:"nudge_when_leaving"`
		} `json:"routes"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me/routes", nil, driver), &list)
	if len(list.Routes) != 1 || !list.Routes[0].Nudge {
		t.Fatalf("routes: %+v", list.Routes)
	}
}

// On the road: stop and resume taking people, change the detour, «Τέλος»,
// and a passenger-only «Είμαι εδώ».
func TestTheDriveCanBeChangedAndEndedOverHTTP(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000511", "Οδηγός")
	driver, _ := h.signUp("+306900000511", "Οδηγός")
	opens := h.clock.Now().Add(10 * time.Minute)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1`,
		occ, opens, opens.Add(30*time.Minute))
	base := "/v1/trips/" + occ + "/drive"

	if rec := h.do(http.MethodPatch, base, map[string]any{"offering": false}, driver); rec.Code != http.StatusConflict || h.errorCode(rec) != "not_driving" {
		t.Fatalf("adjusting before any drive: %d %q", rec.Code, h.errorCode(rec))
	}
	if rec := h.do(http.MethodPost, base, map[string]any{"departs_at": opens.Format(time.RFC3339)}, driver); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	for _, body := range []map[string]any{{"offering": false}, {"offering": true}, {"detour_min": 3}} {
		if rec := h.do(http.MethodPatch, base, body, driver); rec.Code != http.StatusNoContent {
			t.Fatalf("adjust %v: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/here", nil, driver); rec.Code != http.StatusNotFound {
		t.Fatalf("the driver saying «Είμαι εδώ»: %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, base+"/end", nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("end: %d %s", rec.Code, rec.Body.String())
	}
	var trip struct {
		DriveEndedAt *time.Time `json:"drive_ended_at"`
		Destination  []float64  `json:"destination"`
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ, nil, driver), &trip)
	if trip.DriveEndedAt == nil || len(trip.Destination) != 2 {
		t.Fatalf("after «Τέλος»: %+v", trip)
	}
}

// What the driver's maps hand-off needs, read the way the app reads it: each
// passenger's seat, pickup and drop-off, and where the road ends. A passenger
// is given none of another's.
func TestTheDriverIsGivenWhatTheMapsHandOffNeeds(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)

	var asDriver struct {
		Destination []float64 `json:"destination"`
		Passengers  []struct {
			BookingID *string   `json:"booking_id"`
			Pickup    []float64 `json:"pickup"`
			Dropoff   []float64 `json:"dropoff"`
		} `json:"passengers"`
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, driver), &asDriver)
	if len(asDriver.Destination) != 2 || len(asDriver.Passengers) != 1 {
		t.Fatalf("driver's view: %+v", asDriver)
	}
	p := asDriver.Passengers[0]
	if p.BookingID == nil || len(p.Pickup) != 2 || len(p.Dropoff) != 2 {
		t.Fatalf("the driver cannot hand this seat to a maps app: %+v", p)
	}

	var asPassenger struct {
		Destination []float64 `json:"destination"`
		Passengers  []struct {
			BookingID *string `json:"booking_id"`
		} `json:"passengers"`
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &asPassenger)
	if asPassenger.Destination != nil || (len(asPassenger.Passengers) > 0 && asPassenger.Passengers[0].BookingID != nil) {
		t.Fatalf("a passenger was given the driver's view: %+v", asPassenger)
	}
}

// Publishing a trip to leave on now: the answer names its departure, so the
// app can open it, where «Φεύγω» is.
func TestPublishingNamesTheFirstDeparture(t *testing.T) {
	var h *harness
	h = newRoutingHarness(t, func(d *api.Deps) {
		d.Materializer = jobs.NewMaterializer(d.Pool, d.Clock, testLocation())
	})
	token, _ := h.signUp("+306900000521", "Οδηγός")
	h.becomeDriver(token)

	soon := h.clock.Now().In(testLocation()).Add(10 * time.Minute)
	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1720, "destination_lat": 40.8960,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Φέρες",
		"seats": 3, "kind": "oneoff",
		"oneoff_date":     soon.Format("2006-01-02"),
		"depart_earliest": soon.Format("15:04"), "depart_latest": soon.Add(15 * time.Minute).Format("15:04"),
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		First *string `json:"first_occurrence_id"`
	}
	h.decode(rec, &got)
	if got.First == nil {
		t.Fatalf("no first departure in %s", rec.Body.String())
	}
	var departs time.Time
	h.mustQuery(`SELECT depart_earliest FROM trip_occurrences WHERE id = $1`, &departs, *got.First)
	if departs.Sub(soon).Abs() > time.Minute {
		t.Fatalf("the first departure leaves at %v, want %v", departs, soon)
	}
}
