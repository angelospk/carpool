package api_test

import (
	"net/http"
	"testing"
)

type myRoutes struct {
	Routes []struct {
		ID               string `json:"id"`
		OriginLabel      string `json:"origin_label"`
		DestinationLabel string `json:"destination_label"`
		Seats            int    `json:"seats"`
		Kind             string `json:"kind"`
		Weekdays         []int  `json:"weekdays"`
		DepartEarliest   string `json:"depart_earliest"`
		Upcoming         int    `json:"upcoming_trips"`
	} `json:"routes"`
}

// A driver with one route.
//
// Inserted rather than published: POST /v1/routes snaps the line to the road
// network through OSRM, which this harness has no reason to run. What is under
// test here is a driver seeing and retiring what they own, and that starts from
// a row either way.
func driverWithRoute(t *testing.T, h *harness, phone string) (token, routeID string) {
	t.Helper()
	token, _ = h.signUp(phone, "Οδηγός")

	// A route belongs to a driver profile, not to a user, so the profile has to
	// exist first. Made through the API so the consumption is worked out the way
	// it is for a real driver.
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	var driverID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &driverID, phone)

	h.mustQuery(`
		INSERT INTO route_templates (
			driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        'Αλεξανδρούπολη', 'Φέρες',
		        ST_SetSRID(ST_MakeLine(ST_MakePoint(25.8743, 40.8476), ST_MakePoint(26.1717, 40.8975)), 4326),
		        31.2, 28, 3, 'formula', 'test')
		RETURNING id::text`, &routeID, driverID)

	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		VALUES ($1::uuid, 'recurring', ARRAY[1,2,3], '07:30', '08:00')`, routeID)

	return token, routeID
}

func TestADriverCanSeeWhatTheyPublished(t *testing.T) {
	// Until now nothing could answer this. A driver could publish a route and
	// then had no way to look at it, let alone take it down.
	h := newHarness(t)
	token, routeID := driverWithRoute(t, h, "+306900000601")

	rec := h.do(http.MethodGet, "/v1/me/routes", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got myRoutes
	h.decode(rec, &got)

	if len(got.Routes) != 1 {
		t.Fatalf("got %d routes, want 1: %+v", len(got.Routes), got.Routes)
	}
	r := got.Routes[0]
	if r.ID != routeID {
		t.Errorf("id %s, want %s", r.ID, routeID)
	}
	// Everything needed to recognise which route this is, without opening it.
	if r.OriginLabel != "Αλεξανδρούπολη" || r.DestinationLabel != "Φέρες" {
		t.Errorf("labels: %+v", r)
	}
	if r.Seats != 3 || r.Kind != "recurring" || len(r.Weekdays) != 3 {
		t.Errorf("schedule not returned: %+v", r)
	}
	if r.DepartEarliest != "07:30" {
		t.Errorf("depart_earliest %q", r.DepartEarliest)
	}
}

func TestOneDriverCannotSeeAnothersRoutes(t *testing.T) {
	h := newHarness(t)
	_, _ = driverWithRoute(t, h, "+306900000602")
	other, _ := h.signUp("+306900000603", "Άλλος")

	rec := h.do(http.MethodGet, "/v1/me/routes", nil, other)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got myRoutes
	h.decode(rec, &got)
	if len(got.Routes) != 0 {
		t.Errorf("saw somebody else's routes: %+v", got.Routes)
	}
}

func TestRetiringARouteTakesItOutOfTheList(t *testing.T) {
	h := newHarness(t)
	token, routeID := driverWithRoute(t, h, "+306900000604")

	rec := h.do(http.MethodDelete, "/v1/me/routes/"+routeID, nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retiring: %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(http.MethodGet, "/v1/me/routes", nil, token)
	var got myRoutes
	h.decode(rec, &got)
	if len(got.Routes) != 0 {
		t.Errorf("the retired route is still listed: %+v", got.Routes)
	}
}

func TestRetiringIsIdempotent(t *testing.T) {
	// A driver tapping twice, or a request retried after a lost response, must
	// not be an error: the route is gone either way.
	h := newHarness(t)
	token, routeID := driverWithRoute(t, h, "+306900000605")

	for i := range 2 {
		rec := h.do(http.MethodDelete, "/v1/me/routes/"+routeID, nil, token)
		if rec.Code != http.StatusNoContent {
			t.Errorf("attempt %d: %d", i+1, rec.Code)
		}
	}
}

func TestOnlyTheDriverCanRetireTheirRoute(t *testing.T) {
	h := newHarness(t)
	_, routeID := driverWithRoute(t, h, "+306900000606")
	other, _ := h.signUp("+306900000607", "Άλλος")

	rec := h.do(http.MethodDelete, "/v1/me/routes/"+routeID, nil, other)
	// Not found rather than forbidden: somebody else's route id is not theirs
	// to learn the existence of.
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

func TestRetiringAnUnknownRouteIsNotFound(t *testing.T) {
	h := newHarness(t)
	token, _ := driverWithRoute(t, h, "+306900000608")

	for _, id := range []string{"not-a-uuid", "11111111-1111-1111-1111-111111111111"} {
		rec := h.do(http.MethodDelete, "/v1/me/routes/"+id, nil, token)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Errorf("id %q gave %d", id, rec.Code)
		}
	}
}

func TestMyRoutesNeedsASession(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodGet, "/v1/me/routes", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}
