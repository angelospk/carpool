// Package outbox delivers side effects that were enqueued in the same
// transaction as the state change that caused them.
//
// Delivery is at-least-once, not exactly-once. A worker can die between the
// provider accepting a message and the row being marked delivered, and that
// message will be sent again. Consumers deduplicate on the idempotency key —
// see RISKS.md open decision 5.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

const (
	// MaxAttempts is how many times a message is tried before dead-lettering.
	MaxAttempts = 6
	// BaseBackoff is the first retry delay; it doubles per attempt.
	BaseBackoff = 30 * time.Second
	// MaxBackoff caps the exponential growth.
	MaxBackoff = 30 * time.Minute
	// LeaseDuration is how long a claim is held. It must comfortably exceed the
	// provider timeout, or a slow delivery would be picked up twice.
	LeaseDuration = 2 * time.Minute
	// batchSize bounds how many messages one pass claims.
	batchSize = 20
)

// ErrPermanent marks a failure that will never succeed on retry, such as an
// unregistered push token. Join it into the returned error to dead-letter the
// message immediately instead of burning the whole attempt budget.
var ErrPermanent = errors.New("outbox: permanent failure")

// Message is one queued side effect.
type Message struct {
	ID             int64
	Kind           string
	Payload        []byte
	IdempotencyKey string
	Attempts       int
	// claimToken fences this delivery attempt. A worker may only write a result
	// for the claim it actually holds; see markDone/markFailed.
	claimToken string
}

// Handler performs the effect a message describes.
type Handler interface {
	Handle(ctx context.Context, m Message) error
}

// tx is the subset of pgx.Tx that Enqueue needs, so callers can pass whatever
// transaction they are already inside.
type tx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Enqueue adds a message inside the caller's transaction.
//
// A duplicate idempotency key is ignored rather than being an error: the same
// business event asking for the same effect twice is a no-op, not a failure.
func Enqueue(ctx context.Context, t tx, m Message, now time.Time) error {
	return EnqueueAt(ctx, t, m, now)
}

// EnqueueAt is Enqueue with an explicit due time.
//
// The dispatcher compares run_after against its injected clock, so run_after
// must come from that same clock. Letting the database's now() set it mixes two
// clock domains, and the two only agree by accident: with the clock held at
// noon for a test, a message enqueued at the database's real 19:44 is due in
// the future and is never delivered. That is a silently undelivered push in
// production the moment the API host and the database disagree about the time.
//
// A zero time is therefore refused rather than defaulted.
func EnqueueAt(ctx context.Context, t tx, m Message, runAfter time.Time) error {
	if runAfter.IsZero() {
		return fmt.Errorf("outbox: %s has no run_after; pass the caller's clock", m.IdempotencyKey)
	}
	// Deduplication is the whole contract of the key. A blank one matches every
	// other blank one, and ON CONFLICT DO NOTHING would drop the second effect
	// while reporting success.
	if m.IdempotencyKey == "" {
		return fmt.Errorf("outbox: a %s message has no idempotency key", m.Kind)
	}
	_, err := t.Exec(ctx, `
		INSERT INTO outbox (kind, payload, idempotency_key, run_after)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		m.Kind, m.Payload, m.IdempotencyKey, runAfter)
	if err != nil {
		return fmt.Errorf("outbox: enqueuing %s: %w", m.IdempotencyKey, err)
	}
	return nil
}

// Dispatcher claims and delivers queued messages.
type Dispatcher struct {
	pool    *pgxpool.Pool
	clock   clock.Clock
	handler Handler
	// worker identifies this dispatcher in the lease column.
	worker string
}

// NewDispatcher returns a dispatcher delivering through handler.
func NewDispatcher(pool *pgxpool.Pool, c clock.Clock, handler Handler) *Dispatcher {
	return &Dispatcher{pool: pool, clock: c, handler: handler, worker: "dispatcher"}
}

// DispatchOnce claims a batch and delivers it, returning how many messages were
// delivered successfully.
//
// The claim commits before the handler runs, so no database transaction is held
// open across a network call to a push or SMS provider.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	claimed, err := d.claim(ctx)
	if err != nil {
		return 0, err
	}

	var delivered int
	for _, m := range claimed {
		err := d.deliver(ctx, m)
		if err == nil {
			if err := d.markDone(ctx, m); err != nil {
				return delivered, err
			}
			delivered++
			continue
		}

		permanent := errors.Is(err, ErrPermanent)
		if err := d.markFailed(ctx, m, err, permanent); err != nil {
			return delivered, err
		}
		slog.Warn("outbox delivery failed",
			"key", m.IdempotencyKey, "attempts", m.Attempts, "permanent", permanent, "error", err)
	}
	return delivered, nil
}

// deliver runs the handler under a deadline no longer than the lease.
//
// One message can fan out to several devices, and the sends are sequential. A
// handler slower than LeaseDuration keeps working on a row another dispatcher
// has already reclaimed — burning the attempt budget twice over and, in the
// end, dead-lettering a message that was being delivered the whole time.
//
// The bound is real elapsed time on purpose. It measures how long the work has
// been running, not when anything is due, so it is the one place here that does
// not take its reading from the injected clock.
func (d *Dispatcher) deliver(ctx context.Context, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, LeaseDuration)
	defer cancel()
	return d.handler.Handle(ctx, m)
}

// claim leases a batch of due messages.
//
// FOR UPDATE SKIP LOCKED is what lets several dispatchers share the queue: each
// takes rows the others have not, instead of blocking. A row whose lease has
// expired is claimable again, which is how a crashed worker's messages are
// recovered rather than stranded.
func (d *Dispatcher) claim(ctx context.Context) ([]Message, error) {
	now := d.clock.Now()

	// Attempts are counted here rather than on completion, because a worker that
	// dies mid-delivery never reports back. Retiring exhausted messages at claim
	// time is therefore the only place MaxAttempts can actually be enforced: a
	// message that keeps killing its worker would otherwise retry forever.
	if err := d.retireExhausted(ctx, now); err != nil {
		return nil, err
	}

	// A fresh token per claim. locked_by alone cannot distinguish one dispatcher
	// run from the next, so it cannot fence a stale result.
	token := uuid.NewString()

	rows, err := d.pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM outbox
			WHERE done_at IS NULL
			  AND dead_at IS NULL
			  AND attempts < $5
			  AND run_after <= $1
			  AND (locked_until IS NULL OR locked_until <= $1)
			ORDER BY run_after, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox o
		SET locked_by = $3, locked_until = $4, attempts = o.attempts + 1
		FROM due
		WHERE o.id = due.id
		RETURNING o.id, o.kind, o.payload, o.idempotency_key, o.attempts`,
		now, batchSize, token, now.Add(LeaseDuration), MaxAttempts)
	if err != nil {
		return nil, fmt.Errorf("outbox: claiming messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Kind, &m.Payload, &m.IdempotencyKey, &m.Attempts); err != nil {
			return nil, fmt.Errorf("outbox: scanning message: %w", err)
		}
		m.claimToken = token
		out = append(out, m)
	}
	return out, rows.Err()
}

// retireExhausted dead-letters messages that have spent their attempt budget
// and are no longer leased.
func (d *Dispatcher) retireExhausted(ctx context.Context, now time.Time) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE outbox
		SET dead_at = $1,
		    last_error = COALESCE(last_error, 'attempt budget exhausted without a reported result')
		WHERE done_at IS NULL
		  AND dead_at IS NULL
		  AND attempts >= $2
		  AND (locked_until IS NULL OR locked_until <= $1)`, now, MaxAttempts)
	if err != nil {
		return fmt.Errorf("outbox: retiring exhausted messages: %w", err)
	}
	return nil
}

func (d *Dispatcher) markDone(ctx context.Context, m Message) error {
	tag, err := d.pool.Exec(ctx,
		`UPDATE outbox SET done_at = $2, locked_by = NULL, locked_until = NULL
		 WHERE id = $1 AND `+fence, m.ID, d.clock.Now(), m.claimToken, m.Attempts)
	if err != nil {
		return fmt.Errorf("outbox: marking %s delivered: %w", m.IdempotencyKey, err)
	}
	if tag.RowsAffected() == 0 {
		// The lease expired and another worker took over; its result stands.
		slog.Warn("discarding a stale outbox result", "key", m.IdempotencyKey, "attempt", m.Attempts)
	}
	return nil
}

// fence restricts a result write to the claim the worker actually holds. Without
// it a worker whose lease expired mid-delivery could clear a newer owner's lease
// or mark a message dead that the new owner had already delivered.
const fence = `locked_by = $3 AND attempts = $4 AND done_at IS NULL AND dead_at IS NULL`

// markFailed schedules a retry, or dead-letters when the failure is permanent
// or the attempt budget is spent.
func (d *Dispatcher) markFailed(ctx context.Context, m Message, cause error, permanent bool) error {
	now := d.clock.Now()

	if permanent || m.Attempts >= MaxAttempts {
		tag, err := d.pool.Exec(ctx, `
			UPDATE outbox SET dead_at = $2, last_error = $5, locked_by = NULL, locked_until = NULL
			WHERE id = $1 AND `+fence, m.ID, now, m.claimToken, m.Attempts, cause.Error())
		if err != nil {
			return fmt.Errorf("outbox: dead-lettering %s: %w", m.IdempotencyKey, err)
		}
		if tag.RowsAffected() == 0 {
			slog.Warn("discarding a stale outbox failure", "key", m.IdempotencyKey, "attempt", m.Attempts)
		}
		return nil
	}

	tag, err := d.pool.Exec(ctx, `
		UPDATE outbox SET run_after = $2, last_error = $5, locked_by = NULL, locked_until = NULL
		WHERE id = $1 AND `+fence, m.ID, now.Add(backoff(m.Attempts)), m.claimToken, m.Attempts, cause.Error())
	if err != nil {
		return fmt.Errorf("outbox: scheduling retry for %s: %w", m.IdempotencyKey, err)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("discarding a stale outbox failure", "key", m.IdempotencyKey, "attempt", m.Attempts)
	}
	return nil
}

// backoff returns the delay before the next attempt. It is deterministic: the
// retry schedule is asserted directly in tests, and jitter across a single
// dispatcher buys nothing.
func backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := time.Duration(math.Pow(2, float64(attempts-1))) * BaseBackoff
	if d > MaxBackoff || d <= 0 {
		return MaxBackoff
	}
	return d
}
