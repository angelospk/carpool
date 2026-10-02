package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"carpool/internal/drives"
)

// handleStartDrive is the driver saying the minute they are leaving. See
// internal/drives for what it changes and specs/DRIVING-NOW.md for why.
func (d Deps) handleStartDrive(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	occurrenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	var body struct {
		DepartsAt *time.Time `json:"departs_at"`
		DetourMin *int       `json:"detour_min"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.DepartsAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "departs_at is required")
		return
	}

	departs, err := d.Drives.Start(r.Context(), userID, occurrenceID,
		drives.Start{DepartsAt: *body.DepartsAt, DetourMin: body.DetourMin})
	switch {
	case errors.Is(err, drives.ErrNotYours):
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
	case errors.Is(err, drives.ErrNotScheduled):
		writeError(w, http.StatusConflict, "trip_not_running", "this trip has been called off")
	case errors.Is(err, drives.ErrDeparted):
		writeError(w, http.StatusConflict, "drive_departed", "this drive has already left")
	case errors.Is(err, drives.ErrBadTime):
		writeError(w, http.StatusUnprocessableEntity, "bad_departure", "leave within the next 15 minutes")
	case errors.Is(err, drives.ErrOutsideWindow):
		writeError(w, http.StatusUnprocessableEntity, "outside_window", "too far from the published time")
	case errors.Is(err, drives.ErrBadDetour):
		writeError(w, http.StatusUnprocessableEntity, "bad_detour", "detour must be 0 to 30 minutes")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not set the departure")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"departs_at": departs})
	}
}

// handleRouteNudge turns on or off being told, as a departure on this route
// comes up, that somebody on the road is waiting.
func (d Deps) handleRouteNudge(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	var body struct {
		On *bool `json:"on"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.On == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "on is required")
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		UPDATE route_templates SET nudge_when_leaving = $3, updated_at = now()
		 WHERE id = $1 AND driver_id = $2 AND active`, routeID, userID, *body.On)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not change the route")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
