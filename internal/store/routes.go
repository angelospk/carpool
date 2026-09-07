package store

import (
	"carpool/internal/match"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
	"carpool/internal/geo"
)

// RouteTemplate is a driver's published route.
type RouteTemplate struct {
	ID               uuid.UUID
	DriverID         uuid.UUID
	Origin           geo.Point
	Destination      geo.Point
	OriginLabel      string
	DestinationLabel string
	// RouteGeometry is the OSRM-snapped line, encoded as WKT.
	RouteWKT        string
	RouteKm         float64
	RouteMin        int
	Seats           int
	PricingMode     domain.PricingMode
	OSRMDataVersion string
	Active          bool
	Revision        int
}

// CreateRouteTemplateParams is the input to CreateRouteTemplate.
type CreateRouteTemplateParams struct {
	DriverID uuid.UUID
	// VehicleID is which of the driver's cars this route goes out in. Not
	// optional: a route with no car cannot be priced, and the database enforces
	// both that it is present and that it belongs to this driver.
	VehicleID        uuid.UUID
	Origin           geo.Point
	Destination      geo.Point
	OriginLabel      string
	DestinationLabel string
	RouteWKT         string
	RouteKm          float64
	RouteMin         int
	Seats            int
	PricingMode      domain.PricingMode
	// EndsAtPort marks a route the router cut short at a ferry terminal.
	EndsAtPort bool
	// Detour is what the driver will go out of their way, for this route. It
	// lives here rather than on their profile because "today I have time" is a
	// statement about today.
	//
	// A struct rather than three loose fields, and not optional: a bool whose
	// zero value means "take nobody" is a caller that forgets one line and
	// publishes a route no passenger can ever be matched to, with nothing to
	// show for it in any log.
	Detour match.Budget
	// Luggage is the largest thing this route will carry. It lives on the route
	// rather than on the car for the same reason the detour budget does: "the
	// boot is full of my own things today" is a statement about today.
	Luggage domain.Luggage
	// LuggageCapacity is how many items of that size are free for passengers —
	// not how big the boot is. Nothing in the schema models what the driver is
	// bringing themselves, and the matcher subtracts only the luggage already
	// booked by others (internal/match/gross.go), so anything the driver puts
	// in the boot has to be out of this number before it is stored or the route
	// promises room it does not have.
	//
	// Zero is only meaningful alongside a Luggage of 'none', which the schema
	// enforces.
	LuggageCapacity int
	// ContributionShare is how much of the computed cost share this driver asks
	// for. The app caps; the driver chooses. Zero means they did not say, which
	// is read as all of it.
	ContributionShare float64
	OSRMDataVersion   string
	Schedule          domain.Schedule
}

// CreateRouteTemplate stores a template and its schedule in one transaction: a
// template without a schedule would never materialize and is not a valid state.
func CreateRouteTemplate(ctx context.Context, pool *pgxpool.Pool, p CreateRouteTemplateParams) (uuid.UUID, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var templateID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO route_templates (
			driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version, ends_at_port,
			allow_stops, detour_km, detour_min, luggage, luggage_capacity, contribution_share)
		VALUES ($1, $18,
			ST_SetSRID(ST_MakePoint($2, $3), 4326),
			ST_SetSRID(ST_MakePoint($4, $5), 4326),
			$6, $7,
			ST_GeomFromText($8, 4326), $9, $10, $11, $12, $13, $14, $15, $16, $17, $19::luggage_size, $20, $21)
		RETURNING id`,
		p.DriverID,
		p.Origin.Lon, p.Origin.Lat,
		p.Destination.Lon, p.Destination.Lat,
		p.OriginLabel, p.DestinationLabel,
		p.RouteWKT, p.RouteKm, p.RouteMin, p.Seats, string(p.PricingMode), p.OSRMDataVersion, p.EndsAtPort,
		p.Detour.AllowStops, p.Detour.Km, p.Detour.Min, p.VehicleID, luggageOr(p.Luggage), capacityOr(p.LuggageCapacity, p.Luggage), shareOr(p.ContributionShare),
	).Scan(&templateID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: inserting route template: %w", err)
	}

	if err := insertSchedule(ctx, tx, templateID, p.Schedule); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("store: commit: %w", err)
	}
	return templateID, nil
}

func insertSchedule(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, s domain.Schedule) error {
	var oneoff *string
	if s.OneOffDate != nil {
		d := s.OneOffDate.String()
		oneoff = &d
	}

	var weekdays []int
	if s.Kind == domain.ScheduleRecurring {
		weekdays = make([]int, 0, len(s.Weekdays))
		for _, wd := range s.Weekdays {
			weekdays = append(weekdays, isoWeekday(wd))
		}
	}

	exceptions := make([]string, 0, len(s.Exceptions))
	for _, e := range s.Exceptions {
		exceptions = append(exceptions, e.String())
	}

	var until *string
	if s.EffectiveUntil != nil {
		u := s.EffectiveUntil.String()
		until = &u
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO route_schedules (
			template_id, kind, oneoff_date, weekdays,
			depart_earliest, depart_latest, effective_from, effective_until, exceptions)
		VALUES ($1, $2, $3::date, $4::int[],
			make_time($5, $6, 0), make_time($7, $8, 0),
			$9::date, $10::date, $11::date[])`,
		templateID, string(s.Kind), oneoff, weekdays,
		s.DepartEarliest.Hour, s.DepartEarliest.Minute,
		s.DepartLatest.Hour, s.DepartLatest.Minute,
		s.EffectiveFrom.String(), until, exceptions)
	if err != nil {
		return fmt.Errorf("store: inserting schedule: %w", err)
	}
	return nil
}

// scheduleQuerier is satisfied by both a pool and a transaction, so the
// materializer can read a schedule inside the transaction that locked it.
type scheduleQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetSchedule loads the schedule attached to a template.
func GetSchedule(ctx context.Context, q scheduleQuerier, templateID uuid.UUID) (domain.Schedule, error) {
	var (
		kind       string
		oneoff     *string
		weekdays   []int32
		earliestH  int
		earliestM  int
		latestH    int
		latestM    int
		from       string
		until      *string
		exceptions []string
	)
	err := q.QueryRow(ctx, `
		SELECT kind, oneoff_date::text, weekdays,
		       EXTRACT(HOUR FROM depart_earliest)::int, EXTRACT(MINUTE FROM depart_earliest)::int,
		       EXTRACT(HOUR FROM depart_latest)::int,   EXTRACT(MINUTE FROM depart_latest)::int,
		       effective_from::text, effective_until::text,
		       ARRAY(SELECT unnest(exceptions)::text)
		FROM route_schedules WHERE template_id = $1`, templateID).Scan(
		&kind, &oneoff, &weekdays, &earliestH, &earliestM, &latestH, &latestM, &from, &until, &exceptions)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Schedule{}, ErrNotFound
	}
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("store: loading schedule: %w", err)
	}

	s := domain.Schedule{
		Kind:           domain.ScheduleKind(kind),
		DepartEarliest: domain.TimeOfDay{Hour: earliestH, Minute: earliestM},
		DepartLatest:   domain.TimeOfDay{Hour: latestH, Minute: latestM},
	}
	if s.EffectiveFrom, err = parseDate(from); err != nil {
		return domain.Schedule{}, err
	}
	if oneoff != nil {
		d, err := parseDate(*oneoff)
		if err != nil {
			return domain.Schedule{}, err
		}
		s.OneOffDate = &d
	}
	if until != nil {
		d, err := parseDate(*until)
		if err != nil {
			return domain.Schedule{}, err
		}
		s.EffectiveUntil = &d
	}
	for _, wd := range weekdays {
		s.Weekdays = append(s.Weekdays, fromISOWeekday(int(wd)))
	}
	for _, e := range exceptions {
		d, err := parseDate(e)
		if err != nil {
			return domain.Schedule{}, err
		}
		s.Exceptions = append(s.Exceptions, d)
	}
	return s, nil
}

// luggageOr defaults an unset allowance to a suitcase.
//
// That is what an ordinary boot takes, and it is what every route published
// before this column existed was implicitly offering. Defaulting to 'none'
// instead would make a caller that forgot one field publish a route refusing
// everybody, with nothing on screen to say so.
// capacityOr pairs an unstated capacity with whatever size ended up being
// stored.
//
// The two columns constrain each other — 'none' takes nothing, anything else
// takes at least one — so a caller that sets one and forgets the other would hit
// a CHECK rather than a sensible default. Defaulted here, beside luggageOr, so
// that the pairing is decided in one place instead of at every call site.
func capacityOr(capacity int, l domain.Luggage) int {
	if l == domain.LuggageNone {
		return 0
	}
	if capacity <= 0 {
		return defaultLuggageCapacity
	}
	return capacity
}

// defaultLuggageCapacity is an ordinary boot on a trip with three seats on
// offer. It matches the column default in migration 00026.
const defaultLuggageCapacity = 3

func luggageOr(l domain.Luggage) string {
	if !l.Valid() {
		return string(domain.LuggageSuitcase)
	}
	return string(l)
}

// shareOr defaults a share nobody set to all of it, matching domain.shareOf and
// the column's own default. A route published before drivers could choose was
// implicitly asking for the whole share.
func shareOr(share float64) float64 {
	if share <= 0 || share > 1 {
		return 1
	}
	return share
}
