package api

import (
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/store"
)

// handleWaiting lists the open weekend requests a departure could carry, for
// the driver of it. No phone and no exact point: who, how many, which hours and
// the labels they typed.
func (d Deps) handleWaiting(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	occurrenceID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	list, err := d.Asks.Waiting(r.Context(), occurrenceID, userID)
	if err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"request_id":        p.RequestID,
			"passenger_name":    p.PassengerName,
			"seats":             p.Seats,
			"luggage":           p.Luggage,
			"luggage_count":     p.LuggageCount,
			"window_start":      p.WindowStart,
			"window_end":        p.WindowEnd,
			"origin_label":      p.OriginLabel,
			"destination_label": p.DestinationLabel,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"waiting": out})
}

// handleOffer is a driver saying yes to one waiting passenger.
func (d Deps) handleOffer(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	occurrenceID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	requestID, err := uuid.Parse(body.RequestID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request_id must be a uuid")
		return
	}

	askID, err := d.Asks.Offer(r.Context(), occurrenceID, userID, requestID)
	if err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ask_id": askID})
}

// handleListOffers lists the seats drivers have offered the caller.
func (d Deps) handleListOffers(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	list, err := store.OffersTo(r.Context(), d.Pool, userID, d.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list offers")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, o := range list {
		out = append(out, offerJSON(o))
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": out})
}

// offerJSON is built field by field, like askJSON. The plate is not here: it is
// shown once the seat is accepted.
func offerJSON(o store.Offer) map[string]any {
	return map[string]any{
		"id":                  o.ID,
		"request_id":          o.RequestID,
		"occurrence_id":       o.OccurrenceID,
		"driver_name":         o.DriverName,
		"driver_photo_url":    avatarURL(o.DriverID, o.PhotoVersion),
		"driver_rating":       o.Rating,
		"driver_rating_count": o.RatingCount,
		"car":                 map[string]any{"make": o.CarMake, "model": o.CarModel, "colour": o.CarColour},
		"depart_earliest":     o.DepartEarliest,
		"price_eur":           o.PriceEUR,
		"seats":               o.Seats,
		"detour_min":          o.DetourMin,
		"detour_km":           o.DetourKm,
		"origin_label":        o.OriginLabel,
		"destination_label":   o.DestinationLabel,
		"expires_at":          o.ExpiresAt,
	}
}

func (d Deps) handleAcceptOffer(w http.ResponseWriter, r *http.Request) {
	userID, askID, ok := d.askRouteParams(w, r)
	if !ok {
		return
	}
	bookingID, err := d.Asks.AcceptOffer(r.Context(), askID, userID)
	if err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"booking_id": bookingID})
}

func (d Deps) handleDeclineOffer(w http.ResponseWriter, r *http.Request) {
	userID, askID, ok := d.askRouteParams(w, r)
	if !ok {
		return
	}
	if err := d.Asks.DeclineOffer(r.Context(), askID, userID); err != nil {
		status, code, message := askErrorStatus(err)
		writeError(w, status, code, message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
