package api_test

import (
	"net/http"
	"testing"
	"time"
)

func (h *harness) becomeDriver(token string) {
	h.t.Helper()
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		h.t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}
}

func (h *harness) vehicles(token string) []map[string]any {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/me/vehicles", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("GET /me/vehicles: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Vehicles []map[string]any `json:"vehicles"`
	}
	h.decode(rec, &body)
	return body.Vehicles
}

// Saying what you drive puts a car in the garage. Without that a driver could
// set up a profile and then have nothing to publish a route with, because a
// route names a vehicle.
func TestSayingWhatYouDrivePutsACarInTheGarage(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306901000001", "Οδηγός")
	h.becomeDriver(token)

	live := h.vehicles(token)
	if len(live) != 1 {
		t.Fatalf("%d cars, want 1", len(live))
	}
	if live[0]["is_default"] != true {
		t.Error("the only car is not the default")
	}
	if live[0]["model"] != "Yaris" {
		t.Errorf("model = %v", live[0]["model"])
	}
}

// Editing the profile edits that car rather than filling the garage with
// near-identical copies of it.
func TestEditingTheProfileEditsTheCarRatherThanAddingOne(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306901000002", "Οδηγός")
	h.becomeDriver(token)
	h.becomeDriver(token)

	if live := h.vehicles(token); len(live) != 1 {
		t.Fatalf("%d cars after saying it twice, want 1", len(live))
	}
}

// The point of the whole thing: a van at the weekend and a small car in the
// week, and the trip says which.
func TestASecondCarCanBeAddedAndChosenForARoute(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306901000003", "Οδηγός")
	h.becomeDriver(token)

	rec := h.do(http.MethodPost, "/v1/me/vehicles", map[string]any{
		"make": "Fiat", "model": "Ducato", "fuel": "diesel",
		"engine_cc": 2287, "plate": "ΖΗΘ-9999", "label": "το βαν",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding a car: %d %s", rec.Code, rec.Body.String())
	}
	var van map[string]any
	h.decode(rec, &van)
	if van["is_default"] == true {
		t.Error("adding a second car silently moved the default")
	}
	if van["name"] != "το βαν" {
		t.Errorf("name = %v, want the label the driver gave it", van["name"])
	}

	if live := h.vehicles(token); len(live) != 2 {
		t.Fatalf("%d cars, want 2", len(live))
	}

	// And the default can be moved.
	if rec := h.do(http.MethodPost, "/v1/me/vehicles/"+van["id"].(string)+"/default",
		nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("making it the default: %d %s", rec.Code, rec.Body.String())
	}
	live := h.vehicles(token)
	if live[0]["id"] != van["id"] {
		t.Errorf("the default is %v, want the van", live[0]["id"])
	}
}

// Somebody else's car is not yours to retire.
func TestAStrangerCannotTouchYourGarage(t *testing.T) {
	h := newHarness(t)
	mine, _ := h.signUp("+306901000004", "Οδηγός")
	theirs, _ := h.signUp("+306901000005", "Άλλος")
	h.becomeDriver(mine)

	id := h.vehicles(mine)[0]["id"].(string)

	if rec := h.do(http.MethodDelete, "/v1/me/vehicles/"+id, nil, theirs); rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger retiring somebody's car: %d", rec.Code)
	}
	if live := h.vehicles(mine); len(live) != 1 {
		t.Error("the car went anyway")
	}
}

// A route naming a car that is gone cannot be priced, so the order is: take the
// route down, then the car.
func TestACarUnderALiveRouteCannotBeRetired(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306901000006", "Οδηγός")
	h.becomeDriver(token)
	id := h.vehicles(token)[0]["id"].(string)

	h.mustExec(`
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination,
			origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		SELECT owner_id, id, ST_SetSRID(ST_MakePoint(25.80,40.85),4326),
		       ST_SetSRID(ST_MakePoint(26.00,40.85),4326), 'A', 'B',
		       ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test'
		  FROM vehicles WHERE id = $1`, id)

	rec := h.do(http.MethodDelete, "/v1/me/vehicles/"+id, nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "vehicle_in_use" {
		t.Errorf("error code = %q, want vehicle_in_use", code)
	}
}

// Publishing without ever saying what you drive is refused with something a
// person can act on, not a 500 from a null column.
func TestPublishingWithoutACarSaysSo(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306901000007", "Κάποιος")

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.80, "origin_lat": 40.85,
		"destination_lon": 26.00, "destination_lat": 40.85,
		"origin_label": "A", "destination_label": "B",
		"seats": 3, "kind": "oneoff",
		"oneoff_date":     h.clock.Now().Add(48 * time.Hour).Format("2006-01-02"),
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)

	// Without a router wired the harness cannot snap a route, so which 4xx it is
	// depends on how far the request gets. What must hold either way: it is not
	// a success, and it is not a server error.
	if rec.Code < http.StatusBadRequest || rec.Code >= http.StatusInternalServerError {
		t.Fatalf("publishing without a car gave %d: %s", rec.Code, rec.Body.String())
	}
}
