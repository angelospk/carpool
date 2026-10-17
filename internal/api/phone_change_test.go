package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type challenge struct {
	ChallengeID string `json:"challenge_id"`
	DevCode     string `json:"dev_code"`
}

func requestChange(h *harness, token, phone string) (*httptest.ResponseRecorder, challenge) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/me/phone/request", map[string]any{"phone": phone}, token)
	var out challenge
	if rec.Code == http.StatusOK {
		h.decode(rec, &out)
	}
	return rec, out
}

func TestChangingTheNumberMovesTheAccount(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000901", "Γιώργος")

	rec, ch := requestChange(h, token, "+306900000902")
	if rec.Code != http.StatusOK {
		t.Fatalf("request: %d %s", rec.Code, rec.Body.String())
	}
	if ch.ChallengeID == "" {
		t.Fatal("no challenge id")
	}
	// The code goes to the new number, because that is the only thing this
	// proves control of.
	sent := h.sms.Sent()
	if len(sent) == 0 || sent[len(sent)-1].Phone != "+306900000902" {
		t.Errorf("the code did not go to the new number: %+v", sent)
	}

	rec = h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}

	var pair struct {
		AccessToken string `json:"access_token"`
	}
	h.decode(rec, &pair)

	var me struct {
		Phone string `json:"phone"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, pair.AccessToken), &me)
	if me.Phone != "+306900000902" {
		t.Errorf("phone is %s", me.Phone)
	}
}

func TestConfirmingCannotNameADifferentNumber(t *testing.T) {
	// The reason confirmation quotes a challenge rather than a phone number. If
	// the number came from the request, a code sent to one you control could be
	// spent against one you do not.
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000903", "Γιώργος")
	_, ch := requestChange(h, token, "+306900000904")

	rec := h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode, "phone": "+306900000999",
	}, token)
	// The field does not exist, so the request is refused rather than quietly
	// doing something else.
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestANumberSomebodyElseHasCannotBeTaken(t *testing.T) {
	h := newHarnessWith(t, true)
	_, _ = h.signUp("+306900000905", "Μαρία")
	token, _ := h.signUp("+306900000906", "Γιώργος")

	// Cleared first, because signing Μαρία up above legitimately texted her.
	// What matters is whether the refused request texts her again, which is how
	// this endpoint would become a way to send SMS to any number at will.
	h.sms.Reset()

	rec, ch := requestChange(h, token, "+306900000905")
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want the same 200 a free number gets", rec.Code)
	}
	if sent := h.sms.Sent(); len(sent) != 0 {
		t.Errorf("a refused request still sent %+v", sent)
	}
	// And no code can ever confirm it.
	rec = h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode,
	}, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("confirm: status %d, want 401 as for a wrong code", rec.Code)
	}
}

// QA 2026-10-17 A 3: whether a number has an account was answered for free
// and without limit (409 for taken, 200 for free, only the free ones counted).
// Both now answer alike and both count against the caller's hourly limit.
func TestTheNumberCheckIsNotAnOracle(t *testing.T) {
	h := newHarnessWith(t, true)
	for i := 0; i < 5; i++ {
		_, _ = h.signUp("+30690000097"+string(rune('0'+i)), "Κάποιος")
	}
	token, _ := h.signUp("+306900000980", "Ψάχνει")

	for i := 0; i < 5; i++ {
		rec, _ := requestChange(h, token, "+30690000097"+string(rune('0'+i)))
		if rec.Code != http.StatusOK {
			t.Fatalf("probe %d: status %d, want 200", i, rec.Code)
		}
	}
	rec, _ := requestChange(h, token, "+306900000989")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("sixth request: status %d, want 429: probing taken numbers must count", rec.Code)
	}
}

func TestChangingToYourOwnNumberIsRefused(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000907", "Γιώργος")

	rec, _ := requestChange(h, token, "+306900000907")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestAWrongCodeDoesNotMoveAnything(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000908", "Γιώργος")
	_, ch := requestChange(h, token, "+306900000909")

	rec := h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": "000000",
	}, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}

	var me struct {
		Phone string `json:"phone"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &me)
	if me.Phone != "+306900000908" {
		t.Errorf("the number moved to %s on a wrong code", me.Phone)
	}
}

func TestAChallengeIsSpentOnce(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000910", "Γιώργος")
	_, ch := requestChange(h, token, "+306900000911")

	body := map[string]any{"challenge_id": ch.ChallengeID, "code": ch.DevCode}
	if rec := h.do(http.MethodPost, "/v1/me/phone/confirm", body, token); rec.Code != http.StatusOK {
		t.Fatalf("first confirm: %d", rec.Code)
	}
	// Replayed with the old token, which is itself now dead.
	if rec := h.do(http.MethodPost, "/v1/me/phone/confirm", body, token); rec.Code == http.StatusOK {
		t.Error("the same challenge was spent twice")
	}
}

func TestOneAccountsChallengeCannotBeUsedByAnother(t *testing.T) {
	h := newHarnessWith(t, true)
	mine, _ := h.signUp("+306900000912", "Γιώργος")
	theirs, _ := h.signUp("+306900000913", "Μαρία")
	_, ch := requestChange(h, mine, "+306900000914")

	rec := h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode,
	}, theirs)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}

func TestTheOldSessionsStopWorking(t *testing.T) {
	// The point of revoking. Somebody holding a ninety-day refresh token from
	// before the change would otherwise keep an account whose identifier has
	// moved to a number they do not have.
	h := newHarnessWith(t, true)
	token, refresh := h.signUp("+306900000915", "Γιώργος")
	_, ch := requestChange(h, token, "+306900000916")

	if rec := h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode,
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/auth/refresh", map[string]any{"refresh_token": refresh}, "")
	if rec.Code == http.StatusOK {
		t.Error("a refresh token from before the change still works")
	}

	// The access token from before the change is NOT revoked, and that is the
	// documented behaviour rather than an oversight: an access token carries no
	// server-side state to revoke, so its blast radius is its fifteen minutes.
	// Pinned here so that changing the policy has to change this test too.
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusOK {
		t.Errorf("the old access token stopped working immediately (status %d); if that is "+
			"now intended, auth.AccessTokenTTL and this test disagree", rec.Code)
	}
}

func TestSignInFollowsTheNumber(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000917", "Γιώργος")

	var original struct {
		ID string `json:"id"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &original)

	_, ch := requestChange(h, token, "+306900000918")
	if rec := h.do(http.MethodPost, "/v1/me/phone/confirm", map[string]any{
		"challenge_id": ch.ChallengeID, "code": ch.DevCode,
	}, token); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}

	// Signing in on the new number reaches the same account, which is the whole
	// point: the person moved, their trips and ratings did not.
	newToken, _ := h.signUp("+306900000918", "Γιώργος")

	var me struct {
		ID    string `json:"id"`
		Phone string `json:"phone"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, newToken), &me)
	if me.Phone != "+306900000918" {
		t.Errorf("signing in on the new number reached %s", me.Phone)
	}
	if me.ID != original.ID {
		t.Errorf("it reached a different account: %s, was %s", me.ID, original.ID)
	}
}

func TestTheCodeIsNotHandedBackOutsideDevMode(t *testing.T) {
	// Same rule as sign-in. Otherwise knowing somebody's session is enough to
	// move their account without ever holding the new phone.
	h := newHarness(t)
	token, _ := h.signUp("+306900000919", "Γιώργος")

	rec, out := requestChange(h, token, "+306900000920")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if out.DevCode != "" {
		t.Errorf("the code came back in the response: %q", out.DevCode)
	}
}

func TestChangingANumberNeedsASession(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodPost, "/v1/me/phone/request",
		map[string]any{"phone": "+306900000921"}, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("request: status %d, want 401", rec.Code)
	}
	// Both halves, not just the first: a challenge id is a uuid somebody could
	// hold, and confirming without a session would be the whole flow bypassed.
	if rec := h.do(http.MethodPost, "/v1/me/phone/confirm",
		map[string]any{"challenge_id": uuid.NewString(), "code": "123456"}, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("confirm: status %d, want 401", rec.Code)
	}
}
