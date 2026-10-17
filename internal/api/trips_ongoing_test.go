package api_test

import (
	"net/http"
	"testing"
	"time"
)

// listed reports whether the occurrence is in the given trip list for token.
func (h *harness) listed(t *testing.T, path, token, id string) bool {
	t.Helper()
	rec := h.do(http.MethodGet, path, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d; body %s", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Trips []struct {
			ID string `json:"id"`
		} `json:"trips"`
	}
	h.decode(rec, &body)
	for _, tr := range body.Trips {
		if tr.ID == id {
			return true
		}
	}
	return false
}

// QA 2026-10-17 U F1: a trip on the road is still the next trip, not an old
// one. It used to drop off the driver's home and into the passenger's past
// trips the minute the departure window closed, while the car was still
// driving: a trip ends when it arrives (departure plus the route's minutes),
// or, for a drive, when the driver taps «Τέλος».
func TestATripOnTheRoadIsStillUpcoming(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occ := h.bookedTrip(t)
	id := occ.String()
	now := h.clock.Now()

	// route_min is 20: left ten minutes ago, arrives in ten.
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $1`, now.Add(-10*time.Minute))
	for name, token := range map[string]string{"driver": driver, "passenger": passenger} {
		if !h.listed(t, "/v1/trips/upcoming", token, id) {
			t.Errorf("%s: a trip still on the road is missing from upcoming", name)
		}
	}
	if h.listed(t, "/v1/trips/past", passenger, id) {
		t.Errorf("a trip still on the road is listed as past")
	}
	// Confirmed by both on the way: still on the road, still the next trip.
	// Through the real endpoint: both sides confirming also completes the
	// occurrence itself.
	for _, token := range []string{driver, passenger} {
		if rec := h.do(http.MethodPost, "/v1/trips/"+id+"/confirm", nil, token); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("confirm: status %d; body %s", rec.Code, rec.Body.String())
		}
	}
	for name, token := range map[string]string{"driver": driver, "passenger": passenger} {
		if !h.listed(t, "/v1/trips/upcoming", token, id) {
			t.Errorf("%s: a trip confirmed on the road is missing from upcoming", name)
		}
	}

	// Arrived: it is past.
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $1`, now.Add(-30*time.Minute))
	if h.listed(t, "/v1/trips/upcoming", driver, id) {
		t.Errorf("an arrived trip is still upcoming")
	}
	if !h.listed(t, "/v1/trips/past", passenger, id) {
		t.Errorf("an arrived trip is missing from past")
	}

	// A drive runs until «Τέλος», even when it runs late.
	h.mustExec(`UPDATE trip_occurrences SET drive_set_at = $1`, now.Add(-40*time.Minute))
	if !h.listed(t, "/v1/trips/upcoming", driver, id) {
		t.Errorf("a drive not yet ended is missing from upcoming")
	}
	h.mustExec(`UPDATE trip_occurrences SET drive_ended_at = $1`, now.Add(-time.Minute))
	if h.listed(t, "/v1/trips/upcoming", driver, id) {
		t.Errorf("an ended drive is still upcoming")
	}
}
