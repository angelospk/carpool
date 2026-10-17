package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/weekend"
)

func planJSON(p weekend.Plan) map[string]any {
	return map[string]any{
		"id":                p.ID,
		"origin_lon":        p.OriginLon,
		"origin_lat":        p.OriginLat,
		"destination_lon":   p.DestinationLon,
		"destination_lat":   p.DestinationLat,
		"origin_label":      p.OriginLabel,
		"destination_label": p.DestinationLabel,
		"depart_dow":        p.DepartDow,
		"depart_time":       p.DepartTime,
		"return_dow":        p.ReturnDow,
		"return_time":       p.ReturnTime,
		"seats":             p.Seats,
		"luggage":           p.Luggage,
		"luggage_count":     p.LuggageCount,
		"every_week":        p.EveryWeek,
		"declines_in_row":   p.DeclinesInRow,
		"this_week":         p.ThisWeek,
		"ask_to_stop":       p.AskToStop,
	}
}

// handleCreateWeekendPlan stores what a passenger who leaves town most
// weekends says once: where, which day and time, and whether to come back.
func (d Deps) handleCreateWeekendPlan(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		OriginLon        float64 `json:"origin_lon"`
		OriginLat        float64 `json:"origin_lat"`
		DestinationLon   float64 `json:"destination_lon"`
		DestinationLat   float64 `json:"destination_lat"`
		OriginLabel      string  `json:"origin_label"`
		DestinationLabel string  `json:"destination_label"`
		DepartDow        *int    `json:"depart_dow"`
		DepartTime       string  `json:"depart_time"`
		ReturnDow        *int    `json:"return_dow"`
		ReturnTime       *string `json:"return_time"`
		Seats            *int    `json:"seats"`
		Luggage          string  `json:"luggage"`
		LuggageCount     *int    `json:"luggage_count"`
		EveryWeek        *bool   `json:"every_week"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	// The same defaults as a request: absent seats is one, absent luggage is
	// nothing, and an absent count is one when a size was given.
	plan := weekend.Plan{
		OriginLon: body.OriginLon, OriginLat: body.OriginLat,
		DestinationLon: body.DestinationLon, DestinationLat: body.DestinationLat,
		OriginLabel: body.OriginLabel, DestinationLabel: body.DestinationLabel,
		DepartDow: -1, DepartTime: body.DepartTime,
		ReturnDow: body.ReturnDow, ReturnTime: body.ReturnTime,
		Seats: 1, Luggage: "none", EveryWeek: true,
	}
	if body.DepartDow != nil {
		plan.DepartDow = *body.DepartDow
	}
	if body.Seats != nil {
		plan.Seats = *body.Seats
	}
	if body.Luggage != "" {
		plan.Luggage = body.Luggage
		plan.LuggageCount = 1
	}
	if body.LuggageCount != nil {
		plan.LuggageCount = *body.LuggageCount
	}
	if body.EveryWeek != nil {
		plan.EveryWeek = *body.EveryWeek
	}

	if problem := distanceProblem(body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat); problem != nil {
		writeError(w, http.StatusBadRequest, problem.code, problem.message)
		return
	}

	created, err := d.Weekend.Create(r.Context(), userID, plan)
	var bad *weekend.InvalidError
	if errors.As(err, &bad) {
		writeError(w, http.StatusBadRequest, bad.Code, bad.Message)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save the plan")
		return
	}
	writeJSON(w, http.StatusCreated, planJSON(created))
}

func (d Deps) handleListWeekendPlans(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	plans, err := d.Weekend.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load your plans")
		return
	}
	out := make([]map[string]any, 0, len(plans))
	for _, p := range plans {
		out = append(out, planJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

func (d Deps) handleStopWeekendPlan(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such plan")
		return
	}
	err = d.Weekend.Stop(r.Context(), userID, id)
	if errors.Is(err, weekend.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such plan")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not stop the plan")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleWeekendThisWeek takes the Thursday answer: are you going this weekend.
func (d Deps) handleWeekendThisWeek(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such plan")
		return
	}
	var body struct {
		Going *bool `json:"going"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Going == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "say whether you are going: true or false")
		return
	}
	plan, err := d.Weekend.ThisWeek(r.Context(), userID, id, *body.Going)
	if errors.Is(err, weekend.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such plan")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save your answer")
		return
	}
	writeJSON(w, http.StatusOK, planJSON(plan))
}
