package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"carpool/internal/domain"
	"carpool/internal/store"
)

// Proposing a meeting point, and deciding about one.
//
// The automatic list is thin outside the towns and wrong inside the cities, and
// the people who know where a car can actually wait are the ones waiting there.

const maxPendingListed = 50

// maxReasonBytes bounds the note a moderator leaves. Generous for a sentence,
// far below anything worth reading into a request.
const maxReasonBytes = 4 << 10

// handleProposeMeetingPoint records somebody's suggestion, pending review.
func (d Deps) handleProposeMeetingPoint(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		PlaceID string  `json:"place_id"`
		Name    string  `json:"name"`
		Kind    string  `json:"kind"`
		Lon     float64 `json:"lon"`
		Lat     float64 `json:"lat"`
		// A retry after a timeout on a phone must not become a second proposal.
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	got, err := store.ProposeMeetingPoint(r.Context(), d.Pool, store.MeetingPointProposal{
		PlaceID:        body.PlaceID,
		Name:           body.Name,
		Kind:           body.Kind,
		Lon:            body.Lon,
		Lat:            body.Lat,
		ProposedBy:     userID,
		IdempotencyKey: body.IdempotencyKey,
	})
	switch {
	case errors.Is(err, store.ErrUnknownPlace):
		writeError(w, http.StatusBadRequest, "unknown_place", "that place is not in the list")
	case errors.Is(err, store.ErrBadKind):
		writeError(w, http.StatusBadRequest, "invalid_kind", "that is not a kind of meeting point")
	case errors.Is(err, store.ErrTooFarFromPlace):
		writeError(w, http.StatusBadRequest, "too_far", "that point is not in that place")
	case errors.Is(err, store.ErrDuplicatePoint):
		writeError(w, http.StatusConflict, "duplicate_point", "there is already a point there")
	case errors.Is(err, store.ErrTooManyProposals):
		writeError(w, http.StatusTooManyRequests, "too_many", "that is enough suggestions for today")
	case errors.Is(err, domain.ErrBadName):
		writeError(w, http.StatusBadRequest, "invalid_name", "that name cannot be used")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not record that suggestion")
	default:
		writeJSON(w, http.StatusCreated, got)
	}
}

// handleMyMeetingPointProposals is somebody's own suggestions and what became
// of them.
//
// Separate from the public list on purpose. Mixing a pending proposal into the
// answer everybody gets would make it cacheable by accident and selectable by
// its author, and a point nobody has looked at yet must not be attached to a
// ride.
func (d Deps) handleMyMeetingPointProposals(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	got, err := store.MeetingPointRequestsBy(r.Context(), d.Pool, userID, maxPendingListed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your suggestions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": got})
}

// requireModerator refuses anybody the database does not currently call one.
//
// Read on every request rather than carried in the token: revoking has to take
// effect now, not when an access token expires fifteen minutes later.
func (d Deps) requireModerator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := callerID(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
			return
		}

		var is bool
		if err := d.Pool.QueryRow(r.Context(),
			`SELECT is_moderator FROM users WHERE id = $1 AND status = 'active'`, userID).Scan(&is); err != nil || !is {
			// The same answer either way: somebody who is not a moderator does
			// not learn that the queue exists.
			writeError(w, http.StatusForbidden, "forbidden", "that is not yours to do")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (d Deps) handlePendingMeetingPoints(w http.ResponseWriter, r *http.Request) {
	got, err := store.PendingMeetingPointRequests(r.Context(), d.Pool, maxPendingListed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read the queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": got})
}

func (d Deps) handleDecideMeetingPoint(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewer, ok := callerID(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "that is not a request id")
			return
		}

		var body struct {
			Reason string `json:"reason"`
		}
		// A reason is optional on approval and worth having on a refusal, so a
		// missing or unreadable body is not an error: the decision is in the
		// route, not the payload.
		_ = json.NewDecoder(io.LimitReader(r.Body, maxReasonBytes)).Decode(&body)

		err = store.DecideMeetingPointRequest(r.Context(), d.Pool, id, reviewer, approve, body.Reason)
		switch {
		case errors.Is(err, store.ErrAlreadyDecided):
			writeError(w, http.StatusConflict, "already_decided", "somebody has already answered that one")
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal", "could not record that decision")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleModeratorEdit is the other half of moderation, and the half the
// automatic list actually needs: hiding a point that is a ticket agency,
// renaming one whose OSM label is unreadable, or putting the ΚΤΕΛ above the
// three bad entries that came out of the harvest.
func (d Deps) handleModeratorEdit(w http.ResponseWriter, r *http.Request) {
	moderator, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		PlaceID  string  `json:"place_id"`
		Action   string  `json:"action"`
		TargetID string  `json:"target_id"`
		Name     string  `json:"name"`
		Kind     string  `json:"kind"`
		Lon      float64 `json:"lon"`
		Lat      float64 `json:"lat"`
		Priority int     `json:"priority"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	err := store.ApplyModeratorEdit(r.Context(), d.Pool, store.ModeratorEdit{
		PlaceID: body.PlaceID, Action: body.Action, TargetID: body.TargetID,
		Name: body.Name, Kind: body.Kind, Lon: body.Lon, Lat: body.Lat,
		Priority: body.Priority, By: moderator,
	})
	switch {
	case errors.Is(err, store.ErrUnknownPlace):
		writeError(w, http.StatusBadRequest, "unknown_place", "that place is not in the list")
	case errors.Is(err, store.ErrBadKind):
		writeError(w, http.StatusBadRequest, "invalid_kind", "that is not a kind of meeting point")
	case errors.Is(err, store.ErrTooFarFromPlace):
		writeError(w, http.StatusBadRequest, "too_far", "that point is not in that place")
	case errors.Is(err, domain.ErrBadName):
		writeError(w, http.StatusBadRequest, "invalid_name", "that name cannot be used")
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
