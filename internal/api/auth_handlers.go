package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"carpool/internal/auth"
	"carpool/internal/invites"
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

	err := d.OTP.Request(r.Context(), body.Phone)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many codes requested for this number")
	case err != nil:
		// The only other failure mode is a malformed number; treating it as a
		// client error avoids leaking whether the number is known.
		writeError(w, http.StatusBadRequest, "invalid_request", "the phone number must be in E.164 format")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d Deps) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone    string `json:"phone"`
		Code     string `json:"code"`
		Name     string `json:"name"`
		RegionID string `json:"region_id"`
		Invite   string `json:"invite_code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	regionID, err := uuid.Parse(body.RegionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "region_id must be a uuid")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	if err := d.OTP.Verify(r.Context(), body.Phone, body.Code); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_code", "the code is not valid or has expired")
		return
	}

	user, err := d.Invites.Admit(r.Context(), body.Phone, body.Name, regionID,
		strings.ToUpper(strings.TrimSpace(body.Invite)))
	switch {
	case errors.Is(err, invites.ErrCodeRequired):
		writeError(w, http.StatusForbidden, "invite_required", "this pilot is invite-only")
		return
	case errors.Is(err, invites.ErrCodeNotUsable):
		writeError(w, http.StatusForbidden, "invite_invalid", "that invite code cannot be used")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "could not create the account; check region_id")
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
