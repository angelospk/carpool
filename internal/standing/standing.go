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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/jobs"
	"carpool/internal/notices"
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
		 price_eur, pricing_basis, until_date, source_booking_id)
		SELECT $1, $2, $3, b.seats, b.pickup_point, b.dropoff_point, b.price_eur,
		       b.pricing_basis, $4::date, b.id
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

	if err := s.notify(ctx, tx, notices.ArrangementRequested, driverID, id, now); err != nil {
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
			(occurrence_id, passenger_id, arrangement_id, seats, price_eur, pricing_basis,
			 pickup_point, dropoff_point)
			SELECT $1, a.passenger_id, a.id, a.seats, a.price_eur, a.pricing_basis,
			       a.pickup_point, a.dropoff_point
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
	if err := s.notify(ctx, tx, notices.ArrangementApproved, passengerID, arrangementID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("standing: commit: %w", err)
	}
	return nil
}

// notify queues a push in the same transaction as the state it announces.
func (s *Service) notify(
	ctx context.Context, tx pgx.Tx, event notices.Event, recipient, arrangementID uuid.UUID, now time.Time,
) error {
	notice, err := notices.Arrangement(event, recipient, arrangementID)
	if err != nil {
		return fmt.Errorf("standing: %w", err)
	}
	return outbox.Enqueue(ctx, tx, notice, now)
}

// Decline ends a pending arrangement without touching a single week.
//
// It is not a permanent no: the row goes terminal, which frees the slot the
// partial unique index holds, so the passenger may ask again later.
func (s *Service) Decline(ctx context.Context, arrangementID, driverID uuid.UUID) error {
	return s.settle(ctx, arrangementID, driverID, "declined")
}

// Cancel ends an arrangement and releases every week it still holds. Either
// side may do it — a driver whose Tuesdays changed and a passenger who found
// another way to work are the same event to the seats.
func (s *Service) Cancel(ctx context.Context, arrangementID, userID uuid.UUID) error {
	return s.settle(ctx, arrangementID, userID, "cancelled")
}

// settle moves an arrangement to a terminal state and, for a cancellation,
// gives back the seats of every week that has not happened yet.
func (s *Service) settle(ctx context.Context, arrangementID, userID uuid.UUID, to string) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("standing: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var templateID, passengerID, driverID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `
		SELECT a.template_id, a.passenger_id, t.driver_id, a.status
		FROM standing_arrangements a
		JOIN route_templates t ON t.id = a.template_id
		WHERE a.id = $1 FOR UPDATE OF a`, arrangementID).
		Scan(&templateID, &passengerID, &driverID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotRepeatable
	}
	if err != nil {
		return fmt.Errorf("standing: loading the arrangement: %w", err)
	}

	// Declining is the driver's alone; ending an agreement belongs to both.
	if to == "declined" && userID != driverID {
		return ErrNotYours
	}
	// A decline answers a question, and once the weeks are booked the question
	// has been answered. Ending an active arrangement is a cancellation, which
	// gives the seats back; a decline would leave them held by a row that says
	// it was refused.
	if to == "declined" && status != "pending" {
		return ErrNotRepeatable
	}
	if userID != driverID && userID != passengerID {
		return ErrNotYours
	}
	if status == to {
		return nil // already settled; saying so twice is one act
	}
	if status != "pending" && status != "active" {
		return ErrNotRepeatable
	}

	if to == "cancelled" {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
			jobs.TemplateLockNamespace, templateID.String()); err != nil {
			return fmt.Errorf("standing: taking the template lock: %w", err)
		}
		// Only the weeks still ahead, and only the ones still holding a seat.
		// The rides already taken are history, and giving their seats back would
		// invent capacity on a trip that has happened.
		if err := s.releaseFutureWeeks(ctx, tx, arrangementID, userID == driverID, now); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE standing_arrangements SET status = $2, updated_at = now() WHERE id = $1`,
		arrangementID, to); err != nil {
		return fmt.Errorf("standing: settling the arrangement: %w", err)
	}

	// Whoever did not act is the one who needs telling.
	recipient := passengerID
	if userID == passengerID {
		recipient = driverID
	}
	// Spelled out rather than pasted together from the status, so that a new
	// status cannot invent an event name nothing knows how to say.
	event := notices.ArrangementCancelled
	if to == "declined" {
		event = notices.ArrangementDeclined
	}
	if err := s.notify(ctx, tx, event, recipient, arrangementID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("standing: commit: %w", err)
	}
	return nil
}

// releaseFutureWeeks cancels the arrangement's remaining bookings and returns
// their seats, one occurrence at a time and in id order so two cancellations
// racing each other cannot deadlock.
func (s *Service) releaseFutureWeeks(
	ctx context.Context, tx pgx.Tx, arrangementID uuid.UUID, byDriver bool, now time.Time,
) error {
	status := "cancelled_by_passenger"
	if byDriver {
		status = "cancelled_by_driver"
	}
	// The seat is returned from the same statement that spends the booking's
	// confirmed status, so a repeat can never give it back twice.
	rows, err := tx.Query(ctx, `
		WITH released AS (
			UPDATE bookings b
			SET status = $2, updated_at = now()
			FROM trip_occurrences o
			WHERE b.arrangement_id = $1
			  AND o.id = b.occurrence_id
			  AND b.status = 'confirmed'
			  AND o.depart_earliest > $3
			RETURNING b.occurrence_id, b.seats
		)
		UPDATE trip_occurrences o
		SET seats_free = o.seats_free + r.seats, updated_at = now()
		FROM released r WHERE o.id = r.occurrence_id
		RETURNING o.id`, arrangementID, status, now)
	if err != nil {
		return fmt.Errorf("standing: releasing the weeks ahead: %w", err)
	}

	var freed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("standing: reading a released week: %w", err)
		}
		freed = append(freed, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("standing: releasing the weeks ahead: %w", err)
	}

	// Every week this gave back is a trip with room on it again.
	return outbox.WakeAll(ctx, tx, freed, "seat_returned", now)
}

// Arrangement is the view both sides need to render the switch honestly: what
// was agreed, where it stands, and how many weeks it actually holds.
type Arrangement struct {
	ID, TemplateID        uuid.UUID
	DriverID, PassengerID uuid.UUID
	PassengerName         string
	OriginLabel           string
	DestinationLabel      string
	Weekday, Seats        int
	PriceEUR              float64
	Status                string
	Until                 time.Time
	WeeksBooked           int
}

// ForUser returns every arrangement the user is part of, on either side.
func (s *Service) ForUser(ctx context.Context, userID uuid.UUID) ([]Arrangement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.template_id, t.driver_id, a.passenger_id, u.name,
		       t.origin_label, t.destination_label,
		       a.weekday, a.seats, a.price_eur, a.status, a.until_date,
		       (SELECT count(*) FROM bookings b
		        WHERE b.arrangement_id = a.id AND b.status = 'confirmed')
		FROM standing_arrangements a
		JOIN route_templates t ON t.id = a.template_id
		JOIN users u ON u.id = a.passenger_id
		WHERE a.passenger_id = $1 OR t.driver_id = $1
		ORDER BY a.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("standing: listing arrangements: %w", err)
	}
	defer rows.Close()

	var out []Arrangement
	for rows.Next() {
		var a Arrangement
		if err := rows.Scan(&a.ID, &a.TemplateID, &a.DriverID, &a.PassengerID, &a.PassengerName,
			&a.OriginLabel, &a.DestinationLabel, &a.Weekday, &a.Seats, &a.PriceEUR,
			&a.Status, &a.Until, &a.WeeksBooked); err != nil {
			return nil, fmt.Errorf("standing: scanning an arrangement: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
