// Package jobs holds the background workers: the occurrence materializer, the
// ask-timeout sweeper and cleanup tasks.
package jobs

import (
	"context"
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

// materializeLockNamespace namespaces the per-template advisory lock. The
// two-argument advisory lock form takes a pair of int4s, so the namespace is
// int32 and the template uuid is hashed into the second slot.
const materializeLockNamespace int32 = 0x6361_7270 // "carp"

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
// The desired set is computed from the schedule, then compared with what
// exists. Occurrences are only created or cancelled when nothing depends on
// them: an occurrence that is booked, carries a live ask, has already departed
// or is not scheduled any more is frozen. Rewriting one of those would change
// the meaning of an arrangement two people have already made.
func (m *Materializer) reconcile(ctx context.Context, templateID uuid.UUID) error {
	schedule, err := store.GetSchedule(ctx, m.pool, templateID)
	if err != nil {
		return err
	}

	now := m.clock.Now().In(m.loc)
	from := domain.Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
	to := from.AddDays(HorizonDays)

	planned, planErrs := domain.Plan(schedule, m.loc, from, to)
	for _, err := range planErrs {
		// A skipped local time is a data condition, not a failure: record it and
		// carry on with the dates that do exist.
		slog.Warn("skipping an unmaterializable occurrence", "template", templateID, "error", err)
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("jobs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise against any other materializer working on this template. Two
	// runners would otherwise both see "missing" and both insert.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		materializeLockNamespace, templateID.String()); err != nil {
		return fmt.Errorf("jobs: taking template lock: %w", err)
	}

	wanted := make(map[string]domain.PlannedOccurrence, len(planned))
	for _, p := range planned {
		wanted[p.ServiceDate.String()] = p
	}

	if err := m.insertMissing(ctx, tx, templateID, planned); err != nil {
		return err
	}
	if err := m.cancelUnwanted(ctx, tx, templateID, wanted, from); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("jobs: commit: %w", err)
	}
	return nil
}

// insertMissing creates occurrences that the schedule wants and that do not
// exist yet. Existing rows are left untouched, which is what keeps a booked or
// asked-about occurrence stable.
func (m *Materializer) insertMissing(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, planned []domain.PlannedOccurrence) error {
	for _, p := range planned {
		_, err := tx.Exec(ctx, `
			INSERT INTO trip_occurrences (
				template_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision,
				route_geom, route_km, route_min, pricing_mode)
			SELECT t.id, $2::date, $3, $4,
			       t.seats, t.seats, t.revision,
			       t.route_geom, t.route_km, t.route_min, t.pricing_mode
			FROM route_templates t
			WHERE t.id = $1
			ON CONFLICT (template_id, service_date) DO NOTHING`,
			templateID, p.ServiceDate.String(), p.DepartEarliest, p.DepartLatest)
		if err != nil {
			return fmt.Errorf("jobs: inserting occurrence %s: %w", p.ServiceDate, err)
		}
	}
	return nil
}

// cancelUnwanted cancels future occurrences the schedule no longer wants, but
// only those nothing depends on.
func (m *Materializer) cancelUnwanted(
	ctx context.Context, tx pgx.Tx, templateID uuid.UUID,
	wanted map[string]domain.PlannedOccurrence, from domain.Date,
) error {
	keep := make([]string, 0, len(wanted))
	for date := range wanted {
		keep = append(keep, date)
	}

	// Cancelled rather than deleted: asks and bookings reference occurrences,
	// and a tombstone keeps that history intact.
	_, err := tx.Exec(ctx, `
		UPDATE trip_occurrences o
		SET status = 'cancelled', updated_at = now()
		WHERE o.template_id = $1
		  AND o.status = 'scheduled'
		  AND o.service_date >= $2::date
		  AND NOT (o.service_date = ANY($3::date[]))
		  AND NOT EXISTS (
		        SELECT 1 FROM bookings b
		        WHERE b.occurrence_id = o.id
		          AND b.status IN ('confirmed','completed'))
		  AND NOT EXISTS (
		        SELECT 1 FROM asks a
		        WHERE a.occurrence_id = o.id
		          AND a.status = 'pending')`,
		templateID, from.String(), keep)
	if err != nil {
		return fmt.Errorf("jobs: cancelling superseded occurrences: %w", err)
	}
	return nil
}
