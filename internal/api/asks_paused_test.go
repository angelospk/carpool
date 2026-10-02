package api_test

import (
	"net/http"
	"testing"
)

// The matcher has always skipped a driver whose asks are muted, and nothing
// could mute them: the column was read in two places and written in none. A
// driver on holiday had to retire every route to stop being asked.
func TestDriverCanPauseAsks(t *testing.T) {
	h := newHarness(t)
	h.occurrenceOf("+306900000401", "Οδηγός")
	driver, _ := h.signUp("+306900000401", "Οδηγός")

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{"asks_paused": true}, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: %d %s", rec.Code, rec.Body.String())
	}
	var muted bool
	h.mustQuery(`SELECT notifications_muted FROM driver_profiles d JOIN users u ON u.id = d.user_id
	             WHERE u.phone = '+306900000401'`, &muted)
	if !muted {
		t.Fatal("asks_paused did not reach the column the matcher reads")
	}

	var me struct {
		AsksPaused *bool `json:"asks_paused"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, driver), &me)
	if me.AsksPaused == nil || !*me.AsksPaused {
		t.Errorf("GET /me asks_paused = %v, want true", me.AsksPaused)
	}

	h.do(http.MethodPatch, "/v1/me", map[string]any{"asks_paused": false}, driver)
	h.mustQuery(`SELECT notifications_muted FROM driver_profiles d JOIN users u ON u.id = d.user_id
	             WHERE u.phone = '+306900000401'`, &muted)
	if muted {
		t.Error("unpausing left the driver muted")
	}
}

// Somebody with no car has no asks to pause. Saying so beats storing nothing
// and answering 200.
func TestPausingAsksNeedsACar(t *testing.T) {
	h := newHarness(t)
	passenger, _ := h.signUp("+306900000402", "Επιβάτης")

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{"asks_paused": true}, passenger)
	if rec.Code != http.StatusConflict || h.errorCode(rec) != "not_a_driver" {
		t.Fatalf("status = %d code = %q, want 409 not_a_driver", rec.Code, h.errorCode(rec))
	}
	var me struct {
		AsksPaused *bool `json:"asks_paused"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, passenger), &me)
	if me.AsksPaused != nil {
		t.Errorf("a passenger's asks_paused = %v, want absent", *me.AsksPaused)
	}
}

// A pause asked for alongside a change that is then refused is refused with
// it. It used to be written first and survive the refusal.
func TestAPauseDoesNotSurviveARefusedUpdate(t *testing.T) {
	h := newHarness(t)
	h.occurrenceOf("+306900000411", "Οδηγός")
	driver, _ := h.signUp("+306900000411", "Οδηγός")

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{
		"asks_paused":    true,
		"home_region_id": "00000000-0000-0000-0000-000000000000",
	}, driver)
	if rec.Code < 400 {
		t.Fatalf("an unknown region was accepted: %d", rec.Code)
	}
	var muted bool
	h.mustQuery(`SELECT notifications_muted FROM driver_profiles d JOIN users u ON u.id = d.user_id
	             WHERE u.phone = '+306900000411'`, &muted)
	if muted {
		t.Error("the pause was written although the update was refused")
	}
}
