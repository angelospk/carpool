package api_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"carpool/internal/store"
)

// seedTheCountry replaces the harness's single rectangle with the real 75
// prefectures, so a test can name places that are hundreds of kilometres apart
// and have them resolve.
func (h *harness) seedTheCountry() {
	h.t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "regions", "greece.geojson"))
	if err != nil {
		h.t.Fatalf("resolving the country fixture: %v", err)
	}
	if err := store.SeedRegionsFromFile(context.Background(), h.pool, path); err != nil {
		h.t.Fatalf("seeding the country: %v", err)
	}
}

// The rule this replaces required one end of a trip to be inside the
// passenger's own prefecture. That was the pilot's geofence — the app served
// Έβρος and could not match anything else — and once the country opened it
// refused Θεσσαλονίκη → Αθήνα to somebody who lives in Ξάνθη, which is the
// journey a carpool is most obviously for.
func TestARequestMayCrossTheCountry(t *testing.T) {
	h := newHarness(t)
	h.seedTheCountry()
	token, _ := h.signUp("+306900000701", "Ξανθιώτης")

	rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
		"origin_lon": 22.9444, "origin_lat": 40.6401,
		"destination_lon": 23.7348, "destination_lat": 37.9755,
		"origin_label": "Θεσσαλονίκη", "destination_label": "Αθήνα",
		"window_start": h.clock.Now().Add(24 * time.Hour),
		"window_end":   h.clock.Now().Add(30 * time.Hour),
		"seats":        1,
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
}

// What is left of the rule is the honest limit: outside Greece there is no map,
// no toll data and no routing.
func TestARequestCannotLeaveTheCountry(t *testing.T) {
	h := newHarness(t)
	h.seedTheCountry()
	token, _ := h.signUp("+306900000702", "Ταξιδιώτης")

	for _, tt := range []struct {
		name                   string
		oLon, oLat, dLon, dLat float64
	}{
		{"destination in Sofia", 22.9444, 40.6401, 23.3219, 42.6977},
		{"pickup in Sofia", 23.3219, 42.6977, 22.9444, 40.6401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/v1/requests", map[string]any{
				"origin_lon": tt.oLon, "origin_lat": tt.oLat,
				"destination_lon": tt.dLon, "destination_lat": tt.dLat,
				"origin_label": "A", "destination_label": "B",
				"window_start": h.clock.Now().Add(24 * time.Hour),
				"window_end":   h.clock.Now().Add(30 * time.Hour),
				"seats":        1,
			}, token)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
			if code := h.errorCode(rec); code != "outside_country" {
				t.Errorf("error code = %q, want outside_country", code)
			}
		})
	}
}
