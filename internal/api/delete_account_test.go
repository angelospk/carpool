package api_test

import (
	"net/http"
	"testing"
)

// QA 2026-10-17 A14. Deleting an account used to need only the access token, so
// anyone holding a stolen session (or an unlocked phone) could erase it. It now
// needs a fresh SMS code as well.

func requestDeleteCode(h *harness, token string) string {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/me/delete-code", nil, token)
	if rec.Code/100 != 2 {
		h.t.Fatalf("delete-code: %d %s", rec.Code, rec.Body.String())
	}
	return h.sms.LastCode()
}

func TestDeletingWithoutACodeIsRefusedAndNothingHappens(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000801", "Γιώργος")

	rec := h.do(http.MethodDelete, "/v1/me", nil, token)
	if rec.Code != http.StatusBadRequest || h.errorCode(rec) != "code_required" {
		t.Fatalf("no body: %d %s, want 400 code_required", rec.Code, rec.Body.String())
	}
	rec = h.do(http.MethodDelete, "/v1/me", map[string]any{"code": ""}, token)
	if rec.Code != http.StatusBadRequest || h.errorCode(rec) != "code_required" {
		t.Fatalf("empty code: %d %s, want 400 code_required", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("the account was touched: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeletingWithAWrongCodeIsForbidden(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000802", "Γιώργος")
	code := requestDeleteCode(h, token)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}

	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": wrong}, token)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("wrong code: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("the account was touched: %d", rec.Code)
	}
}

func TestDeletingWithoutEverAskingForACodeIsForbidden(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000803", "Γιώργος")

	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": "123456"}, token)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("no code issued: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
	}
}

func TestDeletingWithTheRightCodeAnonymisesTheAccount(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000804", "Γιώργος")
	code := requestDeleteCode(h, token)

	sent := h.sms.Sent()
	if last := sent[len(sent)-1]; last.Phone != "+306900000804" {
		t.Errorf("the code went to %s, want the account's own number", last.Phone)
	}

	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s, want 204", rec.Code, rec.Body.String())
	}
	var status, name string
	h.mustQuery(`SELECT status, name FROM users WHERE phone <> '+306900000804' AND status = 'deleted' LIMIT 1`,
		&status, &name)
	if status != "deleted" || name == "Γιώργος" {
		t.Errorf("not anonymised: status %q name %q", status, name)
	}
	var left int
	h.mustQuery(`SELECT count(*) FROM users WHERE phone = '+306900000804'`, &left)
	if left != 0 {
		t.Errorf("the number is still on an account")
	}
}

func TestADeleteCodeWorksOnce(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000805", "Γιώργος")
	code := requestDeleteCode(h, token)

	if rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("first use: %d %s", rec.Code, rec.Body.String())
	}
	// The same account signed up again must not be erasable with the spent code.
	token2, _ := h.signUp("+306900000805", "Γιώργος ξανά")
	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token2)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("reused code: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/v1/me", nil, token2); rec.Code != http.StatusOK {
		t.Fatalf("the new account was erased by an old code: %d", rec.Code)
	}
}

func TestADeleteCodeIsDeadAfterTooManyWrongGuesses(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000806", "Γιώργος")
	code := requestDeleteCode(h, token)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}

	for i := 0; i < 5; i++ {
		if rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": wrong}, token); rec.Code != http.StatusForbidden {
			t.Fatalf("guess %d: %d", i, rec.Code)
		}
	}
	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("right code after lock: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
	}
}

func TestDeleteCodeRequestsAreLimited(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000807", "Γιώργος")
	for i := 0; i < 5; i++ {
		if rec := h.do(http.MethodPost, "/v1/me/delete-code", nil, token); rec.Code/100 != 2 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	if rec := h.do(http.MethodPost, "/v1/me/delete-code", nil, token); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth request: %d, want 429", rec.Code)
	}
}

func TestAResendReplacesTheEarlierDeleteCode(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000808", "Γιώργος")
	first := requestDeleteCode(h, token)
	second := requestDeleteCode(h, token)
	if first == second {
		t.Skip("the two random codes collided")
	}
	if rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": first}, token); rec.Code != http.StatusForbidden {
		t.Fatalf("superseded code: %d, want 403", rec.Code)
	}
	if rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": second}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("current code: %d %s, want 204", rec.Code, rec.Body.String())
	}
}
