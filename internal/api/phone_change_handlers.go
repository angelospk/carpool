package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/auth"
)

// Changing the number an account signs in with. See internal/auth/phone_change.go
// for why it is shaped the way it is.

func (d Deps) handleRequestPhoneChange(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		Phone string `json:"phone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	challengeID, code, err := d.PhoneChange.Request(r.Context(), userID, body.Phone)
	switch {
	case errors.Is(err, auth.ErrSameNumber):
		writeError(w, http.StatusBadRequest, "same_number", "that is already your number")
		return
	case errors.Is(err, auth.ErrPhoneTaken):
		writeError(w, http.StatusConflict, "phone_unavailable", "that number cannot be used")
		return
	case errors.Is(err, auth.ErrInvalidOTP):
		writeError(w, http.StatusBadRequest, "invalid_request", "that is not a phone number")
		return
	case errors.Is(err, auth.ErrTooManyChanges):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "the code could not be sent")
		return
	}

	out := map[string]any{"challenge_id": challengeID}
	if d.DevMode {
		// Same rule as sign-in: the code comes back only where the log already
		// prints it, and a test asserts it does not otherwise.
		out["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) handleConfirmPhoneChange(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	// Deliberately no phone field. The number being moved to comes out of the
	// challenge; if it came out of the request, a code sent to one number could
	// be spent against another.
	challengeID, err := uuid.Parse(body.ChallengeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "challenge_id must be a uuid")
		return
	}

	pair, err := d.PhoneChange.Confirm(r.Context(), userID, challengeID, body.Code)
	switch {
	case errors.Is(err, auth.ErrPhoneTaken):
		writeError(w, http.StatusConflict, "phone_unavailable", "that number cannot be used")
		return
	case errors.Is(err, auth.ErrTooManyChanges):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	case errors.Is(err, auth.ErrChallengeFailed):
		writeError(w, http.StatusUnauthorized, "invalid_code", "that code is not valid or has expired")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "the number could not be changed")
		return
	}

	// Every previous session is gone, including the one that made this request,
	// so the new pair is not a convenience: without it the person is signed out
	// by their own change.
	writeJSON(w, http.StatusOK, tokenResponse(pair))
}
