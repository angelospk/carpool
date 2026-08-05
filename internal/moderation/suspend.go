// Package moderation carries out decisions about accounts.
//
// It exists because suspending somebody is not a flag. An account marked
// suspended whose trips still run has not been suspended: four people turn up
// tomorrow morning expecting the person the app has just decided cannot carry
// them, and the app tells nobody. So a suspension is a cascade — the account,
// its sessions, its future departures, its future seats and the notifications
// that go with them — and it happens in one transaction or not at all.
package moderation

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

// ErrNoAccount means there is no such user.
var ErrNoAccount = errors.New("moderation: no such account")

// ErrDeleted means the account has been erased. It is not suspended and cannot
// be reinstated: there is nothing left to give back, and setting it active
// would resurrect a person who asked to be forgotten.
var ErrDeleted = errors.New("moderation: that account has been deleted")

// Service applies moderation decisions.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Suspend stops an account and clears up after it.
//
// Idempotent: suspending somebody already suspended records nothing and tells
// nobody, because two moderators reaching the same conclusion is one decision,
// and a second round of "your ride is off" notifications is a second lie.
//
// It does not call bookings.Cancel or bookings.CancelOccurrence. Those handle
// one row at a time, with their own transactions and their own lock ordering;
// this is a set operation over everything an account has outstanding, and it has
// to be one transaction or a failure halfway leaves a suspended driver with live
// trips. What it does share is the vocabulary — the same booking statuses and
// the same booking_cancelled event — so nothing downstream meets a kind of
// cancellation it has never heard of.
func (s *Service) Suspend(ctx context.Context, userID, actorID uuid.UUID, reason string) error {
	if reason == "" {
		return errors.New("moderation: a suspension needs a reason")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.suspendTx(ctx, tx, userID, actorID, reason, s.clock.Now()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("moderation: commit: %w", err)
	}
	return nil
}

// suspendTx is Suspend inside the caller's transaction, which is what lets a
// report be closed and its subject suspended as one act rather than two that
// can half-happen.
func (s *Service) suspendTx(ctx context.Context, tx pgx.Tx, userID, actorID uuid.UUID, reason string, now time.Time) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoAccount
	}
	if err != nil {
		return fmt.Errorf("moderation: loading the account: %w", err)
	}
	switch status {
	case "suspended":
		return nil
	case "deleted":
		return ErrDeleted
	}

	if _, err := tx.Exec(ctx,
		`UPDATE users SET status = 'suspended', updated_at = now() WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("moderation: suspending the account: %w", err)
	}

	// Belt to the middleware's bracer. Every authenticated request re-reads the
	// status, so a live access token stops working within milliseconds; revoking
	// the refresh tokens is what stops a client minting a fresh one.
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID, now); err != nil {
		return fmt.Errorf("moderation: ending the sessions: %w", err)
	}

	told, err := s.callOffTheirDriving(ctx, tx, userID, now)
	if err != nil {
		return err
	}
	asPassenger, err := s.giveBackTheirSeats(ctx, tx, userID, now)
	if err != nil {
		return err
	}
	told = append(told, asPassenger...)

	for _, n := range told {
		payload, err := json.Marshal(map[string]any{
			"event": "booking_cancelled", "recipient_id": n.recipient, "booking_id": n.booking,
		})
		if err != nil {
			return fmt.Errorf("moderation: encoding the notification: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "push",
			Payload:        payload,
			IdempotencyKey: "booking_cancelled:" + n.booking.String(),
		}, now); err != nil {
			return err
		}
	}

	return s.record(ctx, tx, userID, actorID, "suspend", reason)
}

// Reinstate gives an account back.
//
// It does not give the trips back. Those passengers were told their ride was off
// and have made other arrangements; un-cancelling a departure somebody has
// stopped expecting is worse than leaving it cancelled.
func (s *Service) Reinstate(ctx context.Context, userID, actorID uuid.UUID, reason string) error {
	if reason == "" {
		return errors.New("moderation: a reinstatement needs a reason")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoAccount
	}
	if err != nil {
		return fmt.Errorf("moderation: loading the account: %w", err)
	}
	if status == "deleted" {
		return ErrDeleted
	}
	if status == "active" {
		return nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE users SET status = 'active', updated_at = now() WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("moderation: reinstating: %w", err)
	}
	if err := s.record(ctx, tx, userID, actorID, "reinstate", reason); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("moderation: commit: %w", err)
	}
	return nil
}

// told is one person who has to hear that a ride is off.
type told struct{ recipient, booking uuid.UUID }

// callOffTheirDriving cancels every departure of theirs that has not left, and
// every seat booked in one.
//
// The seats are not returned to the occurrence. The departure is gone, so there
// is nothing left to take a seat in, and leaving seats_free where it was keeps
// the row honest about what was booked when it was called off — the same
// reasoning as a driver calling off a trip by hand.
func (s *Service) callOffTheirDriving(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) ([]told, error) {
	rows, err := tx.Query(ctx, `
		WITH doomed AS (
			UPDATE trip_occurrences o SET status = 'cancelled', updated_at = now()
			  FROM route_templates t
			 WHERE t.id = o.template_id
			   AND t.driver_id = $1
			   AND o.depart_earliest > $2
			   AND o.status <> 'cancelled'
			RETURNING o.id
		)
		UPDATE bookings b SET status = 'cancelled_by_driver', updated_at = now()
		 WHERE b.occurrence_id IN (SELECT id FROM doomed) AND b.status = 'confirmed'
		RETURNING b.passenger_id, b.id`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("moderation: calling off their departures: %w", err)
	}
	defer rows.Close()

	var out []told
	for rows.Next() {
		var n told
		if err := rows.Scan(&n.recipient, &n.booking); err != nil {
			return nil, fmt.Errorf("moderation: reading the cancelled bookings: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// giveBackTheirSeats cancels every seat they are holding in a trip that has not
// left, and hands each one back to its driver to sell again.
func (s *Service) giveBackTheirSeats(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) ([]told, error) {
	rows, err := tx.Query(ctx, `
		WITH released AS (
			UPDATE bookings b SET status = 'cancelled_by_passenger', updated_at = now()
			  FROM trip_occurrences o
			 WHERE o.id = b.occurrence_id
			   AND b.passenger_id = $1
			   AND b.status = 'confirmed'
			   AND o.depart_earliest > $2
			   AND o.status <> 'cancelled'
			RETURNING b.id, b.occurrence_id, b.seats
		),
		returned AS (
			UPDATE trip_occurrences o
			   SET seats_free = o.seats_free + r.seats, updated_at = now()
			  FROM released r WHERE o.id = r.occurrence_id
			RETURNING o.id
		)
		SELECT r.id, t.driver_id
		  FROM released r
		  JOIN trip_occurrences o ON o.id = r.occurrence_id
		  JOIN route_templates t  ON t.id = o.template_id`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("moderation: giving back their seats: %w", err)
	}
	defer rows.Close()

	var out []told
	for rows.Next() {
		var n told
		if err := rows.Scan(&n.booking, &n.recipient); err != nil {
			return nil, fmt.Errorf("moderation: reading the released seats: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Service) record(ctx context.Context, tx pgx.Tx, userID, actorID uuid.UUID, action, reason string) error {
	var actor *uuid.UUID
	if actorID != uuid.Nil {
		actor = &actorID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO account_actions (user_id, action, reason, actor_id)
		VALUES ($1, $2, $3, $4)`, userID, action, reason, actor); err != nil {
		return fmt.Errorf("moderation: recording the %s: %w", action, err)
	}
	return nil
}
