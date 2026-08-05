// Package asks implements the sequential driver-ask lifecycle: creating an ask
// for the next candidate, accepting or declining it, and expiring it.
package asks

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
	"carpool/internal/match"
	"carpool/internal/outbox"
	"carpool/internal/tolls"
)

// DefaultTTL is how long a driver has to answer before the ask expires and the
// next candidate is approached.
const DefaultTTL = 30 * time.Minute

// Errors the accept path can return. They are distinct because the app shows a
// different message for each.
var (
	// ErrAskNotPending covers an ask that was already accepted, declined,
	// expired or cancelled.
	ErrAskNotPending = errors.New("asks: the ask is no longer pending")
	// ErrAskExpired means the TTL passed before the driver answered.
	ErrAskExpired = errors.New("asks: the ask has expired")
	// ErrNoSeats means the occurrence filled up first.
	ErrNoSeats = errors.New("asks: the trip has no free seats")
	// ErrNoLuggageRoom means the seats are there but the boot is not. A separate
	// error from ErrNoSeats because it is a separate conversation: the driver
	// may well take this person with one bag instead of three.
	ErrNoLuggageRoom = errors.New("asks: the trip has no room left for that much luggage")
	// ErrStaleItinerary means the driver's plan changed while the detour was
	// being verified, so the quoted detour no longer applies.
	ErrStaleItinerary = errors.New("asks: the itinerary changed during verification")
	// ErrNotYours means the caller is not the driver being asked.
	ErrNotYours = errors.New("asks: the ask belongs to another driver")
)

// Verifier re-checks that a detour still fits the driver's budget, given the
// itinerary version it was computed against.
//
// It is called OUTSIDE the accept transaction: it needs an OSRM round trip, and
// holding row locks across a slow, failure-prone sidecar call would be a
// liveness hazard. The version check inside the transaction is what closes the
// resulting race — see RISKS.md open decision 4.
type Verifier interface {
	Verify(ctx context.Context, askID uuid.UUID, itineraryVersion int) error
}

// Service runs the ask lifecycle.
type Service struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	verifier Verifier
	router   match.Router
	// tolls is the shipped toll network. Nil when the dataset failed to load,
	// which costs the toll component of the suggested contribution and nothing
	// else — the app must still match rides.
	tolls *tolls.Dataset
}

// NewService returns a service. verifier may be nil, in which case the stored
// detour is trusted as-is. router is required by AskNext but not by Accept.
func NewService(pool *pgxpool.Pool, c clock.Clock, v Verifier) *Service {
	s := &Service{pool: pool, clock: c, verifier: v}
	if set, err := tolls.Load(); err == nil {
		s.tolls = set
	}
	return s
}

// WithRouter returns a copy of the service that uses r for the fine matching
// stage.
func (s *Service) WithRouter(r match.Router) *Service {
	out := *s
	out.router = r
	return &out
}

// Accept books the seat an ask offers.
//
// Locks are taken in the canonical order occurrence → ride_request → ask.
// Every path that mutates these rows uses the same order, which is what keeps
// concurrent accepts, declines and sweeps from deadlocking.
//
// Two mechanisms guard against overbooking, deliberately. The row lock below
// makes the seat check and the decrement atomic, so a loser gets a clean
// ErrNoSeats. The seats_free >= 0 CHECK constraint in the schema is the actual
// backstop: mutation-testing this function with the lock removed still produced
// exactly one booking, because the constraint rejects the second decrement.
// The lock buys the good error message; the constraint buys the invariant.
func (s *Service) Accept(ctx context.Context, askID, driverID uuid.UUID) (uuid.UUID, error) {
	// Read the version the ask was quoted against, then verify outside any
	// transaction.
	var (
		quotedVersion int
		preState      string
		preExpiresAt  time.Time
		preDriver     uuid.UUID
	)
	err := s.pool.QueryRow(ctx, `
		SELECT a.itinerary_version, a.status, a.expires_at, t.driver_id
		FROM asks a
		JOIN trip_occurrences o ON o.id = a.occurrence_id
		JOIN route_templates  t ON t.id = o.template_id
		WHERE a.id = $1`, askID).Scan(&quotedVersion, &preState, &preExpiresAt, &preDriver)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAskNotPending
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: loading ask: %w", err)
	}

	// Reject the obvious cases before spending an OSRM round trip. Anyone
	// holding an ask id could otherwise drive routing load with calls that were
	// always going to fail, and a declined ask would surface a routing error
	// instead of the documented ErrAskNotPending. These are all re-checked under
	// lock below; this is a cheap filter, not the authority.
	if preDriver != driverID {
		return uuid.Nil, ErrNotYours
	}
	if preState != "pending" {
		return uuid.Nil, ErrAskNotPending
	}
	if !s.clock.Now().Before(preExpiresAt) {
		return uuid.Nil, ErrAskExpired
	}

	if s.verifier != nil {
		if err := s.verifier.Verify(ctx, askID, quotedVersion); err != nil {
			return uuid.Nil, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Read the ask's foreign keys without locking so the locks below can be
	// taken in the canonical order regardless of which id the caller had.
	var occurrenceID, requestID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT occurrence_id, request_id FROM asks WHERE id = $1`, askID).Scan(&occurrenceID, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrAskNotPending
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: loading ask keys: %w", err)
	}

	// 1. occurrence
	var (
		seatsFree       int
		occurrenceState string
		version         int
		templateDriver  uuid.UUID
	)
	// luggageFree is derived rather than counted: space taken is the sum over the
	// bookings that hold it, so all six paths that cancel a booking give it back
	// by doing what they already do. The FOR UPDATE below is what makes reading
	// the sum and writing the new booking one indivisible thing — two people
	// racing for the last place in the boot serialise on this row exactly as they
	// already do for the last seat.
	var luggageFree int
	err = tx.QueryRow(ctx, `
		SELECT o.seats_free, o.status, o.itinerary_version, t.driver_id,
		       o.luggage_capacity - COALESCE((
		           SELECT sum(b.luggage_count) FROM bookings b
		           WHERE b.occurrence_id = o.id AND b.status IN ('confirmed', 'completed')
		       ), 0)
		FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1
		FOR UPDATE OF o`, occurrenceID).Scan(&seatsFree, &occurrenceState, &version, &templateDriver, &luggageFree)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking occurrence: %w", err)
	}

	// 2. request
	var requestState string
	var passengerID uuid.UUID
	var seats int
	var luggage int
	err = tx.QueryRow(ctx, `
		SELECT status, passenger_id, seats, luggage_count FROM ride_requests WHERE id = $1 FOR UPDATE`,
		requestID).Scan(&requestState, &passengerID, &seats, &luggage)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking request: %w", err)
	}

	// 3. ask
	var (
		askState  string
		expiresAt time.Time
		price     float64
	)
	err = tx.QueryRow(ctx, `
		SELECT status, expires_at, suggested_price_eur FROM asks WHERE id = $1 FOR UPDATE`,
		askID).Scan(&askState, &expiresAt, &price)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: locking ask: %w", err)
	}

	// Revalidate everything now that the rows are held.
	if templateDriver != driverID {
		return uuid.Nil, ErrNotYours
	}
	if askState != "pending" {
		return uuid.Nil, ErrAskNotPending
	}
	now := s.clock.Now()
	if !now.Before(expiresAt) {
		return uuid.Nil, ErrAskExpired
	}
	if occurrenceState != "scheduled" {
		return uuid.Nil, ErrAskNotPending
	}
	if requestState != "open" && requestState != "asking" {
		return uuid.Nil, ErrAskNotPending
	}
	if seatsFree < seats {
		return uuid.Nil, ErrNoSeats
	}
	if luggageFree < luggage {
		return uuid.Nil, ErrNoLuggageRoom
	}
	if version != quotedVersion {
		// The plan moved under us while OSRM was being consulted.
		return uuid.Nil, ErrStaleItinerary
	}

	var bookingID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
		                      pricing_basis, pickup_point, dropoff_point, luggage_count)
		SELECT a.occurrence_id, a.request_id, $2, a.id, $3, a.suggested_price_eur,
		       a.pricing_basis, a.pickup_point, a.dropoff_point, $4
		FROM asks a WHERE a.id = $1
		RETURNING id`, askID, passengerID, seats, luggage).Scan(&bookingID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: creating booking: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE trip_occurrences
		SET seats_free = seats_free - $2, itinerary_version = itinerary_version + 1, updated_at = now()
		WHERE id = $1`, occurrenceID, seats); err != nil {
		return uuid.Nil, fmt.Errorf("asks: reserving seats: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE asks SET status = 'accepted', responded_at = $2 WHERE id = $1`, askID, now); err != nil {
		return uuid.Nil, fmt.Errorf("asks: marking ask accepted: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE ride_requests SET status = 'matched', updated_at = now() WHERE id = $1`,
		requestID); err != nil {
		return uuid.Nil, fmt.Errorf("asks: closing request: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"booking_id":   bookingID,
		"request_id":   requestID,
		"passenger_id": passengerID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("asks: encoding notification: %w", err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "push",
		IdempotencyKey: "booking_confirmed:" + bookingID.String(),
		Payload:        payload,
	}, s.clock.Now()); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("asks: commit: %w", err)
	}
	return bookingID, nil
}

// Decline records a driver's refusal and frees the request for the next
// candidate.
func (s *Service) Decline(ctx context.Context, askID, driverID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var requestID, occurrenceID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT request_id, occurrence_id FROM asks WHERE id = $1`, askID).Scan(&requestID, &occurrenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAskNotPending
	}
	if err != nil {
		return fmt.Errorf("asks: loading ask: %w", err)
	}

	// Canonical lock order, same as Accept.
	var templateDriver uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT t.driver_id FROM trip_occurrences o
		JOIN route_templates t ON t.id = o.template_id
		WHERE o.id = $1 FOR UPDATE OF o`, occurrenceID).Scan(&templateDriver); err != nil {
		return fmt.Errorf("asks: locking occurrence: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM ride_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		return fmt.Errorf("asks: locking request: %w", err)
	}

	var askState string
	var expiresAt time.Time
	if err := tx.QueryRow(ctx,
		`SELECT status, expires_at FROM asks WHERE id = $1 FOR UPDATE`, askID).
		Scan(&askState, &expiresAt); err != nil {
		return fmt.Errorf("asks: locking ask: %w", err)
	}

	if templateDriver != driverID {
		return ErrNotYours
	}
	if askState != "pending" {
		return ErrAskNotPending
	}
	// The TTL is authoritative. Without this check the outcome would depend on
	// whether the driver or the sweeper got there first after the deadline, so
	// the same late tap could land as either 'declined' or 'expired'.
	if !s.clock.Now().Before(expiresAt) {
		return ErrAskExpired
	}

	if _, err := tx.Exec(ctx,
		`UPDATE asks SET status = 'declined', responded_at = $2 WHERE id = $1`,
		askID, s.clock.Now()); err != nil {
		return fmt.Errorf("asks: marking ask declined: %w", err)
	}
	if err := s.requeue(ctx, tx, requestID, "declined"); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("asks: commit: %w", err)
	}
	return nil
}

// SweepExpired expires asks whose TTL has passed and queues the next candidate
// for each affected request. It returns how many asks were expired.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	now := s.clock.Now()

	rows, err := s.pool.Query(ctx,
		`SELECT id, request_id FROM asks WHERE status = 'pending' AND expires_at <= $1 ORDER BY id`, now)
	if err != nil {
		return 0, fmt.Errorf("asks: finding expired asks: %w", err)
	}
	type expired struct{ askID, requestID uuid.UUID }
	var due []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.askID, &e.requestID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("asks: scanning expired ask: %w", err)
		}
		due = append(due, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("asks: reading expired asks: %w", err)
	}

	var count int
	for _, e := range due {
		ok, err := s.expireOne(ctx, e.askID, e.requestID, now)
		if err != nil {
			return count, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}

// expireOne expires a single ask, revalidating under the lock so it does not
// race an accept that landed first.
func (s *Service) expireOne(ctx context.Context, askID, requestID uuid.UUID, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("asks: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT 1 FROM ride_requests WHERE id = $1 FOR UPDATE`, requestID); err != nil {
		return false, fmt.Errorf("asks: locking request: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE asks SET status = 'expired', responded_at = $2
		WHERE id = $1 AND status = 'pending' AND expires_at <= $2`, askID, now)
	if err != nil {
		return false, fmt.Errorf("asks: expiring ask: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// An accept or decline won the race; nothing to do.
		return false, nil
	}

	if err := s.requeue(ctx, tx, requestID, "expired"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("asks: commit: %w", err)
	}
	return true, nil
}

// requeue puts a request back in the matching queue via the outbox, so the next
// candidate is approached by the same at-least-once path as everything else.
func (s *Service) requeue(ctx context.Context, tx pgx.Tx, requestID uuid.UUID, reason string) error {
	tag, err := tx.Exec(ctx,
		`UPDATE ride_requests SET status = 'open', updated_at = now()
		 WHERE id = $1 AND status IN ('open','asking')`, requestID)
	if err != nil {
		return fmt.Errorf("asks: reopening request: %w", err)
	}
	// If the request had already moved on (matched by another ask, cancelled by
	// the passenger), there is nothing to hand to the next driver. Emitting the
	// event regardless would send a second driver after an already-matched
	// passenger.
	if tag.RowsAffected() == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"request_id": requestID, "reason": reason})
	if err != nil {
		return fmt.Errorf("asks: encoding ask_next: %w", err)
	}
	// The key includes the reason and request so a decline and a later expiry
	// of different asks each get their own message.
	key := fmt.Sprintf("ask_next:%s:%s:%d", requestID, reason, s.clock.Now().UnixNano())
	return outbox.Enqueue(ctx, tx, outbox.Message{
		Kind:           "ask_next",
		IdempotencyKey: key,
		Payload:        payload,
	}, s.clock.Now())
}
