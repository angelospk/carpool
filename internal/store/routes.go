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
	// GroupID, when set, offers the route to that group's members only. The
	// caller has already checked that the driver is one of them.
	GroupID *uuid.UUID
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

	if err := refuseDuplicate(ctx, tx, p, uuid.Nil); err != nil {
		return uuid.Nil, err
	}

	var templateID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO route_templates (
			driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version, ends_at_port,
			allow_stops, detour_km, detour_min, luggage, luggage_capacity, contribution_share, group_id)
		VALUES ($1, $18,
			ST_SetSRID(ST_MakePoint($2, $3), 4326),
			ST_SetSRID(ST_MakePoint($4, $5), 4326),
			$6, $7,
			ST_GeomFromText($8, 4326), $9, $10, $11, $12, $13, $14, $15, $16, $17, $19::luggage_size, $20, $21, $22)
		RETURNING id`,
		p.DriverID,
		p.Origin.Lon, p.Origin.Lat,
		p.Destination.Lon, p.Destination.Lat,
		p.OriginLabel, p.DestinationLabel,
		p.RouteWKT, p.RouteKm, p.RouteMin, p.Seats, string(p.PricingMode), p.OSRMDataVersion, p.EndsAtPort,
		p.Detour.AllowStops, p.Detour.Km, p.Detour.Min, p.VehicleID, luggageOr(p.Luggage), capacityOr(p.LuggageCapacity, p.Luggage), shareOr(p.ContributionShare),
		p.GroupID,
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

// ErrDuplicateRoute is returned when the driver already offers this route:
// the same two ends, days and hours.
var ErrDuplicateRoute = errors.New("store: the driver already offers this route")

// refuseDuplicate returns ErrDuplicateRoute if the driver has another active
// route, not except, with the same ends (within 50 m), schedule and hours. A second copy put two
// identical cards in the driver's list and two departures in every search
// (QA 2026-10-16).
//
// Under a lock per driver, held to the end of the transaction, because the
// usual way to publish twice is a double tap: two of these at once each found
// nothing and both inserted.
func refuseDuplicate(ctx context.Context, tx pgx.Tx, p CreateRouteTemplateParams, except uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('publish:' || $1::text, 0))`,
		p.DriverID); err != nil {
		return fmt.Errorf("store: taking the publish lock: %w", err)
	}

	sc := scheduleColumns(p.Schedule)
	weekdays := sc.weekdays
	if weekdays == nil {
		weekdays = []int{}
	}

	var dup bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM route_templates t
		  JOIN route_schedules rs ON rs.template_id = t.id
		 WHERE t.driver_id = $1 AND t.active AND t.id <> $13
		   AND ST_DWithin(t.origin::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 50)
		   AND ST_DWithin(t.destination::geography, ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography, 50)
		   AND rs.kind = $6
		   AND rs.oneoff_date IS NOT DISTINCT FROM $7::date
		   AND ARRAY(SELECT DISTINCT unnest(COALESCE(rs.weekdays, '{}')) ORDER BY 1)
		       = ARRAY(SELECT DISTINCT unnest($8::int[]) ORDER BY 1)
		   AND rs.depart_earliest = make_time($9, $10, 0)
		   AND rs.depart_latest = make_time($11, $12, 0))`,
		p.DriverID, p.Origin.Lon, p.Origin.Lat, p.Destination.Lon, p.Destination.Lat,
		string(p.Schedule.Kind), sc.oneoff, weekdays,
		p.Schedule.DepartEarliest.Hour, p.Schedule.DepartEarliest.Minute,
		p.Schedule.DepartLatest.Hour, p.Schedule.DepartLatest.Minute, except).Scan(&dup)
	if err != nil {
		return fmt.Errorf("store: looking for the same route: %w", err)
	}
	if dup {
		return ErrDuplicateRoute
	}
	return nil
}

// ErrRouteEndsChanged is returned when an edit moves either end of a route.
// That is a different journey, published as a new route: the departures
// already booked were agreed for these two places.
var ErrRouteEndsChanged = errors.New("store: an edit cannot move the ends of a route")

// UpdateRouteTemplate changes a route in place: its schedule, seats, car,
// price, luggage, detour and audience. The ends and the road line stay.
//
// In place, and not as a new route that retires the old one, because the
// materializer already keeps what was agreed: a departure somebody has a seat
// on or is being asked about keeps its time, seats and price (see
// mutablePredicate in internal/jobs). Every other departure takes the new
// terms at the next run. As two routes, the booked morning kept the old
// departure and the new route added a second one at the new time, so the
// driver offered two cars where they drive one (QA 2026-10-16).
//
// p.Origin and p.Destination must be within 50 m of what is stored, or it
// returns ErrRouteEndsChanged. A change that matches another of the driver's
// routes is ErrDuplicateRoute. A route that is not the driver's, or is
// retired, is ErrNotFound.
func UpdateRouteTemplate(ctx context.Context, pool *pgxpool.Pool, templateID uuid.UUID, p CreateRouteTemplateParams) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// An edit can make a route the copy of another one, too.
	if err := refuseDuplicate(ctx, tx, p, templateID); err != nil {
		return err
	}

	var sameEnds bool
	err = tx.QueryRow(ctx, `
		SELECT ST_DWithin(origin::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, 50)
		   AND ST_DWithin(destination::geography, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, 50)
		  FROM route_templates
		 WHERE id = $1 AND driver_id = $2 AND active
		 FOR UPDATE`,
		templateID, p.DriverID, p.Origin.Lon, p.Origin.Lat, p.Destination.Lon, p.Destination.Lat).Scan(&sameEnds)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: locking route template: %w", err)
	}
	if !sameEnds {
		return ErrRouteEndsChanged
	}

	if _, err := tx.Exec(ctx, `
		UPDATE route_templates
		   SET vehicle_id = $2, origin_label = $3, destination_label = $4,
		       seats = $5, pricing_mode = $6, allow_stops = $7, detour_km = $8, detour_min = $9,
		       luggage = $10::luggage_size, luggage_capacity = $11, contribution_share = $12,
		       group_id = $13, revision = revision + 1, updated_at = now()
		 WHERE id = $1`,
		templateID, p.VehicleID, p.OriginLabel, p.DestinationLabel,
		p.Seats, string(p.PricingMode), p.Detour.AllowStops, p.Detour.Km, p.Detour.Min,
		luggageOr(p.Luggage), capacityOr(p.LuggageCapacity, p.Luggage), shareOr(p.ContributionShare),
		p.GroupID); err != nil {
		return fmt.Errorf("store: updating route template: %w", err)
	}
	// The days the driver called off stay off. Each is an exception in the
	// schedule, and the form sends none: replacing them put every called-off
	// departure back at the next run. One statement, so an exception a
	// cancellation adds while this waits for the row is kept as well.
	sc := scheduleColumns(p.Schedule)
	if _, err := tx.Exec(ctx, `
		UPDATE route_schedules
		   SET kind = $2, oneoff_date = $3::date, weekdays = $4::int[],
		       depart_earliest = make_time($5, $6, 0), depart_latest = make_time($7, $8, 0),
		       effective_from = $9::date, effective_until = $10::date,
		       exceptions = ARRAY(SELECT DISTINCT unnest(exceptions || $11::date[]) ORDER BY 1),
		       revision = revision + 1, updated_at = now()
		 WHERE template_id = $1`,
		templateID, string(p.Schedule.Kind), sc.oneoff, sc.weekdays,
		p.Schedule.DepartEarliest.Hour, p.Schedule.DepartEarliest.Minute,
		p.Schedule.DepartLatest.Hour, p.Schedule.DepartLatest.Minute,
		p.Schedule.EffectiveFrom.String(), sc.until, sc.exceptions); err != nil {
		return fmt.Errorf("store: updating schedule: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// scheduleRow is a schedule in the shape its columns take.
type scheduleRow struct {
	oneoff     *string
	weekdays   []int
	until      *string
	exceptions []string
}

func scheduleColumns(s domain.Schedule) scheduleRow {
	var out scheduleRow
	if s.OneOffDate != nil {
		d := s.OneOffDate.String()
		out.oneoff = &d
	}
	if s.Kind == domain.ScheduleRecurring {
		out.weekdays = make([]int, 0, len(s.Weekdays))
		for _, wd := range s.Weekdays {
			out.weekdays = append(out.weekdays, isoWeekday(wd))
		}
	}
	out.exceptions = make([]string, 0, len(s.Exceptions))
	for _, e := range s.Exceptions {
		out.exceptions = append(out.exceptions, e.String())
	}
	if s.EffectiveUntil != nil {
		u := s.EffectiveUntil.String()
		out.until = &u
	}
	return out
}

func insertSchedule(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, s domain.Schedule) error {
	sc := scheduleColumns(s)
	_, err := tx.Exec(ctx, `
		INSERT INTO route_schedules (
			template_id, kind, oneoff_date, weekdays,
			depart_earliest, depart_latest, effective_from, effective_until, exceptions)
		VALUES ($1, $2, $3::date, $4::int[],
			make_time($5, $6, 0), make_time($7, $8, 0),
			$9::date, $10::date, $11::date[])`,
		templateID, string(s.Kind), sc.oneoff, sc.weekdays,
		s.DepartEarliest.Hour, s.DepartEarliest.Minute,
		s.DepartLatest.Hour, s.DepartLatest.Minute,
		s.EffectiveFrom.String(), sc.until, sc.exceptions)
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
