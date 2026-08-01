package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"carpool/internal/store"
)

// tripParams resolves the caller and the trip, and checks participation. Every
// trip-scoped route starts here.
//
// A non-participant gets 404 rather than 403, so trip ids cannot be probed for
// existence by someone who is not on them.
func (d Deps) tripParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, store.TripRole, bool) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return uuid.Nil, uuid.Nil, "", false
	}

	occurrenceID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the trip id must be a uuid")
		return uuid.Nil, uuid.Nil, "", false
	}

	role, err := store.ParticipantRole(r.Context(), d.Pool, occurrenceID, userID)
	if errors.Is(err, store.ErrNotParticipant) {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return uuid.Nil, uuid.Nil, "", false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the trip")
		return uuid.Nil, uuid.Nil, "", false
	}
	return userID, occurrenceID, role, true
}

func (d Deps) handleUpcomingTrips(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	trips, err := store.UpcomingTrips(r.Context(), d.Pool, userID, d.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list trips")
		return
	}

	out := make([]map[string]any, 0, len(trips))
	for _, t := range trips {
		out = append(out, map[string]any{
			"id":                t.OccurrenceID,
			"role":              t.Role,
			"depart_earliest":   t.DepartEarliest,
			"depart_latest":     t.DepartLatest,
			"status":            t.Status,
			"route_km":          t.RouteKm,
			"origin_label":      t.OriginLabel,
			"destination_label": t.DestLabel,
			"price_eur":         t.PriceEUR,
			"seats_free":        t.SeatsFree,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": out})
}

func (d Deps) handleGetTrip(w http.ResponseWriter, r *http.Request) {
	userID, occurrenceID, _, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	trip, err := store.GetTrip(r.Context(), d.Pool, occurrenceID, userID)
	if errors.Is(err, store.ErrNotParticipant) {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the trip")
		return
	}

	passengers := make([]map[string]any, 0, len(trip.Passengers))
	for _, p := range trip.Passengers {
		passengers = append(passengers, map[string]any{
			"id": p.UserID, "name": p.Name, "photo_url": p.PhotoURL,
			"rating": p.Rating, "price_eur": p.PriceEUR, "seats": p.Seats,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                trip.OccurrenceID,
		"role":              trip.Role,
		"depart_earliest":   trip.DepartEarliest,
		"depart_latest":     trip.DepartLatest,
		"status":            trip.Status,
		"route_km":          trip.RouteKm,
		"origin_label":      trip.OriginLabel,
		"destination_label": trip.DestLabel,
		"route_polyline":    trip.RoutePolyline,
		"template_id":       trip.TemplateID,
		"weekday":           trip.Weekday,
		"booking_id":        trip.BookingID,
		"driver": map[string]any{
			"id": trip.Driver.UserID, "name": trip.Driver.Name,
			"photo_url": trip.Driver.PhotoURL, "rating": trip.Driver.Rating,
			"car_model": trip.Driver.CarModel, "car_color": trip.Driver.CarColor,
			// Revealed only here, and only to people already on the trip.
			"car_plate": trip.Driver.CarPlate,
		},
		"passengers": passengers,
	})
}

func (d Deps) handleConfirmTrip(w http.ResponseWriter, r *http.Request) {
	userID, occurrenceID, role, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	err := store.ConfirmTrip(r.Context(), d.Pool, occurrenceID, userID, role, d.now())
	if errors.Is(err, store.ErrNotDeparted) {
		writeError(w, http.StatusConflict, "not_departed", "you can confirm once the trip has set off")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not confirm the trip")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleRateTrip(w http.ResponseWriter, r *http.Request) {
	userID, occurrenceID, role, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	var body struct {
		Stars int     `json:"stars"`
		Tag   *string `json:"tag"`
		// RateeID names which passenger is being rated. A driver carrying
		// several must say; a passenger may omit it.
		RateeID *string `json:"ratee_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	var rateeID *uuid.UUID
	if body.RateeID != nil {
		id, err := uuid.Parse(*body.RateeID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "ratee_id must be a uuid")
			return
		}
		rateeID = &id
	}
	if body.Stars < 1 || body.Stars > 5 {
		writeError(w, http.StatusBadRequest, "invalid_request", "stars must be between 1 and 5")
		return
	}

	err := store.RateTrip(r.Context(), d.Pool, occurrenceID, userID, role, rateeID, body.Stars, body.Tag)
	switch {
	case errors.Is(err, store.ErrNotCompleted):
		writeError(w, http.StatusConflict, "not_completed", "you can rate once the ride is completed")
	case errors.Is(err, store.ErrAlreadyRated):
		writeError(w, http.StatusConflict, "already_rated", "you have already rated this ride")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not store the rating")
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

func (d Deps) handleListMessages(w http.ResponseWriter, r *http.Request) {
	_, occurrenceID, _, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	var afterTime *time.Time
	var afterID *uuid.UUID
	if v := r.URL.Query().Get("after_time"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "after_time must be RFC3339")
			return
		}
		afterTime = &t
	}
	if v := r.URL.Query().Get("after_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "after_id must be a uuid")
			return
		}
		afterID = &id
	}

	if (afterTime == nil) != (afterID == nil) {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"after_time and after_id must be given together")
		return
	}

	messages, err := store.ListMessages(r.Context(), d.Pool, occurrenceID, afterTime, afterID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load messages")
		return
	}

	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		out = append(out, map[string]any{
			"id": m.ID, "sender_id": m.SenderID, "sender_name": m.SenderName,
			"body": m.Body, "created_at": m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func (d Deps) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	userID, occurrenceID, _, ok := d.tripParams(w, r)
	if !ok {
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	m, err := store.PostMessage(r.Context(), d.Pool, occurrenceID, userID, body.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Committed before it is published: the socket is an optimization, and a
	// message a client saw live must still be there when it refetches.
	if d.Hub != nil {
		d.Hub.Publish(occurrenceID, map[string]any{
			"type": "message", "id": m.ID, "sender_id": m.SenderID,
			"sender_name": m.SenderName, "body": m.Body, "created_at": m.CreatedAt,
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": m.ID, "sender_id": m.SenderID, "sender_name": m.SenderName,
		"body": m.Body, "created_at": m.CreatedAt,
	})
}

func (d Deps) handleBlockUser(w http.ResponseWriter, r *http.Request) {
	callerID, targetID, ok := d.userTarget(w, r)
	if !ok {
		return
	}

	if err := store.BlockUser(r.Context(), d.Pool, callerID, targetID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleUnblockUser(w http.ResponseWriter, r *http.Request) {
	callerID, targetID, ok := d.userTarget(w, r)
	if !ok {
		return
	}

	if err := store.UnblockUser(r.Context(), d.Pool, callerID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not unblock")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleReportUser(w http.ResponseWriter, r *http.Request) {
	caller, targetID, ok := d.userTarget(w, r)
	if !ok {
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if err := store.ReportUser(r.Context(), d.Pool, caller, targetID, body.Reason); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userTarget resolves the caller and the {id} user in the path.
func (d Deps) userTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	caller, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return uuid.Nil, uuid.Nil, false
	}
	target, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the user id must be a uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return caller, target, true
}

// now reads the injected clock, falling back to wall time.
func (d Deps) now() time.Time {
	if d.Clock != nil {
		return d.Clock.Now()
	}
	return time.Now()
}
