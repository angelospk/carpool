package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Φέρες. A real settlement out of the embedded national list, inside the
// rectangle the harness seeds as a region, so signing up with it can be checked
// against a region the test knows the id of.
const feres = "node/139260653"

// adultDOB is an ordinary grown-up, for the tests that are about something
// else. Fixed rather than derived from the clock: a date computed from "now"
// would drift with the harness and would make the age gate impossible to test
// from the other side.
const adultDOB = "1990-01-01"

// signupToken runs the first half of joining: a code is requested, the code is
// verified, and what comes back for a number nobody has used is a ticket to
// finish signing up rather than a session.
func (h *harness) signupToken(phone string) string {
	h.t.Helper()

	if rec := h.do(http.MethodPost, "/v1/auth/otp/request",
		map[string]any{"phone": phone}, ""); rec.Code >= 300 {
		h.t.Fatalf("otp/request: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodPost, "/v1/auth/otp/verify",
		map[string]any{"phone": phone, "code": h.sms.LastCode()}, "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("otp/verify: %d %s", rec.Code, rec.Body.String())
	}

	var body struct {
		SignupToken string `json:"signup_token"`
	}
	h.decode(rec, &body)
	if body.SignupToken == "" {
		h.t.Fatalf("no signup_token in %s", rec.Body.String())
	}
	return body.SignupToken
}

// The bug this is all for.
//
// Verifying demanded a name and a region on every sign-in, so somebody who had
// been using the app for a month was walked through onboarding again each time
// they logged in — and the client had to send *something*, so the obvious
// implementation would have been to send the name they typed and quietly
// overwrite the one they had.
func TestOTPVerifySignsInAKnownAccountWithoutAskingWhoTheyAre(t *testing.T) {
	h := newHarness(t)
	h.signUp(phone, "Χαράλαμπος")

	if rec := h.do(http.MethodPost, "/v1/auth/otp/request",
		map[string]any{"phone": phone}, ""); rec.Code >= 300 {
		t.Fatalf("otp/request: %d", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/auth/otp/verify",
		map[string]any{"phone": phone, "code": h.sms.LastCode()}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken string `json:"access_token"`
		SignupToken string `json:"signup_token"`
	}
	h.decode(rec, &body)
	if body.AccessToken == "" {
		t.Errorf("no access token for an account that already exists: %s", rec.Body.String())
	}
	if body.SignupToken != "" {
		t.Errorf("a known number was sent round the signup path again")
	}
}

// A number nobody has used cannot be handed a session, because there is no
// account to hand it for. It gets a ticket instead.
//
// The ticket is why this is two calls rather than one. The client cannot know
// whether the number is known until the code has been checked, and checking the
// code spends it — so without something to carry forward, a new user reaches
// the profile screen holding a credential that has already been used.
func TestOTPVerifyHandsANewNumberATicketRatherThanASession(t *testing.T) {
	h := newHarness(t)

	if rec := h.do(http.MethodPost, "/v1/auth/otp/request",
		map[string]any{"phone": phone}, ""); rec.Code >= 300 {
		t.Fatalf("otp/request: %d", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/auth/otp/verify",
		map[string]any{"phone": phone, "code": h.sms.LastCode()}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken string `json:"access_token"`
		SignupToken string `json:"signup_token"`
	}
	h.decode(rec, &body)
	if body.SignupToken == "" {
		t.Errorf("no signup token for a new number: %s", rec.Body.String())
	}
	if body.AccessToken != "" {
		t.Errorf("a session was issued for an account that does not exist yet")
	}

	var count int
	h.mustQuery(`SELECT count(*) FROM users WHERE phone = $1`, &count, phone)
	if count != 0 {
		t.Errorf("verifying a code created %d accounts; it must only prove the number", count)
	}
}

// Nobody is asked which prefecture they live in. They say which town, and the
// town is on the map.
func TestSignupCompleteCreatesTheAccountAndReadsTheRegionOffTheTown(t *testing.T) {
	h := newHarness(t)
	token := h.signupToken(phone)

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token":  token,
		"name":          "Χαράλαμπος",
		"home_place_id": feres,
		"date_of_birth": adultDOB,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	h.decode(rec, &body)
	if body.AccessToken == "" {
		t.Fatalf("no access token: %s", rec.Body.String())
	}

	var name string
	var region string
	h.mustQuery(`SELECT name, home_region_id::text FROM users WHERE phone = $1`, &name, &region, phone)
	if name != "Χαράλαμπος" {
		t.Errorf("name = %q", name)
	}
	if region != h.regionID.String() {
		t.Errorf("home region = %s, want the region Φέρες is in (%s)", region, h.regionID)
	}
}

// A ticket is a credential. Two accounts must not come out of one proof that
// somebody holds one phone.
func TestASignupTicketIsSpentTheFirstTimeItIsUsed(t *testing.T) {
	h := newHarness(t)
	token := h.signupToken(phone)

	body := map[string]any{
		"signup_token": token, "name": "Χαράλαμπος", "home_place_id": feres,
		"date_of_birth": adultDOB,
	}
	if rec := h.do(http.MethodPost, "/v1/auth/signup/complete", body, ""); rec.Code != http.StatusOK {
		t.Fatalf("first exchange: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", body, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("second exchange: status = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "invalid_token" {
		t.Errorf("error code = %q, want invalid_token", code)
	}
}

func TestASignupTicketExpires(t *testing.T) {
	h := newHarness(t)
	token := h.signupToken(phone)

	h.clock.Advance(11 * time.Minute)

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token": token, "name": "Χαράλαμπος", "home_place_id": feres,
		"date_of_birth": adultDOB,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

// Somebody who asks for a new code has started again — on a new handset, or
// because the first message never arrived. Whatever the half-finished signup on
// the old device was going to do, it does not get to do it later.
func TestAskingForANewCodeVoidsAnOutstandingSignupTicket(t *testing.T) {
	h := newHarness(t)
	token := h.signupToken(phone)

	if rec := h.do(http.MethodPost, "/v1/auth/otp/request",
		map[string]any{"phone": phone}, ""); rec.Code >= 300 {
		t.Fatalf("second otp/request: %d", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token": token, "name": "Χαράλαμπος", "home_place_id": feres,
		"date_of_birth": adultDOB,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

// The place id is the app's own canonical identifier, not a coordinate the
// client picked, so there is nothing to negotiate: it either names a settlement
// or it does not.
func TestSignupCompleteRefusesATownItDoesNotKnow(t *testing.T) {
	h := newHarness(t)

	// A phone each: three code requests per number in a quarter of an hour is
	// the rate limit, and each case needs one.
	for i, place := range []string{"node/999999999999", "curated/whatever", "", "Φέρες"} {
		t.Run(place, func(t *testing.T) {
			phone := fmt.Sprintf("+30690000010%d", i)
			token := h.signupToken(phone)

			rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
				"signup_token": token, "name": "Χαράλαμπος", "home_place_id": place,
				"date_of_birth": adultDOB,
			}, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
			if code := h.errorCode(rec); code != "unknown_place" {
				t.Errorf("error code = %q, want unknown_place", code)
			}

			// A refused attempt must not burn the ticket: the person retypes
			// their town, they do not start the whole sign-in again.
			rec = h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
				"signup_token": token, "name": "Χαράλαμπος", "home_place_id": feres,
				"date_of_birth": adultDOB,
			}, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("retry after a bad town: %d %s", rec.Code, rec.Body.String())
			}
			h.mustExec(`DELETE FROM users WHERE phone = $1`, phone)
		})
	}
}

func TestSignupCompleteRefusesAnEmptyName(t *testing.T) {
	h := newHarness(t)
	token := h.signupToken(phone)

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token": token, "name": "  ", "home_place_id": feres,
		"date_of_birth": adultDOB,
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "invalid_request" {
		t.Errorf("error code = %q, want invalid_request", code)
	}
}

// Nobody under eighteen gets an account.
//
// Checked once, at the door, rather than at every departure. Age only goes one
// way: if every account holder was eighteen on the day they signed up, every
// account holder is eighteen for ever after, and there is no later moment at
// which the answer can have changed. Checking at the door is the whole rule.
func TestSignupRefusesAnybodyUnderEighteen(t *testing.T) {
	h := newHarness(t)

	// Measured against the harness clock rather than written in, so this does
	// not start failing on its own eighteenth birthday.
	today := h.clock.Now().In(time.UTC)
	dayShort := today.AddDate(-18, 0, 1).Format("2006-01-02")
	exactly := today.AddDate(-18, 0, 0).Format("2006-01-02")

	rec := h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token":  h.signupToken("+306944000001"),
		"name":          "Νεαρός",
		"home_place_id": feres,
		"date_of_birth": dayShort,
	}, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("a day short of eighteen gave %d, want 403: %s", rec.Code, rec.Body.String())
	} else if code := h.errorCode(rec); code != "underage" {
		t.Errorf("error code %q, want underage", code)
	}

	// And the refusal left no account behind.
	var n int
	h.mustQuery(`SELECT count(*) FROM users WHERE phone = $1`, &n, "+306944000001")
	if n != 0 {
		t.Errorf("the refused signup created %d accounts", n)
	}

	// Eighteen on the day is eighteen.
	rec = h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
		"signup_token":  h.signupToken("+306944000002"),
		"name":          "Ενήλικη",
		"home_place_id": feres,
		"date_of_birth": exactly,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("exactly eighteen gave %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// And what was stored is the date that was sent. Checking only the verdict
	// would pass just as well against a column filled in with a constant.
	var stored string
	h.mustQuery(`SELECT to_char(date_of_birth, 'YYYY-MM-DD') FROM users WHERE phone = $1`,
		&stored, "+306944000002")
	if stored != exactly {
		t.Errorf("stored date of birth = %q, want %q", stored, exactly)
	}
}

// A missing or nonsensical birth date is refused rather than defaulted. There
// is no sensible default for this, and a signup that quietly stores one is an
// age gate that does nothing.
func TestSignupNeedsARealBirthDate(t *testing.T) {
	h := newHarness(t)

	// A fresh number each time: three codes per number per fifteen minutes is
	// the rate limit, and reusing one would fail this on the limiter rather
	// than on the birth date.
	for i, dob := range []any{nil, "", "15/03/2008", "2008-02-30", "2099-01-01"} {
		body := map[string]any{
			"signup_token":  h.signupToken(fmt.Sprintf("+30694401%04d", i)),
			"name":          "Χωρίς ημερομηνία",
			"home_place_id": feres,
		}
		if dob != nil {
			body["date_of_birth"] = dob
		}

		rec := h.do(http.MethodPost, "/v1/auth/signup/complete", body, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a birth date of %v gave %d, want 400: %s", dob, rec.Code, rec.Body.String())
			continue
		}
		if code := h.errorCode(rec); code != "invalid_birth_date" {
			t.Errorf("a birth date of %v gave error %q, want invalid_birth_date", dob, code)
		}
	}
}

// Somebody's date of birth is not the app's to hand out. It is asked for once,
// to answer one question, and then it stays put — including from the account's
// own profile, which is the response that gets rendered in front of other
// people.
func TestABirthDateNeverLeavesInAProfile(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306944000004", "Επιβάτης")

	rec := h.do(http.MethodGet, "/v1/me", nil, token)
	if body := rec.Body.String(); strings.Contains(body, "date_of_birth") {
		t.Errorf("/v1/me carries a birth date: %s", body)
	}
}
