package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/asks"
	"carpool/internal/domain"
	"carpool/internal/match"
	"carpool/internal/outbox"
	"carpool/internal/store"
)

// maxAlternativeDays is how many days one journey can be looking for a driver
// on at once. A week covers "this week, any day"; more is a standing request.
const maxAlternativeDays = 7

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
		Seats            *int      `json:"seats"`
		// Luggage is what they are bringing. Absent means a bag they can hold,
		// which is most people; the one bringing skis is the one who will
		// happily say so.
		Luggage string `json:"luggage"`
		// LuggageCount is how many of them. Absent means one when a size was
		// given and none when it was not, so a client that has not been updated
		// keeps meaning what it used to.
		LuggageCount *int `json:"luggage_count"`
		// AlternativeOf is the request this one is another day of: the same
		// journey, "or else on Friday". Accepting any day withdraws the rest.
		AlternativeOf *string `json:"alternative_of"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var alternativeOf *uuid.UUID
	if body.AlternativeOf != nil {
		id, err := uuid.Parse(*body.AlternativeOf)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "alternative_of must be a uuid")
			return
		}
		alternativeOf = &id
	}

	seats, problem := seatsProblem(body.Seats, 1, maxPartySeats)
	if problem != nil {
		writeError(w, http.StatusBadRequest, problem.code, problem.message)
		return
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

	// The pair has to agree: "none" is nothing, and nothing is "none". The
	// schema refuses the other combinations, and a 500 from a CHECK is a worse
	// way to learn it than a 400 that says so.
	count := 1
	if luggage == domain.LuggageNone {
		count = 0
	}
	if body.LuggageCount != nil {
		count = *body.LuggageCount
	}
	if (luggage == domain.LuggageNone) != (count == 0) || count < 0 || count > maxLuggageItems {
		writeError(w, http.StatusBadRequest, "invalid_luggage",
			"say how many items you are bringing, between one and four, or say you have none")
		return
	}
	if !body.WindowStart.Before(body.WindowEnd) {
		writeError(w, http.StatusBadRequest, "invalid_request", "window_start must precede window_end")
		return
	}
	if problem := d.windowProblem(body.WindowStart, body.WindowEnd); problem != nil {
		writeError(w, http.StatusBadRequest, problem.code, problem.message)
		return
	}
	if problem := distanceProblem(body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat); problem != nil {
		writeError(w, http.StatusBadRequest, problem.code, problem.message)
		return
	}

	// Both labels are what somebody else will read: on a trip card, and — since
	// the demand count — aggregated into a line put in front of every driver in
	// the area. They arrive from the client and nothing checked them, so the
	// only ceiling was the 64 KiB body.
	originLabel, err := domain.NormalisePlaceLabel(body.OriginLabel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"the pickup needs a name somebody can read")
		return
	}
	destinationLabel, err := domain.NormalisePlaceLabel(body.DestinationLabel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"the destination needs a name somebody can read")
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

	// The same journey asked for twice is one request. A double tap or a retry
	// over a bad connection used to make a second one, and each copy went on to
	// ask the same driver about the same trip. Near enough is a few hundred
	// metres at each end and an overlapping window, for the same party; the
	// answer is the request already under way, so the app carries on as if this
	// one had been made.
	tx, err := d.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	// Held until commit, so two copies of the same post arriving together
	// cannot both look, both find nothing, and both insert.
	if _, err := tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock(hashtext('ride_request:' || $1::text))`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
		return
	}

	// Another day joins the group of the first day, whichever day it names,
	// so the group has one root for Accept to find.
	//
	// Under the passenger's matching lock too, the one Accept takes first: a
	// day added while a driver is accepting another would otherwise be
	// inserted after Accept withdrew the group, and survive it.
	var root *uuid.UUID
	if alternativeOf != nil {
		if _, err := tx.Exec(r.Context(), match.PassengerLockSQL, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
			return
		}
		var id uuid.UUID
		var status string
		err := tx.QueryRow(r.Context(), `
			SELECT COALESCE(alternative_of, id), status
			  FROM ride_requests WHERE id = $1 AND passenger_id = $2`,
			*alternativeOf, userID).Scan(&id, &status)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "not_found", "no such request")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
			return
		case status != "open" && status != "asking":
			writeError(w, http.StatusConflict, "request_closed",
				"that request is no longer looking for a driver")
			return
		}
		root = &id
	}
	// One more live day in the group, refused past a week of them.
	groupFull := func() (bool, error) {
		var days int
		err := tx.QueryRow(r.Context(), `
			SELECT count(*) FROM ride_requests
			 WHERE passenger_id = $1 AND status IN ('open', 'asking')
			   AND COALESCE(alternative_of, id) = $2`, userID, *root).Scan(&days)
		return days >= maxAlternativeDays, err
	}

	var existing struct {
		id     uuid.UUID
		status string
		askID  *uuid.UUID
		root   uuid.UUID
		linked bool
	}
	err = tx.QueryRow(r.Context(), `
		SELECT rr.id, rr.status,
		       (SELECT a.id FROM asks a WHERE a.request_id = rr.id AND a.status = 'pending'
		         ORDER BY a.created_at DESC LIMIT 1),
		       COALESCE(rr.alternative_of, rr.id),
		       rr.alternative_of IS NOT NULL
		         OR EXISTS (SELECT 1 FROM ride_requests c WHERE c.alternative_of = rr.id)
		  FROM ride_requests rr
		 WHERE rr.passenger_id = $1
		   AND rr.status IN ('open', 'asking')
		   AND rr.seats = $2
		   AND rr.luggage = $9::luggage_size AND rr.luggage_count = $10
		   AND rr.window_start < $4 AND rr.window_end > $3
		   -- One whose time has passed is over, whatever its status still says
		   -- before the sweeper comes round.
		   AND rr.window_end > $11
		   AND ST_DWithin(rr.origin::geography, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, 500)
		   AND ST_DWithin(rr.destination::geography, ST_SetSRID(ST_MakePoint($7, $8), 4326)::geography, 500)
		 ORDER BY rr.created_at
		 LIMIT 1`,
		userID, seats, body.WindowStart, body.WindowEnd,
		body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat,
		string(luggage), count, d.now(),
	).Scan(&existing.id, &existing.status, &existing.askID, &existing.root, &existing.linked)
	switch {
	case err == nil && root != nil && existing.root != *root:
		// The same day is already asked for, on its own or as another
		// journey's day. On its own it joins this group, so accepting either
		// withdraws both; in another group it cannot be in two.
		if existing.linked {
			writeError(w, http.StatusConflict, "day_in_another_group",
				"that day is already asked for with other days")
			return
		}
		if full, err := groupFull(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
			return
		} else if full {
			writeError(w, http.StatusConflict, "too_many_days",
				"one journey can be asked for on up to seven days")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE ride_requests SET alternative_of = $2, updated_at = now() WHERE id = $1`,
			existing.id, *root); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
			return
		}
		fallthrough
	case err == nil:
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not check your open requests")
			return
		}
		out := map[string]any{"request_id": existing.id, "asking": existing.status == "asking"}
		if existing.askID != nil {
			out["ask_id"] = *existing.askID
		} else if existing.status == "open" {
			// Nobody is being asked. Perhaps nobody could be the first time, or
			// perhaps that attempt failed after the insert and this post is the
			// retry: either way the retry should do what the first post meant
			// to, rather than report success and leave the next move to the
			// reconciler an hour from now.
			if askID, err := d.Asks.AskNext(r.Context(), existing.id); err == nil {
				out["asking"], out["ask_id"] = true, askID
			} else if !errors.Is(err, asks.ErrNoCandidates) {
				slog.Warn("asking again for an existing request", "request", existing.id, "error", err)
			} else if askID := d.liveAsk(r.Context(), existing.id); askID != nil {
				out["asking"], out["ask_id"] = true, *askID
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	case !errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusInternalServerError, "internal", "could not check your open requests")
		return
	}

	if root != nil {
		if full, err := groupFull(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
			return
		} else if full {
			writeError(w, http.StatusConflict, "too_many_days",
				"one journey can be asked for on up to seven days")
			return
		}
	}

	var requestID uuid.UUID
	err = tx.QueryRow(r.Context(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status, luggage, luggage_count,
		                           alternative_of)
		VALUES ($1,
		        ST_SetSRID(ST_MakePoint($2, $3), 4326),
		        ST_SetSRID(ST_MakePoint($4, $5), 4326),
		        $6, $7, $8, $9, $10, 'open', 'open', $11::luggage_size, $12, $13)
		RETURNING id`,
		userID, body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat,
		originLabel, destinationLabel, body.WindowStart, body.WindowEnd, seats,
		string(luggage), count, root).Scan(&requestID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request was rejected")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save the request")
		return
	}

	// Approach the first driver straight away. A failure here is not fatal: the
	// request stays open, and a trip appearing on its corridor wakes it —
	// published, materialized, or a seat handed back.
	askID, err := d.Asks.AskNext(r.Context(), requestID)
	matched := err == nil
	if err != nil && !errors.Is(err, asks.ErrNoCandidates) {
		writeError(w, http.StatusInternalServerError, "internal", "could not start matching")
		return
	}
	if !matched {
		if live := d.liveAsk(r.Context(), requestID); live != nil {
			askID, matched = *live, true
		}
	}

	out := map[string]any{"request_id": requestID, "asking": matched}
	if matched {
		out["ask_id"] = askID
	}
	writeJSON(w, http.StatusCreated, out)
}

// missedJSON is why the last search asked nobody though a car passes, or nil.
func missedJSON(q store.OpenRequest) map[string]any {
	if q.MissedReason == nil {
		return nil
	}
	return map[string]any{"reason": *q.MissedReason, "need": q.MissedNeed, "limit": q.MissedLimit}
}

// liveAsk is the request's pending ask, if it has one.
//
// For a post whose own attempt to ask found nothing: a copy of the same post,
// arriving at the same moment, may have asked the driver first. AskNext then
// sees a request that is no longer open, or loses the race on the one pending
// ask, and says there was nobody. Both of those commit before AskNext returns,
// so this read sees the ask (QA 2026-10-16, B5).
func (d Deps) liveAsk(ctx context.Context, requestID uuid.UUID) *uuid.UUID {
	var id uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT id FROM asks WHERE request_id = $1 AND status = 'pending'
		 ORDER BY created_at DESC LIMIT 1`, requestID).Scan(&id)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("looking for a live ask", "request", requestID, "error", err)
		}
		return nil
	}
	return &id
}

// handleMyRequests lists what the caller is still waiting on, and how far the
// search for a driver has got.
//
// It is not guarded on deps.Asks the way posting a request is: this only reads
// the pool, and a deployment wired without the matcher should still be able to
// show somebody the requests they already have rather than answering 404 to a
// screen that exists.
func (d Deps) handleMyRequests(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	list, err := store.OpenRequests(r.Context(), d.Pool, userID, d.now(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load your requests")
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, q := range list {
		out = append(out, map[string]any{
			"id":                     q.ID,
			"origin_label":           q.OriginLabel,
			"destination_label":      q.DestinationLabel,
			"window_start":           q.WindowStart,
			"window_end":             q.WindowEnd,
			"seats":                  q.Seats,
			"status":                 q.Status,
			"drivers_asked":          q.DriversAsked,
			"drivers_declined":       q.DriversDeclined,
			"asking_now":             q.AskingNow,
			"ask_expires_at":         q.AskExpiresAt,
			"drivers_on_corridor":    q.DriversOnCorridor,
			"drivers_untried":        q.DriversUntried,
			"most_seats_on_corridor": q.MostSeatsOnCorridor,
			"drivers_departing":      q.DriversDeparting,
			"nearest_departure":      q.NearestDeparture,
			"missed":                 missedJSON(q),
			"alternative_of":         q.AlternativeOf,
			"stranded":               q.Stranded,
			"created_at":             q.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
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

	// What state it is in decides the answer, so it is read before anything is
	// written. This used to be one statement whose row count was discarded, and
	// every call answered 204: a request that had just been matched, one
	// belonging to somebody else, and one that never existed all came back as
	// "cancelled". The app takes 204 as done and navigates home, so a passenger
	// whose driver accepted a moment earlier was sent away believing they were
	// not going, while a seat was held in their name and a driver was on the way
	// to a meeting point.
	var status string
	err = d.Pool.QueryRow(r.Context(),
		`SELECT status FROM ride_requests WHERE id = $1 AND passenger_id = $2`,
		requestID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		// Somebody else's request and one that does not exist get the same
		// answer, so this cannot be used to find out what other people are
		// asking for.
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
		return
	}

	switch status {
	case "open", "asking":
		// Cancelling also kills any live ask, so a driver cannot accept a ride
		// the passenger has already withdrawn.
		//
		// The write is the authority, not the read above it. A driver accepting
		// in the moment between the two is the same race this whole handler is
		// about, only narrower: the statement would match nothing and answering
		// 204 for it would be the original bug with a smaller window. So the
		// affected row is returned, and when there is none the status is read
		// again to say what actually happened.
		//
		// The seats a killed ask held go back with it, so whoever waits behind
		// them is woken in the same transaction. Left to the hourly reconcile,
		// a one-seat trip sat unoffered for up to an hour.
		tx, err := d.Pool.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		// ?days=all takes every live day of the same journey with it (QA
		// 2026-10-17 U F6); without it only the day named goes. The named day
		// is still the one whose row decides the answer below.
		var cancelled uuid.UUID
		var freed []uuid.UUID
		err = tx.QueryRow(r.Context(), `
			WITH me AS (
				SELECT COALESCE(alternative_of, id) AS root FROM ride_requests
				 WHERE id = $1 AND passenger_id = $2
			), cancelled AS (
				UPDATE ride_requests SET status = 'cancelled', updated_at = now()
				WHERE passenger_id = $2 AND status IN ('open','asking')
				  AND (id = $1 OR ($3 AND COALESCE(alternative_of, id) = (SELECT root FROM me)))
				RETURNING id
			), killed AS (
				UPDATE asks SET status = 'cancelled', responded_at = now()
				WHERE request_id IN (SELECT id FROM cancelled) AND status = 'pending'
				RETURNING occurrence_id
			)
			SELECT id, ARRAY(SELECT DISTINCT occurrence_id FROM killed) FROM cancelled WHERE id = $1`,
			requestID, userID, r.URL.Query().Get("days") == "all").Scan(&cancelled, &freed)
		if errors.Is(err, pgx.ErrNoRows) {
			// Released first: reportCancelRace reads through the pool, and
			// holding this connection while it waits for another can starve a
			// small pool.
			_ = tx.Rollback(r.Context())
			d.reportCancelRace(w, r, requestID, userID)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
			return
		}
		if err := outbox.WakeAll(r.Context(), tx, freed, "request_cancelled", d.now()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
			return
		}
	case "matched":
		// There is a booking now, and cancelling that is a different act with a
		// different consequence: a driver is expecting them. The app sends them
		// to the trip rather than pretending this worked.
		writeError(w, http.StatusConflict, "already_matched",
			"a driver has taken this ride; cancel the booking instead")
		return
	default:
		// Already cancelled, or expired. The second press of a button and a
		// retry after a dropped response both arrive here, and both mean what
		// they asked for is true.
	}

	w.WriteHeader(http.StatusNoContent)
}

// reportCancelRace answers when the cancelling statement matched nothing.
//
// Reached only when the request moved between the status read and the write,
// which in practice means a driver accepted while the passenger was pressing
// cancel. Re-reading is what keeps the answer honest for the one case that
// matters: they must not be told the ride is off while somebody is driving to
// meet them.
func (d Deps) reportCancelRace(w http.ResponseWriter, r *http.Request, requestID, userID uuid.UUID) {
	var status string
	err := d.Pool.QueryRow(r.Context(),
		`SELECT status FROM ride_requests WHERE id = $1 AND passenger_id = $2`,
		requestID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not cancel the request")
		return
	}
	if status == "matched" {
		writeError(w, http.StatusConflict, "already_matched",
			"a driver has taken this ride; cancel the booking instead")
		return
	}
	// Cancelled or expired by something else in the meantime, which is what they
	// were asking for.
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

// maxLuggageItems bounds what a passenger can declare. Somebody with five things
// is moving house, and that is a conversation to have rather than a form to fill
// in. It matches the CHECK on ride_requests.luggage_count.
const maxLuggageItems = 4

// handleRequestAlternatives lists, anonymously, the departures the matcher
// would try next for one of the caller's own requests. It is what the request
// screen shows a passenger whose driver cancelled: there is still a car on
// this road, and it is already being asked.
func (d Deps) handleRequestAlternatives(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	requestID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	list, err := d.Asks.Alternatives(r.Context(), requestID, userID, 5)
	if errors.Is(err, asks.ErrNotYourRequest) {
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not look for alternatives")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"depart_earliest":   a.DepartEarliest,
			"depart_latest":     a.DepartLatest,
			"origin_label":      a.OriginLabel,
			"destination_label": a.DestinationLabel,
			"seats_free":        a.SeatsFree,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"alternatives": out})
}
