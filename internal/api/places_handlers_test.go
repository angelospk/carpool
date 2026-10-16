package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"carpool/internal/api"
)

type placeResult struct {
	Places []struct {
		ID   string  `json:"id"`
		Name string  `json:"name"`
		Kind string  `json:"kind"`
		Lon  float64 `json:"lon"`
		Lat  float64 `json:"lat"`
	} `json:"places"`
}

func searchPlaces(h *harness, token, query string) (*httptest.ResponseRecorder, placeResult) {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/places?q="+url.QueryEscape(query), nil, token)
	var out placeResult
	if rec.Code == http.StatusOK {
		h.decode(rec, &out)
	}
	return rec, out
}

func TestPlacesFindsATownFromAFewLatinLetters(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000501", "Νίκος")

	rec, got := searchPlaces(h, token, "alexandr")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(got.Places) == 0 || got.Places[0].Name != "Αλεξανδρούπολη" {
		t.Fatalf("got %+v", got.Places)
	}
	// The coordinate is the point of the whole endpoint: it is what replaces
	// the "25.8743, 40.8476" the user used to type.
	if got.Places[0].Lon == 0 || got.Places[0].Lat == 0 {
		t.Errorf("no coordinate on %+v", got.Places[0])
	}
	if got.Places[0].ID == "" {
		t.Error("no id, so two places of the same name cannot be told apart")
	}
}

func TestPlacesFindsNothingWithoutSayingItIsAnError(t *testing.T) {
	// A query matching nothing is a normal state of an autocomplete field
	// halfway through a word, not a failure to report.
	h := newHarness(t)
	token, _ := h.signUp("+306900000502", "Νίκος")

	rec, got := searchPlaces(h, token, "zzzzzz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if len(got.Places) != 0 {
		t.Errorf("got %+v", got.Places)
	}
}

func TestPlacesRejectsAnEmptyQuery(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000503", "Νίκος")

	for _, q := range []string{"", "   "} {
		if rec, _ := searchPlaces(h, token, q); rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q gave %d, want 400", q, rec.Code)
		}
	}
}

func TestPlacesCapsHowMuchCanBeAskedFor(t *testing.T) {
	// Without a cap the client picks how much work the server does, and the
	// limit arrives as a string from anybody with a token.
	h := newHarness(t)
	token, _ := h.signUp("+306900000504", "Νίκος")

	for _, limit := range []string{"1000", "-1", "0", "abc"} {
		rec := h.do(http.MethodGet, "/v1/places?q=αλεξ&limit="+limit, nil, token)
		if rec.Code != http.StatusOK {
			t.Errorf("limit=%s gave %d", limit, rec.Code)
			continue
		}
		var out placeResult
		h.decode(rec, &out)
		if len(out.Places) > api.MaxPlaceResults {
			t.Errorf("limit=%s returned %d places, above the cap", limit, len(out.Places))
		}
		if len(out.Places) == 0 {
			t.Errorf("limit=%s returned nothing; a bad limit should not empty the list", limit)
		}
	}
}

// Searching settlements does not need a session, because joining asks for a
// home town before there is one. It is a list of Greek place names out of
// OpenStreetMap; there is nothing here a token would be protecting.
//
// Its neighbour is a different matter: meeting points carry moderator
// decisions and the proposals people have made, so that one stays behind a
// session.
func TestPlacesIsPublicButMeetingPointsAreNot(t *testing.T) {
	h := newHarness(t)

	if rec := h.do(http.MethodGet, "/v1/places?q=alexandr", nil, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /places anonymously: status %d, want 200", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/v1/places/points?place=node%2F139260064", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /places/points anonymously: status %d, want 401", rec.Code)
	}
}

func TestMeetingPointsForACity(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000601", "Νίκος")

	rec := h.do(http.MethodGet, "/v1/places/points?place=node%2F139260064", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Points []struct {
			ID, Name, Kind string
			Lon, Lat       float64
		} `json:"points"`
	}
	h.decode(rec, &out)

	if len(out.Points) == 0 {
		t.Fatal("Αλεξανδρούπολη has nowhere to wait")
	}
	if out.Points[0].Name == "" || out.Points[0].Kind == "" || out.Points[0].Lat == 0 {
		t.Errorf("incomplete meeting point: %+v", out.Points[0])
	}
}

// Four settlements in five have no landmark anybody would recognise. That is an
// ordinary answer the app has something to say about, so it is an empty list
// rather than a 404 — and rather than an invented one.
func TestASettlementWithNoMeetingPointsAnswersEmpty(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000602", "Μαρία")

	rec := h.do(http.MethodGet, "/v1/places/points?place=node%2F1", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 with an empty list", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"points":[]`) {
		t.Errorf("body = %s, want an empty list", rec.Body.String())
	}
}

func TestPlacesNearNamesTheSettlementAtACoordinate(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000509", "Νίκος")

	// A POST: a phone's position in a query string ends up in access logs.
	rec := h.do(http.MethodPost, "/v1/places/near", map[string]any{"lon": 26.3051, "lat": 41.1958}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Place struct {
			Name string  `json:"name"`
			Lon  float64 `json:"lon"`
		} `json:"place"`
	}
	h.decode(rec, &got)
	if got.Place.Name != "Σουφλί" || got.Place.Lon == 0 {
		t.Fatalf("got %+v", got)
	}

	if rec := h.do(http.MethodPost, "/v1/places/near", map[string]any{"lon": 20, "lat": 34}, token); rec.Code != http.StatusNotFound {
		t.Errorf("far out at sea: status %d, want 404", rec.Code)
	}
	for _, body := range []any{
		map[string]any{},
		map[string]any{"lon": "abc", "lat": 41},
		map[string]any{"lon": 26.3, "lat": 95},
		map[string]any{"lon": 26.3},
	} {
		if rec := h.do(http.MethodPost, "/v1/places/near", body, token); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400", body, rec.Code)
		}
	}
	if rec := h.do(http.MethodGet, "/v1/places/near?lon=26.3&lat=41.19", nil, token); rec.Code == http.StatusOK {
		t.Error("the query-string form still answers")
	}
}

// With the other end of the journey in Evros, "Νέα" starts with a place in
// Evros, not with the biggest "Νέα" in Greece (QA 2026-10-16, U12).
func TestPlacesNearTheOtherEndComeFirst(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/v1/places?q="+url.QueryEscape("Νέα")+"&near_lon=26.52&near_lat=41.50", nil, "")
	var got placeResult
	h.decode(rec, &got)
	if len(got.Places) == 0 || got.Places[0].Lon < 25.5 || got.Places[0].Lat < 40.6 {
		t.Fatalf("first near Ορεστιάδα: %+v", got.Places)
	}

	_, far := searchPlaces(h, "", "Νέα")
	if len(far.Places) == 0 || far.Places[0].ID == got.Places[0].ID {
		t.Fatalf("near changed nothing: %+v", far.Places)
	}
}
