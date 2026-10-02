package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// A driver's own routes: seeing them, and taking one down.
//
// Neither was possible before. A driver could publish a route and then had no
// way to look at what they had published, no way to tell two similar ones
// apart, and no way to stop offering one short of asking somebody with database
// access. Meanwhile the demo seed had to reach into psql to avoid piling up
// duplicates, which is the same missing endpoint seen from the other side.

type myRoute struct {
	ID               string  `json:"id"`
	OriginLabel      string  `json:"origin_label"`
	DestinationLabel string  `json:"destination_label"`
	RouteKm          float64 `json:"route_km"`
	Seats            int     `json:"seats"`
	PricingMode      string  `json:"pricing_mode"`
	Kind             string  `json:"kind"`
	Weekdays         []int   `json:"weekdays"`
	OneoffDate       *string `json:"oneoff_date,omitempty"`
	DepartEarliest   string  `json:"depart_earliest"`
	DepartLatest     string  `json:"depart_latest"`
	// UpcomingTrips is what makes retiring an informed decision rather than a
	// guess: it says how many materialised departures are still ahead.
	UpcomingTrips int `json:"upcoming_trips"`
	// LinkOpens is how many times a person opened the route's link: whether
	// sharing it reached anybody.
	LinkOpens int `json:"link_opens"`
}

func (d Deps) handleMyRoutes(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	rows, err := d.Pool.Query(r.Context(), `
		SELECT rt.id::text, rt.origin_label, rt.destination_label, rt.route_km,
		       rt.seats, rt.pricing_mode,
		       rs.kind, COALESCE(rs.weekdays, '{}'),
		       -- As text: pgx will not scan a date into a string, and a failed scan
		       -- here used to take down the whole list over one one-off route.
		       to_char(rs.oneoff_date, 'YYYY-MM-DD'),
		       to_char(rs.depart_earliest, 'HH24:MI'), to_char(rs.depart_latest, 'HH24:MI'),
		       (SELECT count(*) FROM trip_occurrences o
		         WHERE o.template_id = rt.id
		           AND o.depart_earliest > now()
		           AND o.status <> 'cancelled'),
		       rt.share_opens
		  FROM route_templates rt
		  JOIN route_schedules rs ON rs.template_id = rt.id
		 WHERE rt.driver_id = $1 AND rt.active
		 ORDER BY rs.depart_earliest, rt.created_at`, userID)
	if err != nil {
		slog.Error("listing routes", "driver", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not read your routes")
		return
	}
	defer rows.Close()

	// Never nil: an empty list is the honest answer for a driver who has not
	// published anything, and a client should not have to tell it from null.
	out := []myRoute{}
	for rows.Next() {
		var m myRoute
		var oneoff *string
		if err := rows.Scan(&m.ID, &m.OriginLabel, &m.DestinationLabel, &m.RouteKm,
			&m.Seats, &m.PricingMode, &m.Kind, &m.Weekdays, &oneoff,
			&m.DepartEarliest, &m.DepartLatest, &m.UpcomingTrips, &m.LinkOpens); err != nil {
			slog.Error("reading a route", "driver", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not read your routes")
			return
		}
		m.OneoffDate = oneoff
		out = append(out, m)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your routes")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}

// handleRetireRoute stops a route being offered.
//
// It marks the template inactive rather than deleting it. Trips already
// arranged on it keep their geometry, their price and their history, and a
// passenger who rode it last week can still open that trip. Deleting would take
// their record with it.
//
// Departures already materialised are deliberately left alone. A driver who
// wants to call off a specific morning cancels that trip; retiring the route
// means "stop offering this from now on", and silently cancelling on somebody
// who has a seat booked for tomorrow is a different act that should look
// different.
func (d Deps) handleRetireRoute(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		// Not a uuid is not a route, and saying which kind of wrong it is tells
		// a caller nothing they can act on.
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}

	// Scoped to the caller in the statement itself, so somebody else's route id
	// is indistinguishable from one that does not exist.
	tag, err := d.Pool.Exec(r.Context(), `
		UPDATE route_templates SET active = false, updated_at = now()
		 WHERE id = $1 AND driver_id = $2 AND active`, routeID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not retire the route")
		return
	}

	if tag.RowsAffected() == 0 {
		// Either it was never theirs, or it is already retired. The second is
		// not a failure: a driver who taps twice, or a client retrying a lost
		// response, wanted the route gone and it is gone. Told apart here only
		// so the first still 404s.
		var exists bool
		if err := d.Pool.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM route_templates WHERE id = $1 AND driver_id = $2)`,
			routeID, userID).Scan(&exists); err != nil {
			// A failed query is not a missing route. Reporting it as one tells
			// a driver their route is gone when it is not.
			writeError(w, http.StatusInternalServerError, "internal", "could not retire the route")
			return
		} else if !exists {
			writeError(w, http.StatusNotFound, "not_found", "no such route")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
