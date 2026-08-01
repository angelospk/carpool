// Package standing records an arrangement two people already made: this
// passenger rides this driver's route every Tuesday, until a date.
//
// It exists because the agreement is free but its execution is not. Two people
// can settle "same time next week" in the car without any app; what they cannot
// do is stop the matcher from handing that seat to a stranger in the meantime.
// Approving an arrangement books the weeks outright, which both holds the seats
// and freezes those occurrences against template edits.
package standing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/jobs"
	"carpool/internal/outbox"
)

// ErrNotYours means the caller is not the side of the arrangement that may do
// this: only the passenger repeats a ride, only the driver approves one.
var ErrNotYours = errors.New("standing: not yours")

// ErrNotRepeatable means the booking cannot found an arrangement — it is
// cancelled, or the trip it belongs to has already gone.
var ErrNotRepeatable = errors.New("standing: that ride cannot be repeated")

// ErrWeekFull reports the dates an approval could not reserve.
//
// Approval is all or nothing. Booking three Tuesdays out of four and saying
// nothing would hand back a different agreement from the one the driver was
// shown, and neither side would learn which week went missing until someone was
// left standing at a bus stop.
type ErrWeekFull struct{ Dates []string }

func (e ErrWeekFull) Error() string {
	return "standing: no seat on " + strings.Join(e.Dates, ", ")
}

// Service creates, approves and cancels standing arrangements.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Repeat turns a booking the passenger holds into a pending arrangement for the
// same weekday, and asks the driver once.
//
// Everything material is copied from the ride they already took: the same stops,
// the same seats, and the same contribution. Recomputing the price week by week
// would move a term of the agreement that neither side revisited.
func (s *Service) Repeat(ctx context.Context, bookingID, passengerID uuid.UUID) (uuid.UUID, error) {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("standing: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		templateID, owner, driverID uuid.UUID
		serviceDate                 time.Time
		seats                       int
		price                       float64
		status                      string
	)
	err = tx.QueryRow(ctx, `
		SELECT o.template_id, b.passenger_id, t.driver_id, o.service_date,
		       b.seats, b.price_eur, b.status
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates  t ON t.id = o.template_id
		WHERE b.id = $1`, bookingID).
		Scan(&templateID, &owner, &driverID, &serviceDate, &seats, &price, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotRepeatable
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("standing: loading the booking: %w", err)
	}
	if owner != passengerID {
		return uuid.Nil, ErrNotYours
	}
	if status != "confirmed" && status != "completed" {
		return uuid.Nil, ErrNotRepeatable
	}

	// The weekday comes from the Athens calendar date the occurrence is keyed
	// on, never from a departure instant: daylight saving moves the instant, not
	// the day people mean when they say "every Tuesday".
	weekday := int(serviceDate.Weekday())
	if weekday == 0 {
		weekday = 7 // ISO: Sunday is 7, and Go calls it 0.
	}

	// An arrangement reaches as far as the calendar the system actually keeps.
	// Materialization runs on a rolling horizon, so a longer promise would be a
	// promise about occurrences that do not exist and cannot be reserved.
	until := now.AddDate(0, 0, jobs.HorizonDays)

	// A second tap is the same agreement, so the conflict is absorbed rather
	// than raised. Letting the unique index reject the insert would abort the
	// transaction, and nothing after it — including the lookup of the row that
	// caused the conflict — could run.
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO standing_arrangements
		(template_id, passenger_id, weekday, seats, pickup_point, dropoff_point,
		 price_eur, until_date, source_booking_id)
		SELECT $1, $2, $3, b.seats, b.pickup_point, b.dropoff_point, b.price_eur, $4::date, b.id
		FROM bookings b WHERE b.id = $5
		ON CONFLICT (template_id, passenger_id, weekday)
		WHERE status IN ('pending','active') DO NOTHING
		RETURNING id`, templateID, passengerID, weekday, until, bookingID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := s.live(ctx, tx, templateID, passengerID, weekday)
		if lookupErr != nil {
			return uuid.Nil, fmt.Errorf("standing: finding the arrangement already standing: %w", lookupErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("standing: commit: %w", err)
		}
		return existing, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("standing: recording the arrangement: %w", err)
	}

	if err := s.notify(ctx, tx, "arrangement_requested", driverID, id, now); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("standing: commit: %w", err)
	}
	return id, nil
}

// live finds the arrangement already standing for this pair and weekday.
func (s *Service) live(
	ctx context.Context, tx pgx.Tx, templateID, passengerID uuid.UUID, weekday int,
) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM standing_arrangements
		WHERE template_id = $1 AND passenger_id = $2 AND weekday = $3
		  AND status IN ('pending','active')`, templateID, passengerID, weekday).Scan(&id)
	return id, err
}

// Approve books every remaining week of the arrangement, or none of them.
func (s *Service) Approve(ctx context.Context, arrangementID, driverID uuid.UUID) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("standing: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		templateID, passengerID, owner uuid.UUID
		weekday, seats                 int
		price                          float64
		status                         string
		until                          time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT a.template_id, a.passenger_id, t.driver_id, a.weekday, a.seats,
		       a.price_eur, a.status, a.until_date
		FROM standing_arrangements a
		JOIN route_templates t ON t.id = a.template_id
		WHERE a.id = $1 FOR UPDATE OF a`, arrangementID).
		Scan(&templateID, &passengerID, &owner, &weekday, &seats, &price, &status, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotRepeatable
	}
	if err != nil {
		return fmt.Errorf("standing: loading the arrangement: %w", err)
	}
	if owner != driverID {
		return ErrNotYours
	}
	// Approving what is already approved is the same act, not a second one.
	if status == "active" {
		return nil
	}
	if status != "pending" {
		return ErrNotRepeatable
	}

	// Serialise against the materializer on this template, so an occurrence
	// cannot be created between the weeks being counted and the weeks being
	// booked.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		jobs.TemplateLockNamespace, templateID.String()); err != nil {
		return fmt.Errorf("standing: taking the template lock: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT o.id, o.service_date
		FROM trip_occurrences o
		WHERE o.template_id = $1
		  AND o.status = 'scheduled'
		  AND o.service_date <= $2::date
		  AND o.depart_earliest > $3
		  AND EXTRACT(ISODOW FROM o.service_date) = $4
		  AND NOT EXISTS (SELECT 1 FROM bookings b
		                  WHERE b.occurrence_id = o.id AND b.passenger_id = $5)
		ORDER BY o.service_date
		FOR UPDATE OF o`, templateID, until, now, weekday, passengerID)
	if err != nil {
		return fmt.Errorf("standing: finding the weeks: %w", err)
	}
	type week struct {
		id   uuid.UUID
		date time.Time
	}
	var weeks []week
	for rows.Next() {
		var wk week
		if err := rows.Scan(&wk.id, &wk.date); err != nil {
			rows.Close()
			return fmt.Errorf("standing: scanning a week: %w", err)
		}
		weeks = append(weeks, wk)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("standing: reading the weeks: %w", err)
	}

	var full []string
	for _, wk := range weeks {
		// Capacity is tested by the update itself. Letting the seats_free >= 0
		// CHECK fire instead would abort the whole transaction, which is exactly
		// what a per-week decision cannot afford.
		tag, err := tx.Exec(ctx, `
			UPDATE trip_occurrences
			SET seats_free = seats_free - $2, itinerary_version = itinerary_version + 1,
			    updated_at = now()
			WHERE id = $1 AND seats_free >= $2`, wk.id, seats)
		if err != nil {
			return fmt.Errorf("standing: reserving a seat: %w", err)
		}
		if tag.RowsAffected() == 0 {
			full = append(full, wk.date.Format("2006-01-02"))
			continue
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO bookings
			(occurrence_id, passenger_id, arrangement_id, seats, price_eur, pickup_point, dropoff_point)
			SELECT $1, a.passenger_id, a.id, a.seats, a.price_eur, a.pickup_point, a.dropoff_point
			FROM standing_arrangements a WHERE a.id = $2`, wk.id, arrangementID); err != nil {
			return fmt.Errorf("standing: booking a week: %w", err)
		}
	}
	if len(full) > 0 {
		// The rollback in the deferred call undoes every seat taken above.
		return ErrWeekFull{Dates: full}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE standing_arrangements SET status = 'active', updated_at = now() WHERE id = $1`,
		arrangementID); err != nil {
		return fmt.Errorf("standing: activating: %w", err)
	}
	if err := s.notify(ctx, tx, "arrangement_approved", passengerID, arrangementID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("standing: commit: %w", err)
	}
	return nil
}

// notify queues a push in the same transaction as the state it announces.
func (s *Service) notify(
	ctx context.Context, tx pgx.Tx, event string, recipient, arrangementID uuid.UUID, now time.Time,
) error {
	payload, err := json.Marshal(map[string]any{
		"event": event, "recipient_id": recipient, "arrangement_id": arrangementID,
	})
	if err != nil {
		return fmt.Errorf("standing: encoding the notification: %w", err)
	}
	return outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "push",
		Payload:        payload,
		IdempotencyKey: event + ":" + arrangementID.String(),
	}, now)
}
