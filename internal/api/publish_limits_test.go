package api_test

import (
	"maps"
	"net/http"
	"testing"
)

// The harness clock is 29 July 2026, 12:00 UTC: 15:00 in Athens.

// A valid route body, from somebody with no car yet. Getting as far as
// no_driver_profile is how a test tells that validation let the body through
// without routing it: that check comes after every one of these and before
// OSRM.
func routeBody(over map[string]any) map[string]any {
	b := map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476, "origin_label": "Αλεξανδρούπολη",
		"destination_lon": 25.4048, "destination_lat": 41.1224, "destination_label": "Κομοτηνή",
		"kind": "oneoff", "oneoff_date": "2026-07-31",
		"depart_earliest": "09:00", "depart_latest": "09:30",
		"seats": 2, "pricing_mode": "formula",
	}
	maps.Copy(b, over)
	return b
}

func TestPublishingRefusesWhatCouldNeverDepart(t *testing.T) {
	h := newHarnessRoutedBy(t, "http://127.0.0.1:1")
	token, _ := h.signUp("+306900000801", "Οδηγός")

	for _, tc := range []struct {
		name string
		over map[string]any
		code string
	}{
		{"no seats", map[string]any{"seats": 0}, "invalid_seats"},
		{"negative seats", map[string]any{"seats": -1}, "invalid_seats"},
		{"nine seats", map[string]any{"seats": 9}, "invalid_seats"},
		{"a date gone by", map[string]any{"oneoff_date": "2026-07-01"}, "oneoff_in_past"},
		{"today, window over", map[string]any{"oneoff_date": "2026-07-29"}, "oneoff_in_past"},
		// 15:00 in Athens: the earliest departure has gone, and the matcher
		// already treats a departure as left from that moment.
		{"today, window under way", map[string]any{"oneoff_date": "2026-07-29", "depart_earliest": "14:50", "depart_latest": "15:30"}, "oneoff_in_past"},
		{"beyond the horizon", map[string]any{"oneoff_date": "2026-08-19"}, "oneoff_too_far"},
		{"window backwards", map[string]any{"depart_earliest": "10:00", "depart_latest": "09:00"}, "invalid_window"},
		{"across midnight", map[string]any{"depart_earliest": "23:50", "depart_latest": "00:20"}, "invalid_window"},
		{"same place", map[string]any{"destination_lon": 25.8743, "destination_lat": 40.8476}, "too_short"},
		{"round the block", map[string]any{"destination_lon": 25.8770, "destination_lat": 40.8490}, "too_short"},
	} {
		rec := h.do(http.MethodPost, "/v1/routes", routeBody(tc.over), token)
		if rec.Code != http.StatusBadRequest || h.errorCode(rec) != tc.code {
			t.Errorf("%s: %d %s, want 400 %s", tc.name, rec.Code, rec.Body.String(), tc.code)
		}
	}

	for _, tc := range []struct {
		name string
		over map[string]any
	}{
		{"no seats said", map[string]any{"seats": nil}},
		{"today, window still ahead", map[string]any{"oneoff_date": "2026-07-29", "depart_earliest": "16:00", "depart_latest": "16:30"}},
		{"the last day of the horizon", map[string]any{"oneoff_date": "2026-08-18"}},
		{"weekly", map[string]any{"kind": "recurring", "weekdays": []int{3, 1, 3}}},
	} {
		rec := h.do(http.MethodPost, "/v1/routes", routeBody(tc.over), token)
		if h.errorCode(rec) != "no_driver_profile" {
			t.Errorf("%s: %d %s, want it past validation", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// Publishing is something a person does a few times a day. Without a ceiling
// a script could fill the schedule with recurring routes, each materialising
// three weeks of departures that every search then walks past.
func TestPublishingHasAnHourlyCeilingPerPerson(t *testing.T) {
	h := newHarnessRoutedBy(t, "http://127.0.0.1:1")
	token, _ := h.signUp("+306900000809", "Οδηγός")

	for i := range 20 {
		if rec := h.do(http.MethodPost, "/v1/routes", routeBody(nil), token); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was refused: a person publishing their week should not hit this", i+1)
		}
	}
	if rec := h.do(http.MethodPost, "/v1/routes", routeBody(nil), token); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 21: %d %s, want 429", rec.Code, rec.Body.String())
	}

	// Somebody else is not held to the first person's count.
	other, _ := h.signUp("+306900000810", "Άλλος")
	if rec := h.do(http.MethodPost, "/v1/routes", routeBody(nil), other); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a second person was refused for the first person's publishing")
	}
}
