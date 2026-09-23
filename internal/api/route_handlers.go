package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"carpool/internal/domain"
	"github.com/google/uuid"

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
	Seats            int     `json:"seats"`
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

	schedule, err := body.schedule()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Above the ceiling is the one answer the app does not accept. Below it is
	// entirely theirs.
	share := 1.0
	if body.ContributionShare != nil {
		share = *body.ContributionShare
		if !(share > 0) || share > 1 {
			writeError(w, http.StatusBadRequest, "invalid_share",
				"you can ask for up to the whole share of the fuel, not more")
			return
		}
	}

	// Checked here, with the rest of the body, rather than after the route has
	// been snapped: a typo in one field should not cost an OSRM call.
	luggage := domain.LuggageSuitcase
	if body.Luggage != "" {
		parsed, err := domain.ParseLuggage(body.Luggage)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_luggage",
				"that is not a luggage size this app knows")
			return
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
		writeError(w, http.StatusBadRequest, "invalid_luggage",
			"say how many bags fit, between one and eight, or say the boot is full")
		return
	}
	if body.Seats <= 0 {
		body.Seats = 3 // the wizard's default
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
		writeError(w, http.StatusBadRequest, "invalid_pricing_mode",
			"a route either suggests a share of the fuel, leaves the contribution open, or asks for nothing")
		return
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
				writeError(w, http.StatusInternalServerError, "internal", "could not check the group")
				return
			}
		}
		if !member {
			writeError(w, http.StatusForbidden, "not_in_group",
				"you can offer a route only to a group you belong to")
			return
		}
		groupID = &id
	}

	// A driver profile is what makes someone publishable; without one there is
	// no car, no detour budget and no fuel figures to price with.
	var hasProfile bool
	if err := d.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM driver_profiles WHERE user_id = $1)`, userID).Scan(&hasProfile); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not check the driver profile")
		return
	}
	if !hasProfile {
		writeError(w, http.StatusConflict, "no_driver_profile", "add your car details before publishing a route")
		return
	}

	// What this driver will go out of their way, for this trip. Their profile
	// holds the defaults; the answer that binds is the one stored on the route
	// and snapshotted onto every occurrence of it.
	detour, err := d.detourFor(r.Context(), userID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your driver profile")
		return
	}
	if detour.Km < 0 || detour.Km > maxDetourKm || detour.Min < 0 || detour.Min > maxDetourMin {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"the detour you will accept is outside the range this service allows")
		return
	}

	// The same check the request side makes. A route's labels are read by every
	// passenger it is offered to, and they arrive from the client with nothing
	// but the body size limit above them.
	originLabel, err := domain.NormalisePlaceLabel(body.OriginLabel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"the start needs a name somebody can read")
		return
	}
	destinationLabel, err := domain.NormalisePlaceLabel(body.DestinationLabel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"the destination needs a name somebody can read")
		return
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
	for _, end := range []struct {
		what string
		at   geo.Point
	}{
		{"start", origin},
		{"destination", destination},
	} {
		_, err := store.RegionAt(r.Context(), d.Pool, end.at.Lon, end.at.Lat)
		if errors.Is(err, store.ErrOutsideGreece) {
			writeError(w, http.StatusBadRequest, "outside_country",
				"the "+end.what+" is outside Greece, which is as far as this app goes")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not locate the route")
			return
		}
	}

	// Snap the route to the road network now: the stored geometry is what the
	// gross match locates pickups along, so it must be a real road line rather
	// than the straight line between two pins.
	ctx, cancel := contextWithRoutingDeadline(r)
	defer cancel()

	shaped, err := d.Router.RouteWithShape(ctx, []geo.Point{origin, destination})
	if errors.Is(err, geo.ErrSeaCrossingOnly) {
		// Somewhere you sail to from a place you sail from. There is nothing to
		// share a car for, and quoting a trip of no kilometres would be worse
		// than saying so.
		writeError(w, http.StatusBadRequest, "sea_crossing",
			"that journey is a ferry crossing, so there is no drive to share")
		return
	}
	if errors.Is(err, geo.ErrNoSegment) {
		// A point too far from any road the routing extract holds. That is a
		// fact about where the trip goes, not a failure of this service, and it
		// is the difference between a fabricated route and none: without the
		// snapping bound OSRM answered Ok for Θεσσαλονίκη → Αθήνα with a
		// 186,6 km line drawn through Thrace, and that line would have been
		// stored and priced.
		writeError(w, http.StatusBadRequest, "outside_service_area",
			"one of those places is outside the area this app can route yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "routing_failed",
			"could not compute a driving route between those points")
		return
	}

	vehicle, err := d.chooseVehicle(r.Context(), userID, body.VehicleID)
	if errors.Is(err, drivers.ErrNoVehicle) {
		writeError(w, http.StatusBadRequest, "no_vehicle",
			"tell us what you drive before publishing a route")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your cars")
		return
	}

	templateID, err := store.CreateRouteTemplate(r.Context(), d.Pool, store.CreateRouteTemplateParams{
		DriverID:          userID,
		VehicleID:         vehicle.ID,
		Origin:            origin,
		Destination:       destination,
		OriginLabel:       originLabel,
		DestinationLabel:  destinationLabel,
		RouteWKT:          shaped.WKT(),
		RouteKm:           shaped.DistanceMeters / 1000,
		RouteMin:          int(shaped.Duration.Minutes()) + 1,
		Seats:             body.Seats,
		PricingMode:       mode,
		OSRMDataVersion:   d.OSRMDataVersion,
		EndsAtPort:        shaped.SeaCrossing,
		Detour:            detour,
		Luggage:           luggage,
		LuggageCapacity:   capacity,
		ContributionShare: share,
		GroupID:           groupID,
		Schedule:          schedule,
	})
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
				"ends_at_port": shaped.SeaCrossing,
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"template_id": templateID,
		"route_km":    shaped.DistanceMeters / 1000,
		// The app needs this to explain a route that stops somewhere the driver
		// did not type. Without it the trip just looks wrong.
		"ends_at_port": shaped.SeaCrossing,
		"materialized": true,
	})
}

// schedule converts the wire payload into a domain schedule.
func (b routeBody) schedule() (domain.Schedule, error) {
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
		for _, iso := range b.Weekdays {
			if iso < 1 || iso > 7 {
				return domain.Schedule{}, errInvalid("weekdays must be ISO numbers 1..7")
			}
			s.Weekdays = append(s.Weekdays, isoWeekday(iso))
		}
		s.EffectiveFrom = today()
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

func today() domain.Date {
	n := time.Now()
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
