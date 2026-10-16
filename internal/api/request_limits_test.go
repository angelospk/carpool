package api_test

import (
	"context"
	"maps"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"carpool/internal/geo"
)

func requestBody(h *harness, over map[string]any) map[string]any {
	b := map[string]any{
		"origin_lon": alexLon, "origin_lat": alexLat,
		"destination_lon": komoLon, "destination_lat": komoLat,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Κομοτηνή",
		"window_start": h.clock.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(4 * time.Hour).Format(time.RFC3339),
		"seats":        1,
	}
	maps.Copy(b, over)
	return b
}

func TestARequestHasToBeOneThatCouldBeAnswered(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000811", "Επιβάτης")
	at := func(d time.Duration) string { return h.clock.Now().Add(d).Format(time.RFC3339) }

	for _, tc := range []struct {
		name string
		over map[string]any
		code string
	}{
		{"no seats", map[string]any{"seats": 0}, "invalid_seats"},
		{"negative seats", map[string]any{"seats": -3}, "invalid_seats"},
		{"five seats", map[string]any{"seats": 5}, "invalid_seats"},
		{"window gone by", map[string]any{"window_start": at(-4 * time.Hour), "window_end": at(-2 * time.Hour)}, "window_in_past"},
		{"next year", map[string]any{"window_start": at(365 * 24 * time.Hour), "window_end": at(366 * 24 * time.Hour)}, "window_too_far"},
		{"a month wide", map[string]any{"window_end": at(30 * 24 * time.Hour)}, "window_too_far"},
		{"ending past the horizon", map[string]any{"window_start": at(20 * 24 * time.Hour), "window_end": at(22 * 24 * time.Hour)}, "window_too_far"},
		{"going nowhere", map[string]any{"destination_lon": alexLon, "destination_lat": alexLat}, "too_short"},
	} {
		rec := h.do(http.MethodPost, "/v1/requests", requestBody(h, tc.over), token)
		if rec.Code != http.StatusBadRequest || h.errorCode(rec) != tc.code {
			t.Errorf("%s: %d %s, want 400 %s", tc.name, rec.Code, rec.Body.String(), tc.code)
		}
	}

	// Started an hour ago and still open is an ordinary request.
	rec := h.do(http.MethodPost, "/v1/requests", requestBody(h, map[string]any{
		"window_start": at(-time.Hour), "seats": nil}), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a window already open: %d %s", rec.Code, rec.Body.String())
	}
}

// A double tap, or a retry over a bad connection, used to make a second
// request, and every copy asked the same driver about the same trip.
func TestTheSameRequestTwiceIsOneRequest(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000812", "Επιβάτης")

	var first, second struct {
		RequestID string `json:"request_id"`
	}
	rec := h.do(http.MethodPost, "/v1/requests", requestBody(h, nil), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	h.decode(rec, &first)

	// Near enough to be the same journey: a few metres off, the window nudged.
	rec = h.do(http.MethodPost, "/v1/requests", requestBody(h, map[string]any{
		"origin_lon": alexLon + 0.001,
		"window_end": h.clock.Now().Add(5 * time.Hour).Format(time.RFC3339),
	}), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("second: %d %s, want 200 with the first", rec.Code, rec.Body.String())
	}
	h.decode(rec, &second)
	if second.RequestID != first.RequestID {
		t.Fatalf("second request %s, want the first %s", second.RequestID, first.RequestID)
	}

	var n int
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE passenger_id = (SELECT id FROM users WHERE phone = $1)`,
		&n, "+306900000812")
	if n != 1 {
		t.Fatalf("%d requests stored, want 1", n)
	}

	// Once its window has passed it is over, and the same journey asked for
	// again is a new request rather than the dead one handed back.
	h.mustExec(`UPDATE ride_requests SET window_start = $1, window_end = $2 WHERE id = $3::uuid`,
		h.clock.Now().Add(-3*time.Hour), h.clock.Now().Add(-time.Minute), first.RequestID)
	rec = h.do(http.MethodPost, "/v1/requests", requestBody(h, nil), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("after the first one's window passed: %d %s, want a new request", rec.Code, rec.Body.String())
	}

	// Another day is another journey.
	rec = h.do(http.MethodPost, "/v1/requests", requestBody(h, map[string]any{
		"window_start": h.clock.Now().Add(26 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(28 * time.Hour).Format(time.RFC3339),
	}), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tomorrow: %d %s, want a new request", rec.Code, rec.Body.String())
	}
}

// Two copies arriving together, the way a retry races the original.
func TestTwoCopiesAtOnceAreOneRequest(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000813", "Επιβάτης")
	body := requestBody(h, nil)

	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = h.do(http.MethodPost, "/v1/requests", body, token).Code
		}()
	}
	wg.Wait()

	var n int
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE passenger_id = (SELECT id FROM users WHERE phone = $1)`,
		&n, "+306900000813")
	if n != 1 {
		t.Fatalf("%d requests stored from four simultaneous copies (codes %v), want 1", n, codes)
	}
	created, same := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			same++
		}
	}
	if created != 1 || same != 3 {
		t.Fatalf("codes %v, want one 201 and three 200", codes)
	}
}

// Near is not the same: a different party, or a different bag, is a
// different request.
func TestADifferentPartyIsADifferentRequest(t *testing.T) {
	h := newHarness(t)
	twoPrefectures(h)
	token, _ := h.signUp("+306900000814", "Επιβάτης")
	if rec := h.do(http.MethodPost, "/v1/requests", requestBody(h, nil), token); rec.Code != http.StatusCreated {
		t.Fatalf("first: %d", rec.Code)
	}
	for name, over := range map[string]map[string]any{
		"two seats":  {"seats": 2},
		"a suitcase": {"luggage": "suitcase", "luggage_count": 1},
	} {
		if rec := h.do(http.MethodPost, "/v1/requests", requestBody(h, over), token); rec.Code != http.StatusCreated {
			t.Errorf("%s: %d %s, want a new request", name, rec.Code, rec.Body.String())
		}
	}
}

// A retry of a request nobody could be asked about asks again. The first post
// found no driver; by the time the same journey is posted again there is one,
// and "you already asked" must not leave them waiting for the reconciler.
func TestAskingAgainForTheSameJourneyLooksAgain(t *testing.T) {
	h := newRoutedMatcherHarness(t)
	token, _ := h.signUp("+306900000815", "Επιβάτης")
	body := requestBody(h, map[string]any{
		"destination_lon": 26.1717, "destination_lat": 40.8975, "destination_label": "Φέρες",
		"window_start": h.clock.Now().Add(23 * time.Hour).Format(time.RFC3339),
		"window_end":   h.clock.Now().Add(26 * time.Hour).Format(time.RFC3339),
	})

	var got struct {
		RequestID string `json:"request_id"`
		Asking    bool   `json:"asking"`
		AskID     string `json:"ask_id"`
	}
	rec := h.do(http.MethodPost, "/v1/requests", body, token)
	h.decode(rec, &got)
	if rec.Code != http.StatusCreated || got.Asking {
		t.Fatalf("first: %d %s, want a request nobody could be asked about yet", rec.Code, rec.Body.String())
	}
	first := got.RequestID

	h.occurrenceOf("+306900000816", "Οδηγός")

	rec = h.do(http.MethodPost, "/v1/requests", body, token)
	h.decode(rec, &got)
	if rec.Code != http.StatusOK || got.RequestID != first || !got.Asking || got.AskID == "" {
		t.Fatalf("second: %d %s, want the same request, now asking", rec.Code, rec.Body.String())
	}
}

type straightLines struct{}

func (straightLines) Route(_ context.Context, waypoints []geo.Point) (geo.Route, error) {
	var km float64
	for i := 1; i < len(waypoints); i++ {
		a, b := waypoints[i-1], waypoints[i]
		km += 111 * math.Hypot(b.Lat-a.Lat, (b.Lon-a.Lon)*math.Cos(a.Lat*math.Pi/180))
	}
	return geo.Route{DistanceMeters: km * 1000, Duration: time.Duration(km / 60 * float64(time.Hour))}, nil
}

// Four copies of the same post at once. One request, one driver asked, and
// every answer says so. One of them used to answer asking:false: its own
// attempt to ask lost to another copy's, and it reported its loss rather than
// the ask that exists (QA 2026-10-16, B5).
func TestCopiesOfOnePostAllSayADriverIsBeingAsked(t *testing.T) {
	for round := range 5 {
		h := newRoutedMatcherHarness(t)
		token, _ := h.signUp("+306900000817", "Επιβάτης")
		h.occurrenceOf("+306900000818", "Οδηγός")
		body := requestBody(h, map[string]any{
			"destination_lon": 26.1717, "destination_lat": 40.8975, "destination_label": "Φέρες",
			"window_start": h.clock.Now().Add(23 * time.Hour).Format(time.RFC3339),
			"window_end":   h.clock.Now().Add(26 * time.Hour).Format(time.RFC3339),
		})

		type answer struct {
			code      int
			RequestID string `json:"request_id"`
			Asking    bool   `json:"asking"`
			AskID     string `json:"ask_id"`
		}
		answers := make([]answer, 4)
		var wg sync.WaitGroup
		for i := range answers {
			wg.Go(func() {
				rec := h.do(http.MethodPost, "/v1/requests", body, token)
				answers[i].code = rec.Code
				h.decode(rec, &answers[i])
			})
		}
		wg.Wait()

		var asks int
		h.mustQuery(`SELECT count(*) FROM asks WHERE status = 'pending'`, &asks)
		if asks != 1 {
			t.Fatalf("round %d: %d drivers asked, want 1", round, asks)
		}
		for i, a := range answers {
			if a.code/100 != 2 || !a.Asking || a.AskID == "" || a.AskID != answers[0].AskID || a.RequestID != answers[0].RequestID {
				t.Fatalf("round %d, copy %d: %+v; all: %+v", round, i, a, answers)
			}
		}
	}
}
