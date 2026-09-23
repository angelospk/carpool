package api_test

import (
	"net/http"
	"testing"
	"time"
)

// Over HTTP: nothing is taken before the trip is near, then the driver's
// position reaches the passenger, and stopping takes it away.
func TestLiveLocationOverTheAPI(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	base := "/v1/trips/" + occurrenceID.String()
	here := map[string]any{"lat": 40.85, "lon": 25.85}

	if rec := h.do(http.MethodPut, base+"/position", here, driver); rec.Code != http.StatusConflict ||
		h.errorCode(rec) != "not_sharing_now" {
		t.Fatalf("a day early: %d %s", rec.Code, rec.Body.String())
	}

	h.clock.Set(time.Date(2026, time.July, 30, 4, 15, 0, 0, time.UTC))
	// Sixteen hours on, the access tokens have long expired; sign in again.
	driver, _ = h.signUp("+306900000031", "Οδηγός")
	passenger, _ := h.signUp("+306900000032", "Επιβάτης")
	if rec := h.do(http.MethodPut, base+"/position", here, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodPut, base+"/position", map[string]any{"lat": 40.85}, driver); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT without a longitude: %d", rec.Code)
	}

	var got struct {
		Positions []struct {
			Name string  `json:"name"`
			Lat  float64 `json:"lat"`
		} `json:"positions"`
	}
	h.decode(h.do(http.MethodGet, base+"/live", nil, passenger), &got)
	if len(got.Positions) != 1 || got.Positions[0].Name != "Οδηγός" {
		t.Fatalf("the passenger sees %+v", got.Positions)
	}

	if rec := h.do(http.MethodDelete, base+"/position", nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d", rec.Code)
	}
	h.decode(h.do(http.MethodGet, base+"/live", nil, passenger), &got)
	if len(got.Positions) != 0 {
		t.Fatalf("still shown after stopping: %+v", got.Positions)
	}

	stranger, _ := h.signUp("+306900000799", "Άσχετος")
	if rec := h.do(http.MethodGet, base+"/live", nil, stranger); rec.Code != http.StatusNotFound {
		t.Fatalf("a stranger reading: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, base+"/live", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
}
