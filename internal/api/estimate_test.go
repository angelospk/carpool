package api_test

import (
	"net/http"
	"testing"
)

// The question every first-time user has, which the app could not answer at
// all: a figure only appeared once a driver had been asked, because it depends
// on their car and their seats.
func TestAnEstimateAnswersRoughlyWhatItCosts(t *testing.T) {
	h := newRoutingHarness(t)
	h.seedTheCountry()
	token, _ := h.signUp("+306903000001", "Επιβάτης")

	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1717, "destination_lat": 40.8975,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Kind              string  `json:"kind"`
		RouteKm           float64 `json:"route_km"`
		ContributionCents int     `json:"contribution_cents"`
		Assumptions       struct {
			Version     string `json:"version"`
			Divisor     int    `json:"divisor"`
			FuelPriceAs string `json:"fuel_price_as_of"`
			RegionName  string `json:"region_name"`
		} `json:"assumptions"`
	}
	h.decode(rec, &got)

	if got.Kind != "planning_estimate" {
		t.Errorf("kind = %q; a guess must never be mistakable for a receipt", got.Kind)
	}
	if got.RouteKm <= 0 {
		t.Errorf("route_km = %v", got.RouteKm)
	}
	if got.ContributionCents <= 0 {
		t.Errorf("contribution = %d cents on a real route", got.ContributionCents)
	}
	if got.ContributionCents%50 != 0 {
		t.Errorf("contribution = %d, which is not a whole half euro", got.ContributionCents)
	}
	if got.Assumptions.Version == "" || got.Assumptions.Divisor == 0 {
		t.Errorf("the figure does not say what it was built from: %+v", got.Assumptions)
	}
	if got.Assumptions.FuelPriceAs == "" || got.Assumptions.RegionName == "" {
		t.Errorf("no price date or region: %+v", got.Assumptions)
	}
}

// Nobody has agreed to anything. A stored estimate invites somebody to argue
// the real price against a guess the app made before it knew whose car it was.
func TestAnEstimateLeavesNothingBehind(t *testing.T) {
	h := newRoutingHarness(t)
	h.seedTheCountry()
	token, _ := h.signUp("+306903000002", "Επιβάτης")

	before := h.countRows()
	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1717, "destination_lat": 40.8975,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	for table, was := range before {
		if now := h.countRows()[table]; now != was {
			t.Errorf("%s went from %d rows to %d; an estimate wrote something down", table, was, now)
		}
	}
	// And it is not cached: a stale price shown as a current one is the failure
	// caching would cause here.
	if store := rec.Header().Get("Cache-Control"); store != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", store)
	}
}

// A confident figure built on nothing is worse than no figure. Without fuel
// prices there is nothing to estimate from, and it says so.
func TestNoFuelPricesMeansNoEstimateRatherThanAGuess(t *testing.T) {
	h := newRoutingHarness(t)
	h.seedTheCountry()
	token, _ := h.signUp("+306903000003", "Επιβάτης")
	h.mustExec(`DELETE FROM fuel_prices`)

	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1717, "destination_lat": 40.8975,
	}, token)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "no_estimate" {
		t.Errorf("error code = %q, want no_estimate", code)
	}
}

func TestAnEstimateNeedsASession(t *testing.T) {
	h := newRoutingHarness(t)
	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": 25.87, "origin_lat": 40.84,
		"destination_lon": 26.17, "destination_lat": 40.89,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
