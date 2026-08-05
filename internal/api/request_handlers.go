package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/domain"
	"carpool/internal/store"
)

func (d Deps) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		OriginLon        float64   `json:"origin_lon"`
		OriginLat        float64   `json:"origin_lat"`
		DestinationLon   float64   `json:"destination_lon"`
		DestinationLat   float64   `json:"destination_lat"`
		OriginLabel      string    `json:"origin_label"`
		DestinationLabel string    `json:"destination_label"`
		WindowStart      time.Time `json:"window_start"`
		WindowEnd        time.Time `json:"window_end"`
		Seats            int       `json:"seats"`
		// Luggage is what they are bringing. Absent means a bag they can hold,
		// which is most people; the one bringing skis is the one who will
		// happily say so.
		Luggage string `json:"luggage"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.Seats <= 0 {
		body.Seats = 1
	}
	luggage := domain.LuggageNone
	if body.Luggage != "" {
		parsed, err := domain.ParseLuggage(body.Luggage)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_luggage",
				"that is not a luggage size this app knows")
			return
		}
		luggage = parsed
	}
	if !body.WindowStart.Before(body.WindowEnd) {
		writeError(w, http.StatusBadRequest, "invalid_request", "window_start must precede window_end")
		return
	}

	// Both ends have to be in the country. Nothing narrower.
	//
	// This used to require one end inside the passenger's own prefecture, which
	// was the pilot's geofence: the app served Έβρος, so a trip with neither end
	// there was a trip the app could not match. With the whole country open that
	// rule stops describing anything real — it refuses Θεσσαλονίκη → Αθήνα to
	// somebody who lives in Ξάνθη, which is exactly the journey a carpool is
	// for — and it quietly made a profile field into an authorisation boundary,
	// which it was never designed to be.
	//
	// What is left is the honest limit: there is no map, no toll data and no
	// routing outside Greece.
	for _, end := range []struct {
		what     string
		lon, lat float64
	}{
		{"pickup", body.OriginLon, body.OriginLat},
		{"destination", body.DestinationLon, body.DestinationLat},
	} {
		_, err := store.RegionAt(r.Context(), d.Pool, end.lon, end.lat)
		if errors.Is(err, store.ErrOutsideGreece) {
			writeError(w, http.StatusBadRequest, "outside_country",
				"the "+end.what+" is outside Greece, which is as far as this app goes")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not locate the trip")
			return
		}
	}

	var requestID uuid.UUID
	err := d.Pool.QueryRow(r.Context(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status, luggage)
		VALUES ($1,
		        ST_SetSRID(ST_MakePoint($2, $3), 4326),
		        ST_SetSRID(ST_MakePoint($4, $5), 4326),
		        $6, $7, $8, $9, $10, 'open', 'open', $11::luggage_size)
		RETURNING id`,
		userID, body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat,
		body.OriginLabel, body.DestinationLabel, body.WindowStart, body.WindowEnd, body.Seats,
		string(luggage)).Scan(&requestID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request was rejected")
		return
	}

	// Approach the first driver straight away. A failure here is not fatal: the
	// request stays open and is retried when the next occurrence appears.
	askID, err := d.Asks.AskNext(r.Context(), requestID)
	matched := err == nil
	if err != nil && !errors.Is(err, asks.ErrNoCandidates) {
		writeError(w, http.StatusInternalServerError, "internal", "could not start matching")
		return
	}

	out := map[string]any{"request_id": requestID, "asking": matched}
	if matched {
		out["ask_id"] = askID
	}
	writeJSON(w, http.StatusCreated, out)
}

func (d Deps) handleCancelRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	requestID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request id must be a uuid")
		return
	}

	// Cancelling also kills any live ask, so a driver cannot accept a ride the
	// passenger has already withdrawn.
	tag, err := d.Pool.Exec(r.Context(), `
		WITH cancelled AS (
			UPDATE ride_requests SET status = 'cancelled', updated_at = now()
			WHERE id = $1 AND passenger_id = $2 AND status IN ('open','asking')
			RETURNING id
		)
		UPDATE asks SET status = 'cancelled', responded_at = now()
		WHERE request_id IN (SELECT id FROM cancelled) AND status = 'pending'`,
		requestID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
		return
	}
	_ = tag

	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handlePutPushToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	if body.Platform != "ios" && body.Platform != "android" {
		writeError(w, http.StatusBadRequest, "invalid_request", "platform must be ios or android")
		return
	}

	// A token can move between accounts when a device is handed on, so
	// re-registering reassigns it and clears any previous disabled flag.
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO push_tokens (token, user_id, platform)
		VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE
		SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, disabled_at = NULL`,
		body.Token, userID, body.Platform); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not register the token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
