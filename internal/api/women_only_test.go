package api_test

import (
	"net/http"
	"testing"
)

func TestWomenDriversOnlyIsASettingForWomen(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000821", "Νίκη")
	patch := func(body map[string]any) (int, string, bool) {
		rec := h.do(http.MethodPatch, "/v1/me", body, token)
		var me struct {
			WomenOnly bool `json:"women_only"`
		}
		if rec.Code == http.StatusOK {
			h.decode(rec, &me)
		}
		return rec.Code, h.errorCode(rec), me.WomenOnly
	}

	if code, err, _ := patch(map[string]any{"women_only": true}); code != 400 || err != "women_only_needs_female" {
		t.Fatalf("no gender set: %d %s", code, err)
	}
	if code, err, _ := patch(map[string]any{"gender": "male", "women_only": true}); code != 400 || err != "women_only_needs_female" {
		t.Fatalf("male: %d %s", code, err)
	}
	if code, err, _ := patch(map[string]any{"gender": "x"}); code != 400 || err != "invalid_gender" {
		t.Fatalf("unknown gender: %d %s", code, err)
	}
	if code, _, on := patch(map[string]any{"gender": "female", "women_only": true}); code != 200 || !on {
		t.Fatalf("female: %d on=%v", code, on)
	}
	// Changing the gender away turns it off rather than leaving a setting the
	// matcher would apply to somebody it does not describe.
	if code, _, on := patch(map[string]any{"gender": "other"}); code != 200 || on {
		t.Fatalf("after changing gender: %d on=%v", code, on)
	}
}
