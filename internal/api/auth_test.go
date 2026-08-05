package api_test

import (
	"github.com/google/uuid"
	"net/http"
	"testing"
	"time"

	"carpool/internal/auth"
)

const phone = "+306900000001"

func TestOTPRequestSendsACode(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	if len(h.sms.Sent()) != 1 {
		t.Fatalf("got %d SMS, want 1", len(h.sms.Sent()))
	}
}

func TestOTPRequestRejectsAMalformedPhone(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": "06900000001"}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if code := h.errorCode(rec); code != "invalid_request" {
		t.Errorf("error code = %q, want invalid_request", code)
	}
}

func TestOTPRequestIsRateLimited(t *testing.T) {
	h := newHarness(t)

	for range auth.OTPMaxPerWindow {
		if rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}

	rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, "")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestOTPVerifyCreatesTheAccountAndReturnsTokens(t *testing.T) {
	h := newHarness(t)

	access, refresh := h.signUp(phone, "Χαράλαμπος")

	if access == "" || refresh == "" {
		t.Fatal("verify did not return both tokens")
	}

	var name string
	if err := h.pool.QueryRow(t.Context(),
		`SELECT name FROM users WHERE phone = $1`, phone).Scan(&name); err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	if name != "Χαράλαμπος" {
		t.Errorf("stored name = %q, want Χαράλαμπος", name)
	}
}

func TestOTPVerifyRejectsAWrongCode(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("otp/request: %d", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/auth/otp/verify", map[string]any{
		"phone": phone, "code": "000000",
	}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	// No account may be created by a failed verification.
	var count int
	if err := h.pool.QueryRow(t.Context(), `SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("counting users: %v", err)
	}
	if count != 0 {
		t.Fatalf("user count = %d, want 0 after a failed verification", count)
	}
}

func TestOTPVerifyLogsInAnExistingAccountWithoutDuplicatingIt(t *testing.T) {
	h := newHarness(t)
	h.signUp(phone, "First Name")

	// A second sign-in with the same number must not create a second account,
	// nor rename the existing one.
	h.signUp(phone, "Someone Else")

	var count int
	var name string
	if err := h.pool.QueryRow(t.Context(),
		`SELECT count(*), max(name) FROM users WHERE phone = $1`, phone).Scan(&count, &name); err != nil {
		t.Fatalf("querying users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
	if name != "First Name" {
		t.Errorf("name = %q; signing in again must not rename the account", name)
	}
}

func TestRefreshReturnsANewTokenPair(t *testing.T) {
	h := newHarness(t)
	_, refresh := h.signUp(phone, "Test")

	rec := h.do(http.MethodPost, "/v1/auth/refresh", map[string]any{"refresh_token": refresh}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	h.decode(rec, &body)
	if body.RefreshToken == refresh {
		t.Fatal("refresh must rotate the token")
	}
	if body.AccessToken == "" {
		t.Fatal("no access token returned")
	}
}

// The client retries a refresh whose response it never saw, presenting the same
// attempt id. Over HTTP that must come back with the same token pair rather than
// a 401, which is what the old strict rule returned and what logged users out.
func TestRetryingARefreshOverHTTPReturnsTheSamePair(t *testing.T) {
	h := newHarness(t)
	_, refresh := h.signUp(phone, "Test")
	attempt := uuid.New().String()

	body := map[string]any{"refresh_token": refresh, "attempt_id": attempt}
	first := h.do(http.MethodPost, "/v1/auth/refresh", body, "")
	second := h.do(http.MethodPost, "/v1/auth/refresh", body, "")

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d then %d, want 200 twice; %s", first.Code, second.Code, second.Body.String())
	}
	var a, b struct {
		RefreshToken string `json:"refresh_token"`
	}
	h.decode(first, &a)
	h.decode(second, &b)
	if a.RefreshToken != b.RefreshToken {
		t.Fatal("the retry returned a different refresh token")
	}
}

func TestRefreshRejectsAMalformedAttemptID(t *testing.T) {
	h := newHarness(t)
	_, refresh := h.signUp(phone, "Test")

	rec := h.do(http.MethodPost, "/v1/auth/refresh",
		map[string]any{"refresh_token": refresh, "attempt_id": "not-a-uuid"}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRefreshRejectsAnUnknownToken(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodPost, "/v1/auth/refresh", map[string]any{"refresh_token": "nope"}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMeRequiresAuthentication(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodGet, "/v1/me", nil, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := h.errorCode(rec); code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", code)
	}
}

func TestMeRejectsAGarbageToken(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodGet, "/v1/me", nil, "not-a-jwt")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMeReturnsTheAuthenticatedProfile(t *testing.T) {
	h := newHarness(t)
	access, _ := h.signUp(phone, "Χαράλαμπος")

	rec := h.do(http.MethodGet, "/v1/me", nil, access)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Phone    string `json:"phone"`
		Name     string `json:"name"`
		RegionID string `json:"home_region_id"`
	}
	h.decode(rec, &body)
	if body.Phone != phone {
		t.Errorf("phone = %q, want %q", body.Phone, phone)
	}
	if body.Name != "Χαράλαμπος" {
		t.Errorf("name = %q, want Χαράλαμπος", body.Name)
	}
	if body.RegionID != h.regionID.String() {
		t.Errorf("home_region_id = %q, want %q", body.RegionID, h.regionID)
	}
}

func TestMeRejectsAnExpiredAccessToken(t *testing.T) {
	h := newHarness(t)
	access, _ := h.signUp(phone, "Test")

	h.clock.Advance(auth.AccessTokenTTL + time.Minute)

	rec := h.do(http.MethodGet, "/v1/me", nil, access)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired token", rec.Code)
	}
}

func TestPatchMeUpdatesTheProfile(t *testing.T) {
	h := newHarness(t)
	access, _ := h.signUp(phone, "Old Name")

	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{"name": "New Name"}, access)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var name string
	if err := h.pool.QueryRow(t.Context(),
		`SELECT name FROM users WHERE phone = $1`, phone).Scan(&name); err != nil {
		t.Fatalf("reading user: %v", err)
	}
	if name != "New Name" {
		t.Fatalf("name = %q, want New Name", name)
	}
}

func TestRegionsAreListedPublicly(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodGet, "/v1/regions", nil, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Regions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"regions"`
	}
	h.decode(rec, &body)
	if len(body.Regions) != 1 {
		t.Fatalf("got %d regions, want 1", len(body.Regions))
	}
	if body.Regions[0].Name != "Έβρος" {
		t.Errorf("region name = %q, want Έβρος", body.Regions[0].Name)
	}
}
