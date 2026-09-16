package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carpool/internal/store"
)

// Αλεξανδρούπολη → Κομοτηνή, which is two prefectures.
//
// SPEC.md said for a long time that at least one endpoint of every trip had to
// lie inside the caller's home region, and README.md recorded the consequence —
// "Κομοτηνή is in Ροδόπη" — as a real limit. NEXT.md §2 then set out to lift it
// for a ΔΠΘ pilot, whose campuses are spread across exactly these prefectures.
//
// Measured on 2026-09-16, all four layers already allow it: the places dataset
// covers the whole country (internal/places/national_test.go), regions are
// seeded from greece.geojson so Κομοτηνή resolves to Ροδόπης rather than to
// nothing, the handlers refuse only what is outside Greece, and the clipped
// Evros routing extract still answers this pair (58.3 km). The rule had been
// removed from request_handlers.go — see the comment there — and never removed
// from the documents.
//
// Which leaves this: nothing asserted it. The corridor worked by the absence of
// a check, and a well-meaning change re-adding a home-region guard would have
// closed it again silently, months after anybody remembered why it was open.
// This test is the guard on the guard.

const (
	alexLon, alexLat = 25.8743, 40.8476
	komoLon, komoLat = 25.4058, 41.1222
)

// twoPrefectures replaces the harness's single region with two that meet along
// 25.6°E, so Αλεξανδρούπολη and Κομοτηνή genuinely sit in different ones. With
// the harness's own wide polygon they share a region and the test would pass
// without proving anything.
func twoPrefectures(h *harness) {
	h.t.Helper()
	h.mustExec(`DELETE FROM regions`)
	h.mustExec(`INSERT INTO regions (name, boundary) VALUES
		('Έβρου',   ST_Multi(ST_GeomFromText('POLYGON((25.6 40.3, 26.7 40.3, 26.7 41.8, 25.6 41.8, 25.6 40.3))', 4326))),
		('Ροδόπης', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 25.6 40.3, 25.6 41.8, 25.3 41.8, 25.3 40.3))', 4326)))`)
}

// realGreece replaces the harness's region with the shipped fixture: all 75
// περιφερειακές ενότητες, with the boundaries the country actually has. Slower
// than a rectangle and the only honest way to ask a question about the border.
func realGreece(h *harness) {
	h.t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "regions", "greece.geojson"))
	if err != nil {
		h.t.Fatalf("resolving the region fixture: %v", err)
	}
	h.mustExec(`DELETE FROM regions`)
	if err := store.SeedRegionsFromFile(context.Background(), h.pool, path); err != nil {
		h.t.Fatalf("SeedRegionsFromFile: %v", err)
	}
}

// The fixture has to actually straddle the boundary, or this passes for the
// wrong reason for the rest of its life.
func TestTheTwoEndsAreInDifferentPrefectures(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)

	for _, tc := range []struct {
		name     string
		lon, lat float64
		want     string
	}{
		{"Αλεξανδρούπολη", alexLon, alexLat, "Έβρου"},
		{"Κομοτηνή", komoLon, komoLat, "Ροδόπης"},
	} {
		var got string
		h.mustQuery(`SELECT name FROM regions
			WHERE ST_Covers(boundary, ST_SetSRID(ST_MakePoint($1, $2), 4326))`,
			&got, tc.lon, tc.lat)
		if got != tc.want {
			t.Fatalf("%s resolves to %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A passenger asking to cross the boundary is not refused. This is the half of
// the rule that was written down in the handler and is now asserted.
func TestARequestMayCrossAPrefectureBoundary(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000401", "Επιβάτης")

	rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Κομοτηνή",
		"window_start": h.clock.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(4 * time.Hour).Format(time.RFC3339),
		"seats":        1,
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("requesting across the boundary gave %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code == "outside_country" {
		t.Fatal("a trip inside Greece was refused as outside it")
	}
}

// A trip with neither end in Greece is still refused. Opening the corridor must
// not have opened everything: routing, tolls and fuel prices all stop at the
// border, so a request from Sofia to Plovdiv is one this app cannot price, let
// alone serve.
func TestARequestOutsideGreeceIsStillRefused(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000402", "Επιβάτης")

	rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
		// Sofia to Plovdiv.
		"origin_lon": 23.3219, "origin_lat": 42.6977,
		"destination_lon": 24.7453, "destination_lat": 42.1354,
		"origin_label": "Σόφια", "destination_label": "Φιλιππούπολη",
		"window_start": h.clock.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(4 * time.Hour).Format(time.RFC3339),
		"seats":        1,
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("requesting outside Greece gave %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := h.errorCode(rec); got != "outside_country" {
		t.Fatalf("error code = %q, want outside_country", got)
	}
}

// The driver's half, through the real router: the road between the two towns is
// in the routing extract, so the route snaps and stores. This is the part that
// a clipped extract would break rather than refuse, which is why it needs a
// real OSRM instead of an assertion about the handler.
func TestARouteMayBePublishedAcrossAPrefectureBoundary(t *testing.T) {
	if os.Getenv(OSRMEnv) == "" {
		t.Skipf("%s is not set; skipping the routing test", OSRMEnv)
	}
	h := newHarnessRoutedBy(t, os.Getenv(OSRMEnv))
	twoPrefectures(h)

	token, _ := h.signUp("+306900000403", "Οδηγός")
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Κομοτηνή",
		"seats": 3, "kind": "recurring", "weekdays": []int{1, 2, 3, 4, 5},
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("publishing across the boundary gave %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		TemplateID string  `json:"template_id"`
		RouteKm    float64 `json:"route_km"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if body.TemplateID == "" {
		t.Fatal("published without a template id")
	}
	// Measured at 58.3 km on both the whole-country extract and the clipped
	// Evros one. A wide band rather than the figure itself: the point is that a
	// real road was found between the two towns, not that OSRM's answer never
	// moves when the map data is refreshed.
	if body.RouteKm < 40 || body.RouteKm > 90 {
		t.Fatalf("route_km = %.1f, want the road between the two towns (~58 km)", body.RouteKm)
	}
}

// A clipped routing extract does not refuse what it does not cover: it snaps
// the coordinate to the nearest road it does have and answers Ok. Measured
// against the extract this deployment serves, Θεσσαλονίκη → Αθήνα came back as
// a confident 186,6 km line drawn through Thrace, and there was nothing
// downstream to tell that from a real answer — it is the geometry a published
// route is stored with and the distance a contribution is priced from.
//
// geo.SnapRadiusMetres now bounds the snap, so OSRM answers NoSegment instead.
// These two tests are about what the caller is then told: a place the app does
// not yet serve is a fact about the request, so it is a 400 somebody can act on
// rather than a 502 that reads as "our fault, try again".
func noSegmentRouter(t *testing.T) *harness {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"code":"NoSegment","message":"Could not find a matching segment for coordinate 1"}`))
	}))
	t.Cleanup(srv.Close)
	return newHarnessRoutedBy(t, srv.URL)
}

func TestPublishingOutsideTheRoutedAreaIsRefusedAsSuch(t *testing.T) {
	h := noSegmentRouter(t)
	token, _ := h.signUp("+306900000404", "Οδηγός")
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	// Both ends inside the harness's regions, so the country check passes and
	// what is being tested is the routing answer. The stub says NoSegment
	// whatever it is asked, which is what a real extract does for a coordinate
	// it does not hold once a snapping bound is sent.
	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": 26.5310, "destination_lat": 41.5010,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Ορεστιάδα",
		"seats": 3, "kind": "recurring", "weekdays": []int{1, 2, 3, 4, 5},
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("publishing outside the routed area gave %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := h.errorCode(rec); got != "outside_service_area" {
		t.Fatalf("error code = %q, want outside_service_area", got)
	}
	// And nothing was stored: a fabricated line is exactly what this prevents.
	var routes int
	h.mustQuery(`SELECT count(*)::int FROM route_templates`, &routes)
	if routes != 0 {
		t.Fatalf("stored %d route templates for a refused publish", routes)
	}
}

func TestEstimatingOutsideTheRoutedAreaIsRefusedAsSuch(t *testing.T) {
	h := noSegmentRouter(t)
	token, _ := h.signUp("+306900000405", "Επιβάτης")

	// Both ends inside the harness's region, so the country check passes and
	// what is being tested is the routing answer rather than the geofence. The
	// stub says NoSegment whatever it is asked.
	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": 26.5310, "destination_lat": 41.5010,
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("estimating outside the routed area gave %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := h.errorCode(rec); got != "outside_service_area" {
		t.Fatalf("error code = %q, want outside_service_area", got)
	}
}

// Labels arrive from the client and are read back by people who were not there
// -- on a trip card, and aggregated into the demand line every driver in the
// area sees. Nothing checked them; the only ceiling was the 64 KiB body.
func TestAPlaceLabelThatHidesItselfIsRefused(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000406", "Επιβάτης")

	for _, tc := range []struct{ name, label string }{
		{"a right-to-left override", "Αλεξ\u202eανδρούπολη"},
		{"a control character", "Αλεξ\u0007ανδρούπολη"},
		{"nothing at all", "   "},
		{"a paragraph", strings.Repeat("α", 500)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
				"origin_lon": alexLon, "origin_lat": alexLat,
				"destination_lon": komoLon, "destination_lat": komoLat,
				"origin_label": tc.label, "destination_label": "Κομοτηνή",
				"window_start": h.clock.Now().Add(2 * time.Hour).Format(time.RFC3339),
				"window_end":   h.clock.Now().Add(4 * time.Hour).Format(time.RFC3339),
				"seats":        1,
			}, token)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("gave %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if got := h.errorCode(rec); got != "invalid_request" {
				t.Fatalf("error code = %q, want invalid_request", got)
			}
		})
	}
}

// And the tidying, which the demand count depends on: a label that differs only
// in whitespace must not become a second corridor and split one number in two.
func TestAPlaceLabelIsStoredTidied(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000407", "Επιβάτης")

	rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "  Αλεξανδρούπολη \t Λιμάνι ", "destination_label": " Κομοτηνή ",
		"window_start": h.clock.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(4 * time.Hour).Format(time.RFC3339),
		"seats":        1,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("gave %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var origin, destination string
	h.mustQuery(`SELECT origin_label, destination_label FROM ride_requests LIMIT 1`,
		&origin, &destination)
	if origin != "Αλεξανδρούπολη Λιμάνι" || destination != "Κομοτηνή" {
		t.Fatalf("stored %q -> %q", origin, destination)
	}
}

// The border runs through this prefecture, and the publish path never asked
// which side of it a pin was on.
//
// Requests and estimates both call store.RegionAt and refuse what is outside
// Greece. Publishing did not, and it mattered more once the snapping bound went
// in: a pin a few kilometres into Turkey or Bulgaria near Ορεστιάδα is well
// within SnapRadiusMetres of a Greek road, so it snaps, routes, and stores —
// priced with Greek fuel and Greek tolls for a journey that does not begin in
// Greece.
func TestPublishingFromOutsideGreeceIsRefused(t *testing.T) {
	if os.Getenv(OSRMEnv) == "" {
		t.Skipf("%s is not set; skipping the routing test", OSRMEnv)
	}
	h := newHarnessRoutedBy(t, os.Getenv(OSRMEnv))
	// The real boundaries, not a rectangle. Any rectangle drawn round Έβρος
	// contains Αδριανούπολη — the border is a river, and the point of this test
	// is which side of it a pin is on, so a square fixture would have been the
	// thing under test rather than the check.
	realGreece(h)

	token, _ := h.signUp("+306900000408", "Οδηγός")
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	// Edirne, about 20 km from Ορεστιάδα and on the other side of the river.
	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 26.5557, "origin_lat": 41.6771,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "Αδριανούπολη", "destination_label": "Κομοτηνή",
		"seats": 3, "kind": "recurring", "weekdays": []int{1, 2, 3, 4, 5},
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("publishing from outside Greece gave %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := h.errorCode(rec); got != "outside_country" {
		t.Fatalf("error code = %q, want outside_country", got)
	}

	var routes int
	h.mustQuery(`SELECT count(*)::int FROM route_templates`, &routes)
	if routes != 0 {
		t.Fatalf("stored %d route templates for a refused publish", routes)
	}
}

// And the ordinary in-country publish still works, so the check has not simply
// closed the door.
func TestPublishingFromInsideGreeceStillWorks(t *testing.T) {
	if os.Getenv(OSRMEnv) == "" {
		t.Skipf("%s is not set; skipping the routing test", OSRMEnv)
	}
	h := newHarnessRoutedBy(t, os.Getenv(OSRMEnv))
	realGreece(h)

	token, _ := h.signUp("+306900000409", "Οδηγός")
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Κομοτηνή",
		"seats": 3, "kind": "recurring", "weekdays": []int{1, 2, 3, 4, 5},
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publishing inside Greece gave %d, want 201: %s", rec.Code, rec.Body.String())
	}
}
