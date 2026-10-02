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
	// KeptTrips is how many of those retiring would leave standing, because
	// somebody has a seat on them or is being asked about one.
	KeptTrips int `json:"kept_trips"`
	// The ends as coordinates, so «Αλλαγή» can fill the publish form in.
	OriginLon      float64 `json:"origin_lon"`
	OriginLat      float64 `json:"origin_lat"`
	DestinationLon float64 `json:"destination_lon"`
	DestinationLat float64 `json:"destination_lat"`
	// LinkOpens is how many times a person opened the route's link: whether
	// sharing it reached anybody.
	LinkOpens int `json:"link_opens"`
	// NudgeWhenLeaving is whether the driver is told, as a departure comes
	// up, that somebody on the road is waiting.
	NudgeWhenLeaving bool `json:"nudge_when_leaving"`
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
		           AND o.depart_earliest > $2
		           AND o.status <> 'cancelled'),
		       (SELECT count(*) FROM trip_occurrences o
		         WHERE o.template_id = rt.id
		           AND o.depart_earliest > $2
		           AND o.status <> 'cancelled'
		           AND (EXISTS (SELECT 1 FROM bookings b
		                         WHERE b.occurrence_id = o.id AND b.status = 'confirmed')
		             OR EXISTS (SELECT 1 FROM asks a
		                         WHERE a.occurrence_id = o.id AND a.status = 'pending'))),
		       rt.share_opens, rt.nudge_when_leaving,
		       ST_X(rt.origin), ST_Y(rt.origin), ST_X(rt.destination), ST_Y(rt.destination)
		  FROM route_templates rt
		  JOIN route_schedules rs ON rs.template_id = rt.id
		 WHERE rt.driver_id = $1 AND rt.active
		 ORDER BY rs.depart_earliest, rt.created_at`, userID, d.now())
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
			&m.DepartEarliest, &m.DepartLatest, &m.UpcomingTrips, &m.KeptTrips, &m.LinkOpens, &m.NudgeWhenLeaving,
			&m.OriginLon, &m.OriginLat, &m.DestinationLon, &m.DestinationLat); err != nil {
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
// The departures already materialised that nobody is in go with it: up to
// three weeks of them, which a driver otherwise had to call off one morning at
// a time, and which kept a retired route in everybody's search until then. A
// departure somebody has a seat on, or is being asked about, stays. Calling
// that off is a different act, which tells the passenger, and the driver takes
// it on the trip itself.
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

	// Also on a second retire, so a route retired before this existed can be
	// cleared by retiring it again.
	if _, err := d.Pool.Exec(r.Context(), `
		UPDATE trip_occurrences o SET status = 'cancelled', updated_at = now()
		 WHERE o.template_id = $1
		   AND o.status = 'scheduled'
		   AND o.depart_earliest > $2
		   AND NOT EXISTS (SELECT 1 FROM bookings b
		                    WHERE b.occurrence_id = o.id AND b.status = 'confirmed')
		   AND NOT EXISTS (SELECT 1 FROM asks a
		                    WHERE a.occurrence_id = o.id AND a.status = 'pending')`,
		routeID, d.now()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the route was retired but its departures were not called off")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
