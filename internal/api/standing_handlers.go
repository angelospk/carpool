package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/bookings"
	"carpool/internal/standing"
	"carpool/internal/store"
)

// standingErrorStatus maps an arrangement error to its HTTP shape. A week that
// has no seat is deliberately distinct from every other conflict: it is the one
// the driver can act on, and the response names the dates.
func standingErrorStatus(err error) (int, string, string) {
	var full standing.ErrWeekFull
	switch {
	case errors.Is(err, standing.ErrNotYours):
		return http.StatusForbidden, "not_yours", "this arrangement is not yours"
	case errors.Is(err, standing.ErrNotRepeatable):
		return http.StatusConflict, "not_repeatable", "this ride cannot be repeated"
	case errors.As(err, &full):
		return http.StatusConflict, "week_full", "one of those days has no free seat"
	default:
		return http.StatusInternalServerError, "internal", "something went wrong"
	}
}

func (d Deps) writeStandingError(w http.ResponseWriter, err error) {
	var full standing.ErrWeekFull
	if errors.As(err, &full) {
		// The dates travel with the error so the driver is told which Tuesday
		// is the problem, rather than being left to guess at a refusal.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{
				"code":    "week_full",
				"message": "one of those days has no free seat",
				"dates":   full.Dates,
			},
		})
		return
	}
	status, code, message := standingErrorStatus(err)
	writeError(w, status, code, message)
}

// standingParams reads the caller and the id in the path.
func (d Deps) standingParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the id must be a uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

// handleRepeatBooking turns a ride the passenger took into a standing request
// for the same weekday, and asks the driver once.
func (d Deps) handleRepeatBooking(w http.ResponseWriter, r *http.Request) {
	userID, bookingID, ok := d.standingParams(w, r)
	if !ok {
		return
	}

	id, err := d.Standing.Repeat(r.Context(), bookingID, userID)
	if err != nil {
		d.writeStandingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrangement_id": id})
}

func (d Deps) handleApproveArrangement(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := d.standingParams(w, r)
	if !ok {
		return
	}
	if err := d.Standing.Approve(r.Context(), id, userID); err != nil {
		d.writeStandingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDeclineArrangement(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := d.standingParams(w, r)
	if !ok {
		return
	}
	if err := d.Standing.Decline(r.Context(), id, userID); err != nil {
		d.writeStandingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleCancelArrangement(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := d.standingParams(w, r)
	if !ok {
		return
	}
	if err := d.Standing.Cancel(r.Context(), id, userID); err != nil {
		d.writeStandingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCancelBooking gives back one seat. For a standing arrangement that is
// one week called off, and the agreement carries on.
func (d Deps) handleCancelBooking(w http.ResponseWriter, r *http.Request) {
	userID, bookingID, ok := d.standingParams(w, r)
	if !ok {
		return
	}
	switch err := d.Bookings.Cancel(r.Context(), bookingID, userID); {
	case errors.Is(err, bookings.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such booking")
		return
	case errors.Is(err, bookings.ErrNotYours):
		writeError(w, http.StatusForbidden, "not_yours", "that seat is not yours")
		return
	case errors.Is(err, bookings.ErrDeparted):
		writeError(w, http.StatusConflict, "departed", "that trip has already left")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not cancel that seat")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListArrangements returns the arrangements the caller is part of, on
// either side, so the app can show the switch in the state it is really in.
func (d Deps) handleListArrangements(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	list, err := d.Standing.ForUser(r.Context(), userID)
	if err != nil {
		d.writeStandingError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"id":                a.ID,
			"template_id":       a.TemplateID,
			"weekday":           a.Weekday,
			"seats":             a.Seats,
			"price_eur":         a.PriceEUR,
			"status":            a.Status,
			"until_date":        a.Until.Format("2006-01-02"),
			"driver_id":         a.DriverID,
			"passenger_id":      a.PassengerID,
			"passenger_name":    a.PassengerName,
			"origin_label":      a.OriginLabel,
			"destination_label": a.DestinationLabel,
			"weeks_booked":      a.WeeksBooked,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrangements": out})
}

// handlePastTrips lists the rides the caller has already taken, which is where
// a repeat starts from.
func (d Deps) handlePastTrips(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	trips, err := store.PastTrips(r.Context(), d.Pool, userID, d.now(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load your rides")
		return
	}
	out := make([]map[string]any, 0, len(trips))
	for _, t := range trips {
		out = append(out, map[string]any{
			"id":                t.OccurrenceID,
			"booking_id":        t.BookingID,
			"role":              "passenger",
			"depart_earliest":   t.DepartEarliest,
			"depart_latest":     t.DepartLatest,
			"status":            t.Status,
			"route_km":          t.RouteKm,
			"origin_label":      t.OriginLabel,
			"destination_label": t.DestLabel,
			"price_eur":         t.PriceEUR,
			"seats_free":        t.SeatsFree,
			"driver_name":       t.DriverName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": out})
}

// handleAgain asks the same driver for the same ride, on their next running.
func (d Deps) handleAgain(w http.ResponseWriter, r *http.Request) {
	userID, bookingID, ok := d.standingParams(w, r)
	if !ok {
		return
	}

	requestID, askID, err := d.Asks.Again(r.Context(), bookingID, userID)
	switch {
	case errors.Is(err, asks.ErrNothingToRepeat):
		writeError(w, http.StatusConflict, "nothing_to_repeat",
			"that route has no next trip with a free seat")
		return
	case errors.Is(err, asks.ErrNotYours):
		writeError(w, http.StatusForbidden, "not_yours", "that ride is not yours")
		return
	case errors.Is(err, asks.ErrNoCandidates):
		// The request stands; that particular day simply did not work out.
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "asking": false})
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not repeat that ride")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": requestID, "ask_id": askID, "asking": true,
	})
}
