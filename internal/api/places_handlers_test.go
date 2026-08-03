package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
		rec := h.do(http.MethodGet, "/v1/places?q=α&limit="+limit, nil, token)
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

func TestPlacesNeedsASession(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/v1/places?q=alexandr", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}
