package api

import (
	"errors"
	"net/http"
	"time"

	"carpool/internal/store"
)

// supplyHorizonDays is how far ahead a supply count answers: today and the
// thirteen days after it, the same fortnight a one-off can be published for.
const supplyHorizonDays = 14

// handleSupply tells somebody searching whether cars go their way on a day,
// before they ask: "several" once at least store.SupplyFloor different drivers
// do, "unknown" otherwise — deliberately not "none", and never a number. A
// POST because it carries coordinates.
func (d Deps) handleSupply(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	var body struct {
		OriginLon      float64 `json:"origin_lon"`
		OriginLat      float64 `json:"origin_lat"`
		DestinationLon float64 `json:"destination_lon"`
		DestinationLat float64 `json:"destination_lat"`
		Date           string  `json:"date"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	athens, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "no time zone")
		return
	}
	day, err := time.ParseInLocation("2006-01-02", body.Date, athens)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "date must be YYYY-MM-DD")
		return
	}
	now := d.now()
	y, m, dd := now.In(athens).Date()
	today := time.Date(y, m, dd, 0, 0, 0, 0, athens)
	if day.Before(today) || !day.Before(today.AddDate(0, 0, supplyHorizonDays)) {
		writeError(w, http.StatusBadRequest, "invalid_request", "date must be within the next two weeks")
		return
	}

	for _, p := range [][2]float64{{body.OriginLon, body.OriginLat}, {body.DestinationLon, body.DestinationLat}} {
		_, err := store.RegionAt(r.Context(), d.Pool, p[0], p[1])
		if errors.Is(err, store.ErrOutsideGreece) {
			writeError(w, http.StatusBadRequest, "outside_country", "that point is outside Greece")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not locate the journey")
			return
		}
	}

	several, err := store.Supply(r.Context(), d.Pool, userID,
		body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat,
		day, day.AddDate(0, 0, 1), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not count drivers")
		return
	}
	status := "unknown"
	if several {
		status = "several"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}
