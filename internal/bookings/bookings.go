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
	"carpool/internal/jobs"
	"carpool/internal/notices"
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

	// request is nil for a seat that came from a standing arrangement.
	var request *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT passenger_id, status, request_id FROM bookings WHERE id = $1 FOR UPDATE`, bookingID).
		Scan(&passengerID, &status, &request)
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
	// before they drive to a stop nobody is standing at. Which of the two it is
	// decides the words as well as the address: a passenger has lost a ride, a
	// driver has got a seat back, and one sentence cannot be both.
	recipient, event := passengerID, notices.BookingCancelledForPassenger
	if !byDriver {
		recipient, event = driverID, notices.BookingCancelledForDriver
	}
	notice, err := notices.BookingCancelled(event, recipient, bookingID)
	if err != nil {
		return fmt.Errorf("bookings: %w", err)
	}
	if err := outbox.Enqueue(ctx, tx, notice, now); err != nil {
		return err
	}

	// A driver taking back one seat strands its passenger exactly as calling
	// off the departure does, and gets the same recovery: the request they
	// already made goes back to the matcher. A passenger giving their own seat
	// back has changed their mind, and is left alone.
	if byDriver && request != nil {
		if err := s.lookAgainFor(ctx, tx, *request, bookingID, now); err != nil {
			return err
		}
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

	// The materializer's lock on the route first, as it takes it: the
	// departure is written off in the schedule below, and a run that read the
	// schedule before that would otherwise find the departure with nobody
	// booked on it and put it back.
	var templateID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT template_id FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("bookings: loading the trip: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		jobs.TemplateLockNamespace, templateID.String()); err != nil {
		return fmt.Errorf("bookings: taking the route lock: %w", err)
	}

	var (
		driverID    uuid.UUID
		status      string
		departs     time.Time
		serviceDate time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT t.driver_id, o.status, o.depart_earliest, o.service_date
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1 FOR UPDATE OF o`, occurrenceID).Scan(&driverID, &status, &departs, &serviceDate)
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
			RETURNING id, passenger_id, request_id
		)
		SELECT id, passenger_id, request_id FROM released`, occurrenceID)
	if err != nil {
		return fmt.Errorf("bookings: cancelling the bookings: %w", err)
	}
	type cancelled struct {
		booking, passenger uuid.UUID
		// request is nil for a seat that came from a standing arrangement,
		// which is its own standing permission and needs no requeueing.
		request *uuid.UUID
	}
	var affected []cancelled
	for rows.Next() {
		var c cancelled
		if err := rows.Scan(&c.booking, &c.passenger, &c.request); err != nil {
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

	// And off the schedule. With nobody booked on it any more the departure
	// is one the materializer may rewrite, and it did: the next run set it
	// back to 'scheduled', and passengers were matched to a morning the
	// driver had called off. An exception is how a schedule says "not this
	// day".
	if _, err := tx.Exec(ctx, `
		UPDATE route_schedules
		   SET exceptions = array_append(exceptions, $2::date), updated_at = now()
		 WHERE template_id = $1 AND NOT ($2::date = ANY(exceptions))`,
		templateID, serviceDate); err != nil {
		return fmt.Errorf("bookings: writing the day off the schedule: %w", err)
	}

	for _, c := range affected {
		notice, err := notices.BookingCancelled(
			notices.BookingCancelledForPassenger, c.passenger, c.booking)
		if err != nil {
			return fmt.Errorf("bookings: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, notice, now); err != nil {
			return err
		}
		if c.request == nil {
			continue
		}
		if err := s.lookAgainFor(ctx, tx, *c.request, c.booking, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("bookings: commit: %w", err)
	}
	return nil
}

// lookAgainFor records that a passenger's seat was cancelled out from under
// them, so that the queue can go looking for another driver.
//
// It records rather than does. A ride_request is standing permission to be
// matched inside its window — it is how the first driver was found, and neither
// the driver cancelling nor a moderator acting on them withdraws it — but
// reopening it here would mean this transaction, which already holds the
// occurrence and its bookings, also taking a lock on a row belonging to
// somebody else, in an order nothing else in the app uses. The recovery happens
// in the dispatcher, where the passenger and their request can be taken in the
// ordinary order and everything can be re-read. See dispatch.handleReopen.
func (s *Service) lookAgainFor(
	ctx context.Context, tx pgx.Tx, requestID, bookingID uuid.UUID, now time.Time,
) error {
	payload, err := json.Marshal(map[string]any{
		"booking_id": bookingID, "request_id": requestID, "reason": "trip_cancelled",
	})
	if err != nil {
		return fmt.Errorf("bookings: encoding the reopen: %w", err)
	}
	return outbox.Enqueue(ctx, tx, outbox.Message{
		Kind: "reopen_request",
		// Keyed on the seat that was lost: one cancellation asks once however
		// many times it is retried, and somebody stranded a second time is a
		// different booking and so a different key.
		IdempotencyKey: "reopen:" + bookingID.String(),
		Payload:        payload,
	}, now)
}
