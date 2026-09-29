package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/auth"
	"carpool/internal/domain"
	"carpool/internal/invites"
	"carpool/internal/places"
	"carpool/internal/store"
)

// maxBodyBytes bounds request bodies; nothing this API accepts is large.
const maxBodyBytes = 64 << 10

// decodeJSON reads a JSON body, rejecting unknown fields so a typo in a client
// payload surfaces as an error instead of being silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request body is not valid JSON")
		return false
	}
	// Exactly one JSON value is expected.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request body must contain a single JSON object")
		return false
	}
	return true
}

func (d Deps) handleOTPRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	code, err := d.OTP.Request(r.Context(), body.Phone)
	if err == nil {
		// Asking for a fresh code means starting again — a new handset, or a
		// message that never arrived. Whatever half-finished signup the previous
		// code produced does not get to complete later.
		//
		// After the request rather than before it: a malformed number or a
		// rate-limited retry sends no code, and destroying a ticket somebody is
		// part way through using because they mistyped their number once is a
		// worse outcome than the one this prevents.
		if err := d.Signup.Void(r.Context(), body.Phone); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not start signing in")
			return
		}
	}
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many codes requested for this number")
	case err != nil:
		// The only other failure mode is a malformed number; treating it as a
		// client error avoids leaking whether the number is known.
		writeError(w, http.StatusBadRequest, "invalid_request", "the phone number must be in E.164 format")
	case d.DevMode:
		// Testing this app means signing in on a device you are not holding,
		// and DEV_MODE writes the code to the API's log, which whoever is
		// testing cannot read. So it comes back in the response instead.
		//
		// It is the real code: real generator, real expiry, real rate limit. A
		// fixed 0000 would be easier and would test a path that production
		// never runs, which is the opposite of what a test build is for.
		//
		// Anywhere but DEV_MODE this would mean that knowing a phone number is
		// enough to take the account. The response stays empty there, and a
		// test asserts it.
		writeJSON(w, http.StatusOK, map[string]any{"dev_code": code})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleOTPVerify checks the code and then answers the only question that is
// left: is this number an account already?
//
// It deliberately does not ask for a name or a region. It used to, on every
// sign-in, which meant somebody who joined in March was walked through
// onboarding again in April — and since the client had to send something, the
// obvious implementation quietly overwrote their profile with whatever they
// retyped.
//
// The order matters. The lookup happens after the code is verified, never
// before, so the endpoint cannot be used to ask whether a stranger's number is
// registered.
func (d Deps) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if err := d.OTP.Verify(r.Context(), body.Phone, body.Code); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_code", "the code is not valid or has expired")
		return
	}

	user, err := store.UserByPhone(r.Context(), d.Pool, body.Phone)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// Nobody yet. The code has been spent proving the number, so what goes
		// back is a ticket to finish joining with.
		token, expires, err := d.Signup.Issue(r.Context(), body.Phone)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not start signing up")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"signup_token": token,
			"expires_at":   expires,
		})
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not look up the account")
		return
	}

	// Checked here as well as in the middleware, because this is the one door
	// that issues a session rather than presenting one. Told after the code is
	// verified, not before: somebody holding the number is entitled to know why
	// they cannot get in, and a stranger guessing at it never gets this far.
	if user.Status != "active" {
		writeError(w, http.StatusForbidden, "account_suspended", "this account cannot be used")
		return
	}

	pair, err := d.Sessions.Create(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start a session")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse(pair))
}

// handleSignupComplete turns a verified number into an account.
//
// The home region is read off the town rather than chosen from a list of
// seventy-five prefectures. Somebody knows which village they live in; far
// fewer would pick "Περιφερειακή Ενότητα Δυτικού Τομέα Αθηνών" from a menu, and
// the answer is on the map anyway. The place id is the app's own identifier for
// a settlement, so there is no coordinate for a client to invent.
func (d Deps) handleSignupComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SignupToken string `json:"signup_token"`
		Name        string `json:"name"`
		HomePlaceID string `json:"home_place_id"`
		Invite      string `json:"invite_code"`

		// DateOfBirth is YYYY-MM-DD, and is the one thing here with no
		// tolerable default. Asked once, checked once: age only goes one way, so
		// somebody who was eighteen the day they signed up is eighteen for ever
		// after and there is no later moment at which the answer changes.
		DateOfBirth string `json:"date_of_birth"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	phone, err := d.Signup.Peek(r.Context(), body.SignupToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token",
			"that signup has expired; ask for a new code")
		return
	}

	// The same rule the profile edit applies. Trimming alone let in, at signup,
	// everything that edit refuses: line breaks, direction overrides, a name
	// three hundred letters long.
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	name, err := domain.NormaliseName(body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", "that name cannot be used")
		return
	}

	// Today in Athens, because a birthday is a calendar day and half past eleven
	// at night here is already tomorrow in UTC.
	today := domain.DateIn(d.Clock.Now(), d.Location)

	born, err := domain.ParseBirthDate(body.DateOfBirth, today)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_birth_date",
			"your date of birth is needed, as YYYY-MM-DD")
		return
	}
	// A separate answer from a separate question, and a separate status: the
	// first was "is this a date", this is "does it let you in". 403 rather than
	// 400 because nothing about the request was malformed.
	if !domain.IsAdultOn(born, today) {
		writeError(w, http.StatusForbidden, "underage",
			"this app is for adults; you have to be 18 to travel with strangers")
		return
	}

	place, ok := places.ByID(body.HomePlaceID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown_place", "that is not a settlement we know")
		return
	}

	region, err := store.RegionAt(r.Context(), d.Pool, place.Lon, place.Lat)
	if errors.Is(err, store.ErrOutsideGreece) {
		writeError(w, http.StatusBadRequest, "unknown_place", "that settlement is outside the country")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not locate that town")
		return
	}

	user, err := d.Invites.AdmitWith(r.Context(), phone, name, region.ID, born,
		strings.ToUpper(strings.TrimSpace(body.Invite)),
		func(ctx context.Context, tx pgx.Tx) error {
			return d.Signup.SpendTx(ctx, tx, body.SignupToken)
		})
	switch {
	case errors.Is(err, invites.ErrCodeRequired):
		writeError(w, http.StatusForbidden, "invite_required", "this pilot is invite-only")
		return
	case errors.Is(err, invites.ErrCodeNotUsable):
		writeError(w, http.StatusForbidden, "invite_invalid", "that invite code cannot be used")
		return
	case errors.Is(err, auth.ErrNoTicket):
		// Lost a race with another exchange of the same ticket.
		writeError(w, http.StatusUnauthorized, "invalid_token", "that signup has already been used")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not create the account")
		return
	}

	pair, err := d.Sessions.Create(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start a session")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse(pair))
}

func (d Deps) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
		// AttemptID lets a client that lost the response retry safely; see
		// SessionService.Refresh. Optional: an absent id means the strict
		// no-retry rule applies.
		AttemptID string `json:"attempt_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	var attemptID uuid.UUID
	if body.AttemptID != "" {
		parsed, err := uuid.Parse(body.AttemptID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "attempt_id must be a uuid")
			return
		}
		attemptID = parsed
	}

	pair, err := d.Sessions.Refresh(r.Context(), body.RefreshToken, attemptID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "the refresh token is not valid")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse(pair))
}

func (d Deps) handleLogout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if err := d.Sessions.Revoke(r.Context(), body.RefreshToken); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not end the session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func tokenResponse(p auth.TokenPair) map[string]any {
	return map[string]any{
		"access_token":  p.AccessToken,
		"refresh_token": p.RefreshToken,
		"expires_at":    p.ExpiresAt,
	}
}
