package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/store"
)

// askErrorStatus maps a lifecycle error to its HTTP shape. The app shows a
// different message for each, so they must not collapse into one code.
func askErrorStatus(err error) (int, string, string) {
	switch {
	case errors.Is(err, asks.ErrNotYours):
		return http.StatusForbidden, "not_yours", "this request was not sent to you"
	case errors.Is(err, asks.ErrAskExpired):
		return http.StatusConflict, "ask_expired", "this request has expired"
	case errors.Is(err, asks.ErrAskNotPending):
		return http.StatusConflict, "ask_closed", "this request has already been answered"
	case errors.Is(err, asks.ErrNoSeats):
		return http.StatusConflict, "no_seats", "the trip has no free seats left"
	case errors.Is(err, asks.ErrNoLuggageRoom):
		return http.StatusConflict, "no_luggage_room", "the trip has no room left for that much luggage"
	case errors.Is(err, asks.ErrTripDeparted):
		return http.StatusConflict, "trip_departed", "this trip has already set off"
	case errors.Is(err, asks.ErrAlreadyAboard):
		return http.StatusConflict, "already_aboard", "this passenger already has a seat on this trip"
	case errors.Is(err, asks.ErrStaleItinerary):
		return http.StatusConflict, "itinerary_changed", "your route changed; please reopen the request"
	default:
		// Anything unnamed above is a failure nobody planned for; the sentence
		// the driver sees says nothing, so the log has to.
		slog.Error("answering an ask", "error", err)
		return http.StatusInternalServerError, "internal", "something went wrong"
	}
}

func (d Deps) handleAcceptAsk(w http.ResponseWriter, r *http.Request) {
	userID, askID, ok := d.askRouteParams(w, r)
	if !ok {
		return
	}

	bookingID, err := d.Asks.Accept(r.Context(), askID, userID)
	if err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"booking_id": bookingID})
}

func (d Deps) handleDeclineAsk(w http.ResponseWriter, r *http.Request) {
	userID, askID, ok := d.askRouteParams(w, r)
	if !ok {
		return
	}

	if err := d.Asks.Decline(r.Context(), askID, userID); err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// askRouteParams pulls the caller and the ask id off a request.
func (d Deps) askRouteParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return uuid.Nil, uuid.Nil, false
	}

	askID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the ask id must be a uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, askID, true
}

// handlePendingAsks lists the asks waiting on the caller as a driver.
func (d Deps) handlePendingAsks(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	list, err := store.PendingAsks(r.Context(), d.Pool, userID, d.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list asks")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, askJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"asks": out})
}

// handleGetAsk is what a notification tap opens.
func (d Deps) handleGetAsk(w http.ResponseWriter, r *http.Request) {
	userID, askID, ok := d.askRouteParams(w, r)
	if !ok {
		return
	}
	a, err := store.AskFor(r.Context(), d.Pool, askID, userID, d.now())
	if errors.Is(err, store.ErrNotParticipant) {
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the request")
		return
	}
	writeJSON(w, http.StatusOK, askJSON(a))
}

// askJSON is built field by field, so a new field on the store type does not
// reach a driver's phone because somebody exported it.
func askJSON(a store.PendingAsk) map[string]any {
	return map[string]any{
		"id":                a.ID,
		"occurrence_id":     a.OccurrenceID,
		"passenger_name":    a.PassengerName,
		"origin_label":      a.OriginLabel,
		"destination_label": a.DestinationLabel,
		"detour_min":        a.DetourMin,
		"detour_km":         a.DetourKm,
		"price_eur":         a.PriceEUR,
		"seats":             a.Seats,
		"depart_earliest":   a.DepartEarliest,
		"expires_at":        a.ExpiresAt,
	}
}
