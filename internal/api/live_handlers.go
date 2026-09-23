package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/live"
)

// liveTrip reads the trip id and the caller, answering 404 for anything that
// is not a trip id so the error does not say more than a missing trip would.
func (d Deps) liveTrip(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, _ := callerID(r.Context())
	tripID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return uuid.Nil, uuid.Nil, false
	}
	return tripID, userID, true
}

func (d Deps) liveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, live.ErrNotOnTrip):
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
	case errors.Is(err, live.ErrOutsideWindow):
		writeError(w, http.StatusConflict, "not_sharing_now",
			"live location is only shared around the time of the trip")
	case errors.Is(err, live.ErrBadPosition):
		writeError(w, http.StatusBadRequest, "invalid_position", "that is not a position")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "could not share the location")
	}
}

// handleLive answers when sharing is open for this trip and where the others
// the caller may see are.
func (d Deps) handleLive(w http.ResponseWriter, r *http.Request) {
	tripID, userID, ok := d.liveTrip(w, r)
	if !ok {
		return
	}
	opens, closes, err := d.Live.Window(r.Context(), tripID, userID)
	if err != nil {
		d.liveError(w, err)
		return
	}
	list, err := d.Live.Visible(r.Context(), tripID, userID)
	if err != nil {
		d.liveError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"user_id": p.UserID, "name": p.Name, "lat": p.Lat, "lon": p.Lon, "updated_at": p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"opens": opens, "closes": closes, "positions": out})
}

func (d Deps) handlePutPosition(w http.ResponseWriter, r *http.Request) {
	tripID, userID, ok := d.liveTrip(w, r)
	if !ok {
		return
	}
	var body struct {
		Lat *float64 `json:"lat"`
		Lon *float64 `json:"lon"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Lat == nil || body.Lon == nil {
		d.liveError(w, live.ErrBadPosition)
		return
	}
	if err := d.Live.Put(r.Context(), tripID, userID, *body.Lat, *body.Lon); err != nil {
		d.liveError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleStopPosition(w http.ResponseWriter, r *http.Request) {
	tripID, userID, ok := d.liveTrip(w, r)
	if !ok {
		return
	}
	if err := d.Live.Stop(r.Context(), tripID, userID); err != nil {
		d.liveError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
