package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/asks"
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
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.Seats <= 0 {
		body.Seats = 1
	}
	if !body.WindowStart.Before(body.WindowEnd) {
		writeError(w, http.StatusBadRequest, "invalid_request", "window_start must precede window_end")
		return
	}

	// A trip is admissible only if one endpoint lies in the passenger's home
	// region. Checking it here keeps the matching SQL free of the concern.
	var admissible bool
	err := d.Pool.QueryRow(r.Context(), `
		SELECT ST_Contains(g.boundary, ST_SetSRID(ST_MakePoint($2, $3), 4326))
		    OR ST_Contains(g.boundary, ST_SetSRID(ST_MakePoint($4, $5), 4326))
		FROM users u JOIN regions g ON g.id = u.home_region_id
		WHERE u.id = $1`,
		userID, body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat).Scan(&admissible)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "the account no longer exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not check the region")
		return
	}
	if !admissible {
		writeError(w, http.StatusBadRequest, "outside_region",
			"either the pickup or the destination must be inside your region")
		return
	}

	var requestID uuid.UUID
	err = d.Pool.QueryRow(r.Context(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status)
		VALUES ($1,
		        ST_SetSRID(ST_MakePoint($2, $3), 4326),
		        ST_SetSRID(ST_MakePoint($4, $5), 4326),
		        $6, $7, $8, $9, $10, 'open', 'open')
		RETURNING id`,
		userID, body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat,
		body.OriginLabel, body.DestinationLabel, body.WindowStart, body.WindowEnd, body.Seats).Scan(&requestID)
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
