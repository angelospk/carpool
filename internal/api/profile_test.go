package api_test

import (
	"net/http"
	"strings"
	"testing"

	"carpool/internal/domain"
)

// Somebody deciding whether to get into a stranger's car has a photo, a name
// and a rating. All three say who; none says what the two hours will be like.
func TestAProfileCarriesAFewWordsAndSomeTags(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306902000001", "Χαράλαμπος")

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{
		"bio":         "Δουλεύω στην Αλεξανδρούπολη, κατεβαίνω Τρίτη και Πέμπτη.",
		"preferences": []string{"no_smoking", "chatty", "big_luggage_ok"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body.String())
	}

	var me struct {
		Bio         *string  `json:"bio"`
		Preferences []string `json:"preferences"`
	}
	h.decode(rec, &me)
	if me.Bio == nil || !strings.Contains(*me.Bio, "Αλεξανδρούπολη") {
		t.Errorf("bio came back as %v", me.Bio)
	}
	if len(me.Preferences) != 3 {
		t.Errorf("%d preferences, want 3: %v", len(me.Preferences), me.Preferences)
	}
}

// Preferences are replaced, not merged. "Untick the one I ticked by mistake"
// has to be expressible, and a merge cannot express it.
func TestSendingPreferencesReplacesThemRatherThanAddingToThem(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306902000002", "Χαράλαμπος")

	h.do(http.MethodPatch, "/v1/me", map[string]any{
		"preferences": []string{"no_smoking", "chatty"},
	}, token)

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{
		"preferences": []string{"pets_ok"},
	}, token)
	var me struct {
		Preferences []string `json:"preferences"`
	}
	h.decode(rec, &me)

	if len(me.Preferences) != 1 || me.Preferences[0] != "pets_ok" {
		t.Errorf("preferences = %v, want just pets_ok", me.Preferences)
	}
}

// Leaving them out of the request leaves them alone, like every other field on
// this endpoint. Sending an empty list clears them, which is a different act.
func TestPreferencesLeftOutAreLeftAloneAndAnEmptyListClearsThem(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306902000003", "Χαράλαμπος")

	h.do(http.MethodPatch, "/v1/me", map[string]any{"preferences": []string{"chatty"}}, token)

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{"name": "Μπάμπης"}, token)
	var me struct {
		Preferences []string `json:"preferences"`
	}
	h.decode(rec, &me)
	if len(me.Preferences) != 1 {
		t.Errorf("a name change wiped the preferences: %v", me.Preferences)
	}

	rec = h.do(http.MethodPatch, "/v1/me", map[string]any{"preferences": []string{}}, token)
	h.decode(rec, &me)
	if len(me.Preferences) != 0 {
		t.Errorf("an empty list left %v behind", me.Preferences)
	}
}

func TestAProfileRefusesWhatItCannotShow(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306902000004", "Χαράλαμπος")

	for _, tt := range []struct {
		name string
		body map[string]any
		code string
	}{
		{"a bio longer than a profile takes",
			map[string]any{"bio": strings.Repeat("α", domain.MaxBioRunes+1)}, "bio_too_long"},
		{"a tag from nowhere",
			map[string]any{"preferences": []string{"καπνιστής"}}, "unknown_preference"},
		{"two tags that cannot both hold",
			map[string]any{"preferences": []string{"no_smoking", "smoking_ok"}}, "contradictory_preferences"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPatch, "/v1/me", tt.body, token)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
			if code := h.errorCode(rec); code != tt.code {
				t.Errorf("error code = %q, want %q", code, tt.code)
			}
		})
	}
}

// A profile with nothing filled in is an empty list, not a missing field, so
// every client renders it the same way.
func TestAnEmptyProfileStillCarriesAList(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306902000005", "Χαράλαμπος")

	rec := h.do(http.MethodGet, "/v1/me", nil, token)
	var raw map[string]any
	h.decode(rec, &raw)

	prefs, present := raw["preferences"]
	if !present {
		t.Fatal("no preferences field at all")
	}
	if prefs == nil {
		t.Error("preferences came back as null rather than an empty list")
	}
}

// Where it matters. A passenger deciding whether to get in is on the trip
// screen, not on a profile page they would have to go and find.
func TestTheTripCarriesWhatTheRideIsLike(t *testing.T) {
	h := newHarness(t)
	driverToken, passengerToken, occurrenceID := h.bookedTrip(t)

	if rec := h.do(http.MethodPatch, "/v1/me", map[string]any{
		"bio":         "Ξεκινάω νωρίς και σταματάω μία φορά για καφέ.",
		"preferences": []string{"no_smoking", "music_on"},
	}, driverToken); rec.Code != http.StatusOK {
		t.Fatalf("setting the driver's profile: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, passengerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Driver struct {
			Bio         *string  `json:"bio"`
			Preferences []string `json:"preferences"`
		} `json:"driver"`
		Passengers []struct {
			Preferences []string `json:"preferences"`
		} `json:"passengers"`
	}
	h.decode(rec, &body)

	if body.Driver.Bio == nil || !strings.Contains(*body.Driver.Bio, "καφέ") {
		t.Errorf("the driver's own words did not travel: %v", body.Driver.Bio)
	}
	if len(body.Driver.Preferences) != 2 {
		t.Errorf("driver preferences = %v, want two", body.Driver.Preferences)
	}
	// And a passenger who has set none is an empty list, not a missing field.
	if body.Passengers[0].Preferences == nil {
		t.Error("a passenger with no preferences came back as null")
	}
}
