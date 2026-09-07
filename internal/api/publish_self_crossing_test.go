package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Publishing a route whose road crosses itself.
//
// This is the whole bug, end to end: a driver taps publish, OSRM hands back a
// line that touches itself somewhere, and the insert fails a CHECK constraint
// the driver has no way to know about. What reached the phone was a 400 reading
// "the route was rejected", and what reached the home screen after that was
// "δεν έχεις επόμενες διαδρομές", because the route was never stored.
//
// A stub router rather than the live one: the failure needs a specific shape
// out of OSRM, and "run this against the Greece extract and hope
// Αλεξανδρούπολη → Μάκρη is still non-simple after the next data refresh" is
// not a regression test. The bow tie below is the smallest line that crosses
// itself and has no repeated coordinate, so nothing about it can be blamed on
// degenerate input.
func TestARouteWhoseRoadCrossesItselfCanBePublished(t *testing.T) {
	const bowTie = `{"code":"Ok","routes":[{"distance":18000,"duration":1200,` +
		`"geometry":{"coordinates":[[25.80,40.85],[25.90,40.87],[25.90,40.85],[25.80,40.87]]},` +
		`"legs":[{"steps":[]}]}],"waypoints":[]}`

	osrm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bowTie))
	}))
	defer osrm.Close()

	h := newHarnessRoutedBy(t, osrm.URL)
	token, _ := h.signUp("+306901000031", "Οδηγός")
	h.becomeDriver(token)

	// The fixture has to actually cross itself, or this test passes for the
	// wrong reason for the rest of its life.
	var simple bool
	h.mustQuery(`SELECT ST_IsSimple(ST_GeomFromText(
		'LINESTRING(25.80 40.85, 25.90 40.87, 25.90 40.85, 25.80 40.87)', 4326))`, &simple)
	if simple {
		t.Fatal("the bow tie does not cross itself")
	}

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.80, "origin_lat": 40.85,
		"destination_lon": 25.80, "destination_lat": 40.87,
		"origin_label": "A", "destination_label": "B",
		"seats": 3, "kind": "oneoff",
		"oneoff_date":     h.clock.Now().Add(48 * time.Hour).Format("2006-01-02"),
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("publishing gave %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if body.TemplateID == "" {
		t.Fatal("published without a template id")
	}

	// Stored, and stored as the line OSRM gave: same vertices, same order.
	// ST_OrderingEquals rather than ST_Equals, because a route reversed or
	// re-noded is spatially identical and behaviourally the opposite trip.
	var ordered bool
	h.mustQuery(`SELECT ST_OrderingEquals(route_geom, ST_GeomFromText(
		'LINESTRING(25.80 40.85, 25.90 40.87, 25.90 40.85, 25.80 40.87)', 4326))
		FROM route_templates WHERE id = $1`, &ordered, body.TemplateID)
	if !ordered {
		t.Error("the stored geometry is not the line the router returned, in the order it returned it")
	}
}
