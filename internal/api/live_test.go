package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"carpool/internal/api"
	"carpool/internal/geo"
	"carpool/internal/live"
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

// The passenger is told when the phone took the fix, not only when it arrived:
// a fix taken a minute and a half ago and uploaded now is a minute and a half old.
func TestLiveGivesWhenTheFixWasTaken(t *testing.T) {
	h := newHarness(t)
	_, _, occurrenceID := h.bookedTrip(t)
	base := "/v1/trips/" + occurrenceID.String()
	h.clock.Set(time.Date(2026, time.July, 30, 4, 15, 0, 0, time.UTC))
	driver, _ := h.signUp("+306900000031", "Οδηγός")
	passenger, _ := h.signUp("+306900000032", "Επιβάτης")
	taken := h.clock.Now().Add(-90 * time.Second)
	if rec := h.do(http.MethodPut, base+"/position",
		map[string]any{"lat": 40.85, "lon": 25.85, "observed_at": taken}, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Positions []struct {
			UpdatedAt  time.Time `json:"updated_at"`
			ObservedAt time.Time `json:"observed_at"`
		} `json:"positions"`
	}
	h.decode(h.do(http.MethodGet, base+"/live", nil, passenger), &got)
	if len(got.Positions) != 1 || !got.Positions[0].ObservedAt.Equal(taken) ||
		!got.Positions[0].UpdatedAt.Equal(h.clock.Now()) {
		t.Fatalf("positions = %+v, want observed %v and updated now", got.Positions, taken)
	}
}

type eightMinutes struct{}

func (eightMinutes) Route(context.Context, []geo.Point) (geo.Route, error) {
	return geo.Route{Duration: 8 * time.Minute}, nil
}

// The passenger is told how far the driver is; the driver is not.
func TestLiveAnswersTheDriversETAToThePassengerOnly(t *testing.T) {
	h := newHarnessDeps(t, false, nil, func(d *api.Deps) {
		d.Live = live.NewService(d.Pool, d.Clock).WithRouter(eightMinutes{})
	})
	driver, _, occurrenceID := h.bookedTrip(t)
	base := "/v1/trips/" + occurrenceID.String()
	h.clock.Set(time.Date(2026, time.July, 30, 4, 15, 0, 0, time.UTC))
	driver, _ = h.signUp("+306900000031", "Οδηγός")
	passenger, _ := h.signUp("+306900000032", "Επιβάτης")
	if rec := h.do(http.MethodPut, base+"/position", map[string]any{"lat": 40.85, "lon": 25.85}, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	h.decode(h.do(http.MethodGet, base+"/live", nil, passenger), &got)
	if got["driver_eta_min"] != float64(8) || got["eta_at"] == nil {
		t.Fatalf("passenger body = %v, want driver_eta_min 8 and eta_at", got)
	}
	got = nil
	h.decode(h.do(http.MethodGet, base+"/live", nil, driver), &got)
	if _, has := got["driver_eta_min"]; has {
		t.Fatalf("driver body has an ETA: %v", got)
	}
}
