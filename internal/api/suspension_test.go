package api_test

import (
	"net/http"
	"testing"
)

// A suspended account has to stop working everywhere, not only where somebody
// remembered to check.
//
// Nothing read users.status until now. The column has existed since the first
// migration, three values deep, and a row set to 'suspended' carried on booking
// rides — which meant the moderation queue could reach a decision the app then
// ignored.
func TestASuspendedAccountCannotUseTheApp(t *testing.T) {
	h := newHarness(t)
	token, refresh := h.signUp("+306900000801", "Παραβάτης")

	// It works first, so the test cannot pass on a token that was never valid.
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("before suspension: GET /me = %d", rec.Code)
	}

	h.mustExec(`UPDATE users SET status = 'suspended' WHERE phone = $1`, "+306900000801")

	t.Run("an authenticated request", func(t *testing.T) {
		rec := h.do(http.MethodGet, "/v1/me", nil, token)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
		}
		if code := h.errorCode(rec); code != "account_suspended" {
			t.Errorf("error code = %q, want account_suspended", code)
		}
	})

	t.Run("refreshing the session", func(t *testing.T) {
		rec := h.do(http.MethodPost, "/v1/auth/refresh",
			map[string]any{"refresh_token": refresh}, "")
		if rec.Code == http.StatusOK {
			t.Fatalf("a suspended account refreshed its way to a new token: %s", rec.Body.String())
		}
	})

	t.Run("signing in again", func(t *testing.T) {
		if rec := h.do(http.MethodPost, "/v1/auth/otp/request",
			map[string]any{"phone": "+306900000801"}, ""); rec.Code >= 300 {
			t.Fatalf("otp/request: %d", rec.Code)
		}
		rec := h.do(http.MethodPost, "/v1/auth/otp/verify",
			map[string]any{"phone": "+306900000801", "code": h.sms.LastCode()}, "")

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
		}
		if code := h.errorCode(rec); code != "account_suspended" {
			t.Errorf("error code = %q, want account_suspended", code)
		}
	})
}

// A deleted account is refused too, and by the same door. Somebody who asked to
// be erased must not be able to walk back in on an old token.
func TestADeletedAccountCannotUseTheApp(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000802", "Φευγάτος")
	h.mustExec(`UPDATE users SET status = 'deleted' WHERE phone = $1`, "+306900000802")

	rec := h.do(http.MethodGet, "/v1/me", nil, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
}

// The check is a read of current state on every request, not something baked
// into the token, so a reinstated account works again at once rather than when
// its access token happens to expire.
func TestReinstatingLetsThemBackInWithoutWaitingForANewToken(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000803", "Επιστρέφων")

	h.mustExec(`UPDATE users SET status = 'suspended' WHERE phone = $1`, "+306900000803")
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusForbidden {
		t.Fatalf("while suspended: %d", rec.Code)
	}

	h.mustExec(`UPDATE users SET status = 'active' WHERE phone = $1`, "+306900000803")
	if rec := h.do(http.MethodGet, "/v1/me", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("after reinstatement: %d %s", rec.Code, rec.Body.String())
	}
}

// An account that no longer exists is not the same as one that is suspended,
// and saying "suspended" would be telling somebody they had been punished when
// they had been deleted.
func TestAVanishedAccountIsNotCalledSuspended(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000804", "Χαμένος")
	h.mustExec(`DELETE FROM users WHERE phone = $1`, "+306900000804")

	rec := h.do(http.MethodGet, "/v1/me", nil, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}
