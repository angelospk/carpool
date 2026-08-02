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
	err = tx.QueryRow(ctx, `
		SELECT b.passenger_id, t.driver_id, b.status, o.depart_earliest
		FROM bookings b
		JOIN trip_occurrences o ON o.id = b.occurrence_id
		JOIN route_templates  t ON t.id = o.template_id
		WHERE b.id = $1 FOR UPDATE OF b`, bookingID).
		Scan(&passengerID, &driverID, &status, &departs)
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
	if _, err := tx.Exec(ctx, `
		WITH released AS (
			UPDATE bookings SET status = $2, updated_at = now()
			WHERE id = $1 AND status = 'confirmed'
			RETURNING occurrence_id, seats
		)
		UPDATE trip_occurrences o
		SET seats_free = o.seats_free + r.seats, updated_at = now()
		FROM released r WHERE o.id = r.occurrence_id`, bookingID, to); err != nil {
		return fmt.Errorf("bookings: cancelling: %w", err)
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
