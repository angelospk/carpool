package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type liveMap struct {
	Trips []struct {
		From      string `json:"from"`
		To        string `json:"to"`
		SeatsFree int    `json:"seats_free"`
		Polyline  string `json:"polyline"`
	} `json:"trips"`
}

// The map of what is on the road now. Anonymous: where each car goes, as
// settlement names and a line, with no driver, no live position and the ends
// of the line cut off, because where a route starts is often a front door.
func TestTheLiveMapShowsTripsUnderwayAndNothingAboutWhoIsInThem(t *testing.T) {
	h := newHarness(t)
	_, _, tripID, _ := bookedTrip(t, h, "+306900001201", "+306900001202")
	viewer, _ := h.signUp("+306900001203", "Θεατής")

	var templateID string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1::uuid`, &templateID, tripID)

	departure := func(at time.Time, status string) string {
		var id string
		h.mustQuery(`
			INSERT INTO trip_occurrences (
				template_id, vehicle_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode, status)
			SELECT $1::uuid, vehicle_id, $2::date, $2, $3, 3, 2, 1, route_geom, route_km, route_min, pricing_mode, $4
			  FROM route_templates WHERE id = $1::uuid
			RETURNING id::text`, &id, templateID, at, at.Add(30*time.Minute), status)
		return id
	}
	now := h.clock.Now()
	underway := departure(now.Add(-10*time.Minute), "scheduled")
	departure(now.Add(48*time.Hour), "scheduled") // not yet

	rec := h.do(http.MethodGet, "/v1/live/map", nil, viewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got liveMap
	h.decode(rec, &got)
	if len(got.Trips) != 1 {
		t.Fatalf("got %d trips, want the one underway: %+v", len(got.Trips), got.Trips)
	}
	trip := got.Trips[0]
	if trip.From != "Αλεξανδρούπολη" || trip.To != "Φέρες" || trip.SeatsFree != 2 || trip.Polyline == "" {
		t.Errorf("trip: %+v", trip)
	}
	for _, leak := range []string{"Οδηγός", "Επιβάτης", "phone", "driver", "name"} {
		if containsFold(rec.Body.String(), leak) {
			t.Errorf("the response mentions %q: %s", leak, rec.Body.String())
		}
	}

	// The line stops a kilometre short of both ends, measured, not just
	// different from the stored one.
	var startGap, endGap float64
	h.mustQuery(`
		SELECT ST_Distance(ST_StartPoint(ST_LineFromEncodedPolyline($1, 6))::geography, t.origin::geography)
		  FROM route_templates t WHERE t.id = $2::uuid`, &startGap, trip.Polyline, templateID)
	h.mustQuery(`
		SELECT ST_Distance(ST_EndPoint(ST_LineFromEncodedPolyline($1, 6))::geography, t.destination::geography)
		  FROM route_templates t WHERE t.id = $2::uuid`, &endGap, trip.Polyline, templateID)
	if startGap < 900 || endGap < 900 {
		t.Errorf("the line ends %.0f m and %.0f m from the route's ends, want at least ~1 km", startGap, endGap)
	}

	// The line stops short of both ends.
	var full string
	h.mustQuery(`SELECT ST_AsEncodedPolyline(route_geom, 6) FROM route_templates WHERE id = $1::uuid`, &full, templateID)
	if trip.Polyline == full {
		t.Error("the line runs to the driver's front door")
	}

	// The names are the server's, not the driver's: a label can be a typed
	// coordinate or a street, which would hand out the door the line hides.
	h.mustExec(`UPDATE route_templates SET origin_label = '25.8743, 40.8476 οδός Δημοκρατίας 12' WHERE id = $1::uuid`, templateID)
	rec = h.do(http.MethodGet, "/v1/live/map", nil, viewer)
	got = liveMap{}
	h.decode(rec, &got)
	if len(got.Trips) != 1 || got.Trips[0].From != "Αλεξανδρούπολη" {
		t.Errorf("the driver's own label reached the map: %+v", got.Trips)
	}

	// A route offered to one group is that group's business.
	h.mustExec(`INSERT INTO groups (name, join_code, created_by) SELECT 'Νοσοκομείο', 'LIVEMAP01', driver_id FROM route_templates WHERE id = $1::uuid`, templateID)
	h.mustExec(`UPDATE route_templates SET group_id = (SELECT id FROM groups LIMIT 1) WHERE id = $1::uuid`, templateID)
	rec = h.do(http.MethodGet, "/v1/live/map", nil, viewer)
	got = liveMap{}
	h.decode(rec, &got)
	if len(got.Trips) != 0 {
		t.Errorf("a group-only route is on the public map: %+v", got.Trips)
	}

	// Nor is a departure that was called off.
	h.mustExec(`UPDATE route_templates SET group_id = NULL WHERE id = $1::uuid`, templateID)
	h.mustExec(`UPDATE trip_occurrences SET status = 'cancelled' WHERE id = $1::uuid`, underway)
	rec = h.do(http.MethodGet, "/v1/live/map", nil, viewer)
	got = liveMap{}
	h.decode(rec, &got)
	if len(got.Trips) != 0 {
		t.Errorf("a cancelled departure is on the map: %+v", got.Trips)
	}
}

func TestTheLiveMapIsForSignedInPeople(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodGet, "/v1/live/map", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
