package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"carpool/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/drivers"
	"carpool/internal/geo"
	"carpool/internal/groups"
	"carpool/internal/match"
	"carpool/internal/store"
)

// routeBody is the publish-wizard payload.
type routeBody struct {
	OriginLon        float64 `json:"origin_lon"`
	OriginLat        float64 `json:"origin_lat"`
	DestinationLon   float64 `json:"destination_lon"`
	DestinationLat   float64 `json:"destination_lat"`
	OriginLabel      string  `json:"origin_label"`
	DestinationLabel string  `json:"destination_label"`
	Seats            *int    `json:"seats"`
	PricingMode      string  `json:"pricing_mode"`

	// Absent means "use my defaults", which is what the wizard sends when it
	// has not asked. A pointer rather than a zero value because false and 0 are
	// both meaningful answers here.
	AllowStops *bool    `json:"allow_stops"`
	DetourKm   *float64 `json:"detour_km"`
	DetourMin  *int     `json:"detour_min"`

	// Luggage is the largest thing this route will carry. Absent means a
	// suitcase, which is what an ordinary boot takes.
	Luggage string `json:"luggage"`
	// LuggageCapacity is how many of them are free for passengers, which is not
	// the size of the boot: the driver's own luggage is not modelled, so it has
	// to be deducted before it gets here. See store.RoutePlan.LuggageCapacity.
	// Absent means three, which is an ordinary boot on a trip with three seats
	// on offer.
	LuggageCapacity *int `json:"luggage_capacity"`

	// ContributionShare is how much of the computed cost share this driver
	// asks for, between zero (exclusive) and one. Absent means all of it, which
	// is what every route published before drivers could choose was implicitly
	// asking. The app computes the ceiling; this is the driver deciding where
	// under it they sit.
	//
	// A pointer, because zero is a real answer and not the same as silence. As
	// a plain float the two arrived identically and both were read as "all of
	// it", so a client that sent 0 by mistake asked each passenger for the full
	// figure — the wrong direction to be wrong in. Asking for nothing already
	// has its own representation in pricing_mode = 'free'.
	ContributionShare *float64 `json:"contribution_share"`

	// VehicleID names which of the driver's cars this route goes out in.
	// Absent means their default, which is what somebody with one car sends and
	// what the app sends until the driver has a second.
	VehicleID string `json:"vehicle_id"`

	// GroupID offers the route to one group's members only. Absent is
	// everybody. The driver has to be a member themselves: offering a route to
	// a group you are not in would be a way to reach its members without them
	// having let you in.
	GroupID string `json:"group_id"`

	Kind           string   `json:"kind"` // oneoff | recurring
	OneOffDate     string   `json:"oneoff_date"`
	Weekdays       []int    `json:"weekdays"`        // ISO 1..7
	DepartEarliest string   `json:"depart_earliest"` // "HH:MM" local Athens
	DepartLatest   string   `json:"depart_latest"`
	Exceptions     []string `json:"exceptions"`
}

func (d Deps) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body routeBody
	if !decodeJSON(w, r, &body) {
		return
	}
	params, ok := d.routeParams(w, r, userID, body, true)
	if !ok {
		return
	}

	templateID, err := store.CreateRouteTemplate(r.Context(), d.Pool, params)
	if errors.Is(err, store.ErrDuplicateRoute) {
		writeError(w, http.StatusConflict, "duplicate_route", "you already offer this route")
		return
	}
	if err != nil {
		// The response stays vague on purpose — a rejected route must not
		// describe the schema to whoever sent it. But throwing the reason away
		// entirely leaves nothing to debug from either, which cost an hour of
		// bisecting a seed script by hand.
		slog.Warn("route rejected", "driver", userID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request", "the route was rejected")
		return
	}

	// Materialize immediately so the route is matchable straight away rather
	// than at the next scheduled tick.
	if d.Materializer != nil {
		if err := d.Materializer.Run(r.Context()); err != nil {
			// The scheduled run will pick it up; publishing still succeeded.
			writeJSON(w, http.StatusCreated, map[string]any{
				"template_id": templateID, "materialized": false,
				"ends_at_port": params.EndsAtPort,
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"template_id": templateID,
		"route_km":    params.RouteKm,
		// The app needs this to explain a route that stops somewhere the driver
		// did not type. Without it the trip just looks wrong.
		"ends_at_port":        params.EndsAtPort,
		"materialized":        true,
		"first_occurrence_id": d.firstDeparture(r.Context(), templateID),
	})
}

// handleEditRoute changes a route the driver already published. The body is
// the publish form, with the same two ends.
//
// A departure somebody has a seat on, or is being asked about, keeps the time,
// seats and price it was agreed with; the rest take the new terms. The
// response says how many were kept, so the app can tell the driver which
// mornings stay as they were.
//
// Moving an end is a different route: 409 route_ends_changed, and the app
// publishes a new one and retires this.
func (d Deps) handleEditRoute(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	routeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}

	var body routeBody
	if !decodeJSON(w, r, &body) {
		return
	}
	params, ok := d.routeParams(w, r, userID, body, false)
	if !ok {
		return
	}

	err = store.UpdateRouteTemplate(r.Context(), d.Pool, routeID, params)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	if errors.Is(err, store.ErrDuplicateRoute) {
		writeError(w, http.StatusConflict, "duplicate_route", "you already offer this route")
		return
	}
	if errors.Is(err, store.ErrRouteEndsChanged) {
		writeError(w, http.StatusConflict, "route_ends_changed",
			"a route with other ends is a new route; publish it as one")
		return
	}
	if err != nil {
		slog.Warn("route edit rejected", "driver", userID, "route", routeID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request", "the route was rejected")
		return
	}

	materialized := true
	if d.Materializer != nil {
		if err := d.Materializer.Run(r.Context()); err != nil {
			// The scheduled run applies it; the edit itself is stored.
			slog.Warn("materialising an edited route", "route", routeID, "error", err)
			materialized = false
		}
	}

	var kept int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM trip_occurrences o
		 WHERE o.template_id = $1 AND o.status = 'scheduled' AND o.depart_earliest > $2
		   AND (EXISTS (SELECT 1 FROM bookings b
		                 WHERE b.occurrence_id = o.id AND b.status = 'confirmed')
		     OR EXISTS (SELECT 1 FROM asks a
		                 WHERE a.occurrence_id = o.id AND a.status = 'pending'))`,
		routeID, d.now()).Scan(&kept); err != nil {
		slog.Warn("counting kept departures", "route", routeID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"template_id":         routeID,
		"materialized":        materialized,
		"kept_trips":          kept,
		"first_occurrence_id": d.firstDeparture(r.Context(), routeID),
	})
}

// firstDeparture is the route's next departure, for a driver publishing a trip
// to leave on now: the app opens it, where «Φεύγω» is. Nil when there is none
// yet.
func (d Deps) firstDeparture(ctx context.Context, templateID uuid.UUID) *uuid.UUID {
	var first *uuid.UUID
	if err := d.Pool.QueryRow(ctx, `
		SELECT id FROM trip_occurrences
		 WHERE template_id = $1 AND status = 'scheduled' AND depart_latest > $2
		 ORDER BY depart_earliest LIMIT 1`, templateID, d.now()).Scan(&first); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("finding the first departure", "template", templateID, "error", err)
	}
	return first
}

// routeParams checks a publish-form body and turns it into what is stored. It
// writes the error response itself and reports whether to go on.
//
// snap is whether to route the two ends on the road network. An edit keeps the
// ends, so it keeps the line already stored and leaves the geometry fields
// empty for the caller to fill.
func (d Deps) routeParams(w http.ResponseWriter, r *http.Request, userID uuid.UUID, body routeBody, snap bool) (store.CreateRouteTemplateParams, bool) {
	fail := func(status int, code, message string) (store.CreateRouteTemplateParams, bool) {
		writeError(w, status, code, message)
		return store.CreateRouteTemplateParams{}, false
	}

	schedule, err := body.schedule(d.today())
	if err != nil {
		return fail(http.StatusBadRequest, "invalid_request", err.Error())
	}
	seats, problem := seatsProblem(body.Seats, 3, maxCarSeats) // 3 is the wizard's default
	if problem == nil {
		problem = d.scheduleProblem(schedule)
	}
	if problem == nil {
		problem = distanceProblem(body.OriginLon, body.OriginLat, body.DestinationLon, body.DestinationLat)
	}
	if problem != nil {
		return fail(http.StatusBadRequest, problem.code, problem.message)
	}

	// Above the ceiling is the one answer the app does not accept. Below it is
	// entirely theirs.
	share := 1.0
	if body.ContributionShare != nil {
		share = *body.ContributionShare
		if !(share > 0) || share > 1 {
			return fail(http.StatusBadRequest, "invalid_share",
				"you can ask for up to the whole share of the fuel, not more")
		}
	}

	// Checked here, with the rest of the body, rather than after the route has
	// been snapped: a typo in one field should not cost an OSRM call.
	luggage := domain.LuggageSuitcase
	if body.Luggage != "" {
		parsed, err := domain.ParseLuggage(body.Luggage)
		if err != nil {
			return fail(http.StatusBadRequest, "invalid_luggage",
				"that is not a luggage size this app knows")
		}
		luggage = parsed
	}

	// Size and number have to agree. "I will take nothing" and "I will take four
	// of nothing" are the same sentence, and only one of them is storable.
	capacity := defaultLuggageCapacity
	if luggage == domain.LuggageNone {
		capacity = 0
	}
	if body.LuggageCapacity != nil {
		capacity = *body.LuggageCapacity
	}
	if (luggage == domain.LuggageNone) != (capacity == 0) || capacity < 0 || capacity > maxLuggageCapacity {
		return fail(http.StatusBadRequest, "invalid_luggage",
			"say how many bags fit, between one and eight, or say the boot is full")
	}
	// An unknown mode used to be cast straight through to the insert, where the
	// column's CHECK refused it and the driver was told "internal error". The
	// set is closed and small, so it is checked here and named back.
	//
	// Absent is not an error: every client published before this field existed,
	// and they all meant the formula.
	var mode domain.PricingMode
	switch body.PricingMode {
	case "", string(domain.PricingFormula):
		mode = domain.PricingFormula
	case string(domain.PricingFreeContribution):
		mode = domain.PricingFreeContribution
	case string(domain.PricingFree):
		mode = domain.PricingFree
	default:
		return fail(http.StatusBadRequest, "invalid_pricing_mode",
			"a route either suggests a share of the fuel, leaves the contribution open, or asks for nothing")
	}

	// A share is a fraction of a figure the formula produced. Under any other
	// mode there is no figure, so a share stored beside it would be a number
	// nobody could later explain — see the receipt in domain.Basis.
	if mode != domain.PricingFormula {
		share = 1
	}

	var groupID *uuid.UUID
	if body.GroupID != "" {
		id, err := uuid.Parse(body.GroupID)
		member := false
		if err == nil {
			member, err = groups.IsMember(r.Context(), d.Pool, userID, id)
			if err != nil {
				return fail(http.StatusInternalServerError, "internal", "could not check the group")
			}
		}
		if !member {
			return fail(http.StatusForbidden, "not_in_group",
				"you can offer a route only to a group you belong to")
		}
		groupID = &id
	}

	// A driver profile is what makes someone publishable; without one there is
	// no car, no detour budget and no fuel figures to price with.
	var hasProfile bool
	if err := d.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM driver_profiles WHERE user_id = $1)`, userID).Scan(&hasProfile); err != nil {
		return fail(http.StatusInternalServerError, "internal", "could not check the driver profile")
	}
	if !hasProfile {
		return fail(http.StatusConflict, "no_driver_profile", "add your car details before publishing a route")
	}

	// What this driver will go out of their way, for this trip. Their profile
	// holds the defaults; the answer that binds is the one stored on the route
	// and snapshotted onto every occurrence of it.
	detour, err := d.detourFor(r.Context(), userID, body)
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "could not read your driver profile")
	}
	if detour.Km < 0 || detour.Km > maxDetourKm || detour.Min < 0 || detour.Min > maxDetourMin {
		return fail(http.StatusBadRequest, "invalid_request",
			"the detour you will accept is outside the range this service allows")
	}

	// The same check the request side makes. A route's labels are read by every
	// passenger it is offered to, and they arrive from the client with nothing
	// but the body size limit above them.
	originLabel, err := domain.NormalisePlaceLabel(body.OriginLabel)
	if err != nil {
		return fail(http.StatusBadRequest, "invalid_request",
			"the start needs a name somebody can read")
	}
	destinationLabel, err := domain.NormalisePlaceLabel(body.DestinationLabel)
	if err != nil {
		return fail(http.StatusBadRequest, "invalid_request",
			"the destination needs a name somebody can read")
	}

	origin := geo.Point{Lon: body.OriginLon, Lat: body.OriginLat}
	destination := geo.Point{Lon: body.DestinationLon, Lat: body.DestinationLat}

	// Both ends have to be in the country, which the request and estimate paths
	// have always checked and this one never did. The border runs through this
	// prefecture, and it matters more now that a snapping bound exists: a pin a
	// few kilometres into Turkey or Bulgaria near Ορεστιάδα is well inside
	// SnapRadiusMetres of a Greek road, so it snapped, routed and stored — a
	// journey beginning in another country, priced with Greek fuel and Greek
	// tolls. Measured: Αδριανούπολη → Κομοτηνή published at 184,2 km.
	var shaped *geo.ShapedRoute
	if snap {
		for _, end := range []struct {
			what string
			at   geo.Point
		}{
			{"start", origin},
			{"destination", destination},
		} {
			_, err := store.RegionAt(r.Context(), d.Pool, end.at.Lon, end.at.Lat)
			if errors.Is(err, store.ErrOutsideGreece) {
				return fail(http.StatusBadRequest, "outside_country",
					"the "+end.what+" is outside Greece, which is as far as this app goes")
			}
			if err != nil {
				return fail(http.StatusInternalServerError, "internal", "could not locate the route")
			}
		}

		// Snap the route to the road network now: the stored geometry is what the
		// gross match locates pickups along, so it must be a real road line rather
		// than the straight line between two pins.
		ctx, cancel := contextWithRoutingDeadline(r)
		defer cancel()

		route, err := d.Router.RouteWithShape(ctx, []geo.Point{origin, destination})
		if errors.Is(err, geo.ErrSeaCrossingOnly) {
			// Somewhere you sail to from a place you sail from. There is nothing to
			// share a car for, and quoting a trip of no kilometres would be worse
			// than saying so.
			return fail(http.StatusBadRequest, "sea_crossing",
				"that journey is a ferry crossing, so there is no drive to share")
		}
		if errors.Is(err, geo.ErrNoSegment) {
			// A point too far from any road the routing extract holds. That is a
			// fact about where the trip goes, not a failure of this service, and it
			// is the difference between a fabricated route and none: without the
			// snapping bound OSRM answered Ok for Θεσσαλονίκη → Αθήνα with a
			// 186,6 km line drawn through Thrace, and that line would have been
			// stored and priced.
			return fail(http.StatusBadRequest, "outside_service_area",
				"one of those places is outside the area this app can route yet")
		}
		if err != nil {
			return fail(http.StatusBadGateway, "routing_failed",
				"could not compute a driving route between those points")
		}
		shaped = &route
	}

	vehicle, err := d.chooseVehicle(r.Context(), userID, body.VehicleID)
	if errors.Is(err, drivers.ErrNoVehicle) {
		return fail(http.StatusBadRequest, "no_vehicle",
			"tell us what you drive before publishing a route")
	}
	if err != nil {
		return fail(http.StatusInternalServerError, "internal", "could not read your cars")
	}

	params := store.CreateRouteTemplateParams{
		DriverID:          userID,
		VehicleID:         vehicle.ID,
		Origin:            origin,
		Destination:       destination,
		OriginLabel:       originLabel,
		DestinationLabel:  destinationLabel,
		Seats:             seats,
		PricingMode:       mode,
		OSRMDataVersion:   d.OSRMDataVersion,
		Detour:            detour,
		Luggage:           luggage,
		LuggageCapacity:   capacity,
		ContributionShare: share,
		GroupID:           groupID,
		Schedule:          schedule,
	}
	if shaped != nil {
		params.RouteWKT = shaped.WKT()
		params.RouteKm = shaped.DistanceMeters / 1000
		params.RouteMin = int(shaped.Duration.Minutes()) + 1
		params.EndsAtPort = shaped.SeaCrossing
	}
	return params, true
}

// schedule converts the wire payload into a domain schedule.
func (b routeBody) schedule(today domain.Date) (domain.Schedule, error) {
	s := domain.Schedule{Kind: domain.ScheduleKind(b.Kind)}

	var err error
	if s.DepartEarliest, err = parseTimeOfDay(b.DepartEarliest); err != nil {
		return domain.Schedule{}, err
	}
	if s.DepartLatest, err = parseTimeOfDay(b.DepartLatest); err != nil {
		return domain.Schedule{}, err
	}

	switch s.Kind {
	case domain.ScheduleOneOff:
		d, err := parseDate(b.OneOffDate)
		if err != nil {
			return domain.Schedule{}, err
		}
		s.OneOffDate = &d
		s.EffectiveFrom = d
	case domain.ScheduleRecurring:
		if len(b.Weekdays) == 0 {
			return domain.Schedule{}, errInvalid("weekdays is required for a recurring route")
		}
		for _, iso := range uniqueSorted(b.Weekdays) {
			if iso < 1 || iso > 7 {
				return domain.Schedule{}, errInvalid("weekdays must be ISO numbers 1..7")
			}
			s.Weekdays = append(s.Weekdays, isoWeekday(iso))
		}
		s.EffectiveFrom = today
	default:
		return domain.Schedule{}, errInvalid("kind must be oneoff or recurring")
	}

	for _, e := range b.Exceptions {
		d, err := parseDate(e)
		if err != nil {
			return domain.Schedule{}, err
		}
		s.Exceptions = append(s.Exceptions, d)
	}
	return s, nil
}

func parseTimeOfDay(s string) (domain.TimeOfDay, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return domain.TimeOfDay{}, errInvalid("departure times must be HH:MM, got " + s)
	}
	return domain.TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
}

func parseDate(s string) (domain.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return domain.Date{}, errInvalid("dates must be YYYY-MM-DD, got " + s)
	}
	return domain.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

func isoWeekday(iso int) time.Weekday {
	if iso == 7 {
		return time.Sunday
	}
	return time.Weekday(iso)
}

// today is the civil date in the service's timezone, by the injected clock,
// the same day the materializer plans from. It read the host's wall clock.
func (d Deps) today() domain.Date {
	n := d.now()
	if d.Location != nil {
		n = n.In(d.Location)
	}
	return domain.Date{Year: n.Year(), Month: n.Month(), Day: n.Day()}
}

// errInvalid is a plain error whose text is safe to show a client.
type errInvalid string

func (e errInvalid) Error() string { return string(e) }

// The widest detour the service will store, whatever a client sends. Fifty
// kilometres is already a long way to go for somebody else's convenience, and
// half an hour is the cap the schema had before any of this was per-trip.
const (
	maxDetourKm  = 50.0
	maxDetourMin = 30
)

// detourFor resolves what the driver agreed to for this route, filling in from
// their profile whatever the request did not say.
func (d Deps) detourFor(ctx context.Context, driverID uuid.UUID, body routeBody) (match.Budget, error) {
	var out match.Budget
	err := d.Pool.QueryRow(ctx,
		`SELECT default_allow_stops, default_detour_km, detour_budget_min
		   FROM driver_profiles WHERE user_id = $1`, driverID).
		Scan(&out.AllowStops, &out.Km, &out.Min)
	if err != nil {
		return match.Budget{}, err
	}

	if body.AllowStops != nil {
		out.AllowStops = *body.AllowStops
	}
	if body.DetourKm != nil {
		out.Km = *body.DetourKm
	}
	if body.DetourMin != nil {
		out.Min = *body.DetourMin
	}
	return out, nil
}

// chooseVehicle resolves which car a route goes out in.
//
// An id names one of the driver's own — the database would refuse somebody
// else's, but refusing it here gives a 400 rather than a 500. No id means their
// default, which is what a driver with one car sends and what the app sends
// until they have a second.
func (d Deps) chooseVehicle(ctx context.Context, driverID uuid.UUID, id string) (drivers.Vehicle, error) {
	if id == "" {
		return d.Drivers.DefaultVehicle(ctx, driverID)
	}
	vehicleID, err := uuid.Parse(id)
	if err != nil {
		return drivers.Vehicle{}, drivers.ErrNoVehicle
	}
	return d.Drivers.Vehicle(ctx, driverID, vehicleID)
}

// The boot, in items of whatever size the route states. Eight is a van with the
// seats down; three is a hatchback with room for the people in it. They match
// the CHECK on route_templates.luggage_capacity.
const (
	defaultLuggageCapacity = 3
	maxLuggageCapacity     = 8
)
