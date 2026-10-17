package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Autocomplete types fast, so the ceiling sits well above that; a script
// walking the list is what it is for. Both routes need no account and share
// the address's count.
func TestPlacesSearchHasAPerAddressCeiling(t *testing.T) {
	h := newHarness(t)

	for i := range 120 {
		if rec := h.do(http.MethodGet, "/v1/places?q=%CE%91%CE%BB", nil, ""); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused: typing in a search box should not hit this", i+1)
		}
	}
	if rec := h.do(http.MethodGet, "/v1/places?q=%CE%91%CE%BB", nil, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 121: %d %s, want 429", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodPost, "/v1/places/near", map[string]any{"lat": 40.85, "lon": 25.87}, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("places/near: %d %s, want 429 from the shared count", rec.Code, rec.Body.String())
	}
}

func TestEstimatesHaveAPerPersonCeiling(t *testing.T) {
	h := newHarnessRoutedBy(t, "http://127.0.0.1:1")
	token, _ := h.signUp("+306900000821", "Επιβάτης")

	// A body that fails validation is enough: the count is taken before it.
	for i := range 30 {
		if rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{}, token); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused", i+1)
		}
	}
	if rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{}, token); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31: %d %s, want 429", rec.Code, rec.Body.String())
	}

	other, _ := h.signUp("+306900000822", "Άλλος")
	if rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{}, other); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a second person was refused for the first person's estimates")
	}
}

func TestPositionUpdatesHaveAPerPersonCeiling(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000823", "Οδηγός")
	path := "/v1/trips/" + uuid.NewString() + "/position"
	here := map[string]any{"lat": 40.85, "lon": 25.85}

	// The trip does not exist; the count is taken before that is found out.
	for i := range 60 {
		if rec := h.do(http.MethodPut, path, here, token); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("update %d was refused: a phone sends far fewer", i+1)
		}
	}
	if rec := h.do(http.MethodPut, path, here, token); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("update 61: %d %s, want 429", rec.Code, rec.Body.String())
	}

	other, _ := h.signUp("+306900000824", "Άλλος")
	if rec := h.do(http.MethodPut, path, here, other); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a second person was refused for the first person's updates")
	}
}
