package api_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/auth"
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

// Spent twice for the same user, with the service itself: through the API the
// first spend erases the account, so a second request would be refused for that
// reason rather than because the code was used.
func TestTheSameUsersDeleteCodeCannotBeSpentTwice(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000809", "Γιώργος")
	var userID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000809'`, &userID)
	code := requestDeleteCode(h, token)

	svc := auth.NewDeleteCodeService(h.pool, h.clock, h.sms, []byte(testSecret))
	if err := svc.Spend(context.Background(), userID, code); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := svc.Spend(context.Background(), userID, code); !errors.Is(err, auth.ErrDeleteCodeFailed) {
		t.Fatalf("second spend: %v, want ErrDeleteCodeFailed", err)
	}
}

func TestTwoRedemptionsOfOneDeleteCodeAtOnceLetExactlyOneThrough(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000810", "Γιώργος")
	var userID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000810'`, &userID)
	code := requestDeleteCode(h, token)

	svc := auth.NewDeleteCodeService(h.pool, h.clock, h.sms, []byte(testSecret))
	const n = 2
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = svc.Spend(context.Background(), userID, code)
		}()
	}
	close(start)
	wg.Wait()

	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, auth.ErrDeleteCodeFailed):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d spends succeeded, want exactly 1: %v", won, errs)
	}
}

// A failed erasure must not burn the code: the person tries again.
func TestAFailedErasureLeavesTheDeleteCodeUsable(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000811", "Γιώργος")
	code := requestDeleteCode(h, token)

	h.mustExec(`
		CREATE FUNCTION test_refuse_erasure() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'erasure refused for the test'; END $$`)
	h.mustExec(`
		CREATE TRIGGER test_refuse_erasure BEFORE UPDATE ON users
		FOR EACH ROW WHEN (NEW.status = 'deleted' AND OLD.phone = '+306900000811')
		EXECUTE FUNCTION test_refuse_erasure()`)
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		h.mustExec(`DROP TRIGGER test_refuse_erasure ON users`)
		h.mustExec(`DROP FUNCTION test_refuse_erasure()`)
	}
	t.Cleanup(drop)

	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing erasure: %d %s, want 500", rec.Code, rec.Body.String())
	}
	drop()

	rec = h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retry with the same code: %d %s, want 204", rec.Code, rec.Body.String())
	}
}

// A code texted to the number the account has since left must not erase it.
func TestADeleteCodeSentToTheOldNumberDiesWithTheChange(t *testing.T) {
	h := newHarnessWith(t, true)
	token, _ := h.signUp("+306900000812", "Γιώργος")
	oldCode := requestDeleteCode(h, token)

	rec, ch := requestChange(h, token, "+306900000813")
	if rec.Code != http.StatusOK {
		t.Fatalf("request: %d %s", rec.Code, rec.Body.String())
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

	rec = h.do(http.MethodDelete, "/v1/me", map[string]any{"code": oldCode}, pair.AccessToken)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("code for the old number: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/v1/me", nil, pair.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("the account was erased: %d", rec.Code)
	}
}

// Even if the old code row were still live, it is tied to the number it went to.
func TestADeleteCodeIsTiedToTheNumberItWasSentTo(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000814", "Γιώργος")
	code := requestDeleteCode(h, token)
	h.mustExec(`UPDATE users SET phone = '+306900000815' WHERE phone = '+306900000814'`)

	rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": code}, token)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "bad_code" {
		t.Fatalf("code for another number: %d %s, want 403 bad_code", rec.Code, rec.Body.String())
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

// Codex review, wave 3: a resend and a redemption at the same moment take the
// account and the code rows in one order, so neither deadlocks the other.
func TestAResendDuringARedemptionDoesNotDeadlock(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000811", "Γιώργος")
	var userID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000811'`, &userID)
	requestDeleteCode(h, token)

	svc := auth.NewDeleteCodeService(h.pool, h.clock, h.sms, []byte(testSecret))
	ctx := context.Background()
	for round := 0; round < 4; round++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		var resendErr, spendErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, resendErr = svc.Request(ctx, userID)
		}()
		go func() {
			defer wg.Done()
			<-start
			spendErr = svc.Spend(ctx, userID, "000000")
		}()
		close(start)
		wg.Wait()
		if resendErr != nil {
			t.Fatalf("round %d: resend: %v", round, resendErr)
		}
		if spendErr != nil && !errors.Is(spendErr, auth.ErrDeleteCodeFailed) {
			t.Fatalf("round %d: spend: %v", round, spendErr)
		}
	}
}
