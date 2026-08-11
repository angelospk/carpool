// Package bookings cancels a seat somebody is holding.
//
// It is separate from the two things that create a booking — a driver accepting
// an ask, and a driver approving a standing arrangement — because undoing one
// is the same act whichever way it was made: the seat goes back, the other side
// is told, and nothing else about the trip changes. A standing arrangement in
// particular survives one of its weeks being called off.
package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/outbox"
)

// ErrNotYours means the caller is neither the passenger holding the seat nor
// the driver carrying them.
var ErrNotYours = errors.New("bookings: not yours")

// ErrDeparted means the trip has already left. A seat cannot come back from a
// journey that has happened.
var ErrDeparted = errors.New("bookings: that trip has already left")

// ErrNotFound means there is no such booking.
var ErrNotFound = errors.New("bookings: no such booking")

// Service cancels bookings.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Cancel gives back one seat. Either side may do it, and the record says which.
//
// Cancelling is idempotent: a second call on an already-cancelled booking is
// the same act, not another one, and must not return the seat twice.
func (s *Service) Cancel(ctx context.Context, bookingID, userID uuid.UUID) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bookings: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		passengerID, driverID uuid.UUID
		status                string
		departs               time.Time
	)
	// The occurrence is locked before the booking, and CancelOccurrence does the
	// same, because the two paths touch the same two rows. Locking the booking
	// first here — which is what this did — meant a passenger giving back a seat
	// while the driver called off the departure took the rows in opposite orders
	// and the pair deadlocked.
	var occurrenceID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT occurrence_id FROM bookings WHERE id = $1`, bookingID).Scan(&occurrenceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("bookings: finding the trip: %w", err)
	}

	err = tx.QueryRow(ctx, `
		SELECT t.driver_id, o.depart_earliest
		  FROM trip_occurrences o
		  JOIN route_templates t ON t.id = o.template_id
		 WHERE o.id = $1 FOR UPDATE OF o`, occurrenceID).Scan(&driverID, &departs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("bookings: loading the trip: %w", err)
	}

	err = tx.QueryRow(ctx,
		`SELECT passenger_id, status FROM bookings WHERE id = $1 FOR UPDATE`, bookingID).
		Scan(&passengerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("bookings: loading the booking: %w", err)
	}
	if userID != passengerID && userID != driverID {
		return ErrNotYours
	}
	if status != "confirmed" {
		return nil // already cancelled, or the ride is done and rated
	}
	if !departs.After(now) {
		return ErrDeparted
	}

	byDriver := userID == driverID
	to := "cancelled_by_passenger"
	if byDriver {
		to = "cancelled_by_driver"
	}

	// The seat is returned by the same statement that spends the booking's
	// confirmed status. Two statements would let a repeat give it back twice.
	var freed []uuid.UUID
	rows, err := tx.Query(ctx, `
		WITH released AS (
			UPDATE bookings SET status = $2, updated_at = now()
			WHERE id = $1 AND status = 'confirmed'
			RETURNING occurrence_id, seats
		)
		UPDATE trip_occurrences o
		SET seats_free = o.seats_free + r.seats, updated_at = now()
		FROM released r WHERE o.id = r.occurrence_id
		RETURNING o.id`, bookingID, to)
	if err != nil {
		return fmt.Errorf("bookings: cancelling: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("bookings: reading the freed seat: %w", err)
		}
		freed = append(freed, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("bookings: cancelling: %w", err)
	}

	// A seat coming back is a trip becoming available again, and somebody on
	// this corridor may have been told there was no room on it.
	if err := outbox.WakeAll(ctx, tx, freed, "seat_returned", now); err != nil {
		return err
	}

	// Whoever did not act is the one who needs telling, and they need telling
	// before they drive to a stop nobody is standing at.
	recipient := passengerID
	if !byDriver {
		recipient = driverID
	}
	payload, err := json.Marshal(map[string]any{
		"event": "booking_cancelled", "recipient_id": recipient, "booking_id": bookingID,
	})
	if err != nil {
		return fmt.Errorf("bookings: encoding the notification: %w", err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "push",
		Payload:        payload,
		IdempotencyKey: "booking_cancelled:" + bookingID.String(),
	}, now); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bookings: commit: %w", err)
	}
	return nil
}

// CancelOccurrence is a driver calling off one departure.
//
// Distinct from retiring a route, which stops future departures being offered
// and leaves arranged ones alone. This is the opposite act: tomorrow morning is
// not happening, and the people expecting a seat in it need telling now.
//
// Every booking is cancelled through the same status the passenger-facing path
// uses, so a passenger sees "cancelled by the driver" and gets the same push,
// rather than a second kind of cancellation that the rest of the app has never
// heard of.
func (s *Service) CancelOccurrence(ctx context.Context, occurrenceID, userID uuid.UUID) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bookings: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		driverID uuid.UUID
		status   string
		departs  time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT t.driver_id, o.status, o.depart_earliest
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1 FOR UPDATE OF o`, occurrenceID).Scan(&driverID, &status, &departs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("bookings: loading the trip: %w", err)
	}
	// Not ErrNotYours: a passenger asking to cancel somebody else's departure
	// should not learn that it exists.
	if userID != driverID {
		return ErrNotFound
	}
	if status == "cancelled" {
		return nil // already called off; asking twice is not a failure
	}
	if !departs.After(now) {
		// Once it has set off, calling it off is not what happened. The trip is
		// confirmed or not, and that is a different question.
		return ErrDeparted
	}

	rows, err := tx.Query(ctx, `
		WITH released AS (
			UPDATE bookings SET status = 'cancelled_by_driver', updated_at = now()
			 WHERE occurrence_id = $1 AND status = 'confirmed'
			RETURNING id, passenger_id
		)
		SELECT id, passenger_id FROM released`, occurrenceID)
	if err != nil {
		return fmt.Errorf("bookings: cancelling the bookings: %w", err)
	}
	type cancelled struct{ booking, passenger uuid.UUID }
	var affected []cancelled
	for rows.Next() {
		var c cancelled
		if err := rows.Scan(&c.booking, &c.passenger); err != nil {
			rows.Close()
			return fmt.Errorf("bookings: reading the cancelled bookings: %w", err)
		}
		affected = append(affected, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return fmt.Errorf("bookings: reading the cancelled bookings: %w", rows.Err())
	}

	// The seats are not given back. The departure is gone, so there is nothing
	// left to take a seat in, and leaving seats_free where it was keeps the
	// row honest about what was booked when it was called off.
	if _, err := tx.Exec(ctx, `
		UPDATE trip_occurrences SET status = 'cancelled', updated_at = now()
		 WHERE id = $1`, occurrenceID); err != nil {
		return fmt.Errorf("bookings: cancelling the trip: %w", err)
	}

	for _, c := range affected {
		payload, err := json.Marshal(map[string]any{
			"event": "booking_cancelled", "recipient_id": c.passenger, "booking_id": c.booking,
		})
		if err != nil {
			return fmt.Errorf("bookings: encoding the notification: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "push",
			Payload:        payload,
			IdempotencyKey: "booking_cancelled:" + c.booking.String(),
		}, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bookings: commit: %w", err)
	}
	return nil
}
