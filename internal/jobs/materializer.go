// Package jobs holds the background workers: the occurrence materializer, the
// ask-timeout sweeper and cleanup tasks.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/domain"
	"carpool/internal/store"
)

// HorizonDays is the rolling window the materializer keeps filled. It is
// expressed in calendar days rather than as a duration because "now + 21×24h"
// drifts by an hour across a DST transition.
const HorizonDays = 21

// TemplateLockNamespace namespaces the per-template advisory lock. The
// two-argument advisory lock form takes a pair of int4s, so the namespace is
// int32 and the template uuid is hashed into the second slot.
//
// It is exported because anything that reserves seats on a template's
// occurrences has to serialise against materialization, or it counts weeks that
// change underneath it.
const TemplateLockNamespace int32 = 0x6361_7270 // "carp"

// Materializer expands route schedules into concrete trip occurrences.
type Materializer struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	loc   *time.Location
}

// NewMaterializer returns a materializer working in loc, which is
// Europe/Athens in production.
func NewMaterializer(pool *pgxpool.Pool, c clock.Clock, loc *time.Location) *Materializer {
	return &Materializer{pool: pool, clock: c, loc: loc}
}

// Run reconciles every active template's occurrences with its schedule.
func (m *Materializer) Run(ctx context.Context) error {
	rows, err := m.pool.Query(ctx,
		`SELECT id FROM route_templates WHERE active ORDER BY id`)
	if err != nil {
		return fmt.Errorf("jobs: listing templates: %w", err)
	}

	var templateIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("jobs: scanning template id: %w", err)
		}
		templateIDs = append(templateIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("jobs: reading templates: %w", err)
	}

	for _, id := range templateIDs {
		if err := m.reconcile(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// reconcile brings one template's occurrences in line with its schedule.
//
// Everything the decision depends on — the schedule, the template snapshot and
// the horizon — is read *after* the advisory lock is taken and inside the same
// transaction. Reading before the lock let a concurrent schedule edit, or a run
// that started before midnight, reconcile against a stale plan and cancel rows
// a newer run had just created.
//
// An occurrence is mutable only while nothing depends on it: no live booking,
// no pending ask, and it has not departed. Mutable rows are created, retimed or
// cancelled freely. Everything else is left exactly as it is, because rewriting
// it would change an arrangement two people already made.
func (m *Materializer) reconcile(ctx context.Context, templateID uuid.UUID) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("jobs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise against any other materializer working on this template before
	// reading anything it might be changing.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		TemplateLockNamespace, templateID.String()); err != nil {
		return fmt.Errorf("jobs: taking template lock: %w", err)
	}

	// Lock the template row so its revision cannot change mid-reconciliation.
	// Under READ COMMITTED each statement takes a fresh snapshot, so without
	// this a single run could commit a horizon spanning two revisions.
	var tmpl templateSnapshot
	err = tx.QueryRow(ctx, `
		SELECT revision, seats, route_km, route_min, pricing_mode, ends_at_port,
		       allow_stops, detour_km, detour_min, ST_AsText(route_geom)
		FROM route_templates WHERE id = $1 AND active
		FOR UPDATE`, templateID).Scan(
		&tmpl.revision, &tmpl.seats, &tmpl.routeKm, &tmpl.routeMin, &tmpl.pricingMode, &tmpl.endsAtPort,
		&tmpl.allowStops, &tmpl.detourKm, &tmpl.detourMin, &tmpl.routeWKT)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deactivated between listing and locking
	}
	if err != nil {
		return fmt.Errorf("jobs: loading template: %w", err)
	}

	schedule, err := store.GetSchedule(ctx, tx, templateID)
	if err != nil {
		return err
	}

	now := m.clock.Now()
	local := now.In(m.loc)
	from := domain.Date{Year: local.Year(), Month: local.Month(), Day: local.Day()}
	to := from.AddDays(HorizonDays)

	planned, planErrs := domain.Plan(schedule, m.loc, from, to)
	for _, err := range planErrs {
		// A skipped local time is a data condition, not a failure: record it and
		// carry on with the dates that do exist.
		slog.Warn("skipping an unmaterializable occurrence", "template", templateID, "error", err)
	}

	if err := m.upsert(ctx, tx, templateID, tmpl, planned, now); err != nil {
		return err
	}
	if err := m.cancelUnwanted(ctx, tx, templateID, planned, from, to, now); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("jobs: commit: %w", err)
	}
	return nil
}

// templateSnapshot is the template state copied onto each occurrence.
type templateSnapshot struct {
	revision    int
	seats       int
	routeKm     float64
	routeMin    int
	pricingMode string
	endsAtPort  bool
	allowStops  bool
	detourKm    float64
	detourMin   int
	routeWKT    string
}

// upsert creates the occurrences the schedule wants, and brings mutable
// existing ones back in line — including reviving one that a lifted exception
// had previously cancelled.
func (m *Materializer) upsert(
	ctx context.Context, tx pgx.Tx, templateID uuid.UUID,
	tmpl templateSnapshot, planned []domain.PlannedOccurrence, now time.Time,
) error {
	for _, p := range planned {
		_, err := tx.Exec(ctx, `
			INSERT INTO trip_occurrences (
				template_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision,
				route_geom, route_km, route_min, pricing_mode, ends_at_port,
				allow_stops, detour_km, detour_min)
			VALUES ($1, $2::date, $3, $4,
			        $5, $5, $6, ST_GeomFromText($7, 4326), $8, $9, $10, $12, $13, $14, $15)
			ON CONFLICT (template_id, service_date) DO UPDATE
			SET depart_earliest   = EXCLUDED.depart_earliest,
			    depart_latest     = EXCLUDED.depart_latest,
			    status            = 'scheduled',
			    seats_total       = EXCLUDED.seats_total,
			    -- Preserve seats already taken; only the unsold remainder moves.
			    seats_free        = EXCLUDED.seats_total
			                        - (trip_occurrences.seats_total - trip_occurrences.seats_free),
			    template_revision = EXCLUDED.template_revision,
			    route_geom        = EXCLUDED.route_geom,
			    route_km          = EXCLUDED.route_km,
			    route_min         = EXCLUDED.route_min,
			    pricing_mode      = EXCLUDED.pricing_mode,
			    ends_at_port      = EXCLUDED.ends_at_port,
			    allow_stops       = EXCLUDED.allow_stops,
			    detour_km         = EXCLUDED.detour_km,
			    detour_min        = EXCLUDED.detour_min,
			    updated_at        = now()
			WHERE `+mutablePredicate("$11"),
			templateID, p.ServiceDate.String(), p.DepartEarliest, p.DepartLatest,
			tmpl.seats, tmpl.revision, tmpl.routeWKT, tmpl.routeKm, tmpl.routeMin, tmpl.pricingMode,
			now, tmpl.endsAtPort, tmpl.allowStops, tmpl.detourKm, tmpl.detourMin)
		if err != nil {
			return fmt.Errorf("jobs: upserting occurrence %s: %w", p.ServiceDate, err)
		}
	}
	return nil
}

// mutablePredicate is the shared definition of "nothing depends on this yet":
// it has not departed, nobody has booked it, and no driver is being asked about
// it right now.
//
// The departure comparison takes the instant as a parameter rather than calling
// now(). SQL's now() is the database's wall clock, while every other decision
// here comes from the injected clock; mixing the two makes the rules depend on
// which clock happens to be ahead.
func mutablePredicate(nowParam string) string {
	return `
	trip_occurrences.depart_earliest > ` + nowParam + `
	AND NOT EXISTS (
	      SELECT 1 FROM bookings b
	      WHERE b.occurrence_id = trip_occurrences.id
	        AND b.status IN ('confirmed','completed'))
	AND NOT EXISTS (
	      SELECT 1 FROM asks a
	      WHERE a.occurrence_id = trip_occurrences.id
	        AND a.status = 'pending')`
}

// cancelUnwanted cancels mutable occurrences inside the horizon that the
// schedule no longer wants.
//
// The upper bound matters as much as the lower one: occurrences beyond the
// rolling horizon are simply not in scope yet, not unwanted.
func (m *Materializer) cancelUnwanted(
	ctx context.Context, tx pgx.Tx, templateID uuid.UUID,
	planned []domain.PlannedOccurrence, from, to domain.Date, now time.Time,
) error {
	keep := make([]string, 0, len(planned))
	for _, p := range planned {
		keep = append(keep, p.ServiceDate.String())
	}

	// Cancelled rather than deleted: asks and bookings reference occurrences,
	// and a tombstone keeps that history intact. A later run can revive it.
	_, err := tx.Exec(ctx, `
		UPDATE trip_occurrences
		SET status = 'cancelled', updated_at = now()
		WHERE trip_occurrences.template_id = $1
		  AND trip_occurrences.status = 'scheduled'
		  AND trip_occurrences.service_date >= $2::date
		  AND trip_occurrences.service_date <  $3::date
		  AND NOT (trip_occurrences.service_date = ANY($4::date[]))
		  AND `+mutablePredicate("$5"),
		templateID, from.String(), to.String(), keep, now)
	if err != nil {
		return fmt.Errorf("jobs: cancelling superseded occurrences: %w", err)
	}
	return nil
}
