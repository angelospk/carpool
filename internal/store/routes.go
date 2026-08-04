package store

import (
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
	DriverID         uuid.UUID
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
	EndsAtPort      bool
	OSRMDataVersion string
	Schedule        domain.Schedule
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
			driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version, ends_at_port)
		VALUES ($1,
			ST_SetSRID(ST_MakePoint($2, $3), 4326),
			ST_SetSRID(ST_MakePoint($4, $5), 4326),
			$6, $7,
			ST_GeomFromText($8, 4326), $9, $10, $11, $12, $13, $14)
		RETURNING id`,
		p.DriverID,
		p.Origin.Lon, p.Origin.Lat,
		p.Destination.Lon, p.Destination.Lat,
		p.OriginLabel, p.DestinationLabel,
		p.RouteWKT, p.RouteKm, p.RouteMin, p.Seats, string(p.PricingMode), p.OSRMDataVersion, p.EndsAtPort,
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
