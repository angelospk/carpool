package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/outbox"
)

// ReconcileBatch is how many requests one query reads, not how many a pass
// covers. A pass walks the whole set a batch at a time.
//
// The distinction is the difference between a bound and a bug. Reading only the
// first batch and stopping would look reasonable — the order is by how long
// each has waited, so surely the ones served are the ones who have waited
// longest — but the oldest requests are old precisely because nothing has
// matched them, so they would hold every place in every pass for ever and
// everybody behind them would never be looked at again. There is no later event
// to catch those: this is the thing that catches what events miss.
const ReconcileBatch = 100

// ReconcileCeiling stops a pass that has found an implausible amount of work,
// so a runaway cannot queue without limit. Hitting it is logged rather than
// silent, because at that point the reconciler is no longer covering everybody
// and the number is the evidence.
const ReconcileCeiling = 20000

// Reconciler puts open requests that nobody is talking to back through the
// matcher.
//
// The wakes are the fast path and cover the changes worth instrumenting: a trip
// published, materialized, a seat given back, an offer ended. This is the slow
// one, and it exists because that list is a list of the ways somebody
// remembered. A driver turning notifications back on, widening the radius they
// will detour for, coming off a suspension, editing a route — each of those
// makes trips matchable without touching a seat, and none of them enqueues
// anything. So does the shortlist cap: a wake takes the ten longest-waiting and
// leaves the rest for whatever happens next, and on a quiet corridor nothing
// might.
//
// It is deliberately not the primary path. Wakes give a passenger an answer in
// seconds; this runs on the hour and its job is to make sure nobody is stranded
// for ever by a gap nobody has thought of yet.
type Reconciler struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

func NewReconciler(pool *pgxpool.Pool, c clock.Clock) *Reconciler {
	return &Reconciler{pool: pool, clock: c}
}

// Run queues another look at the requests still waiting for somebody.
//
// It returns how many it *looked at*, not how many messages were written: a
// duplicate key is dropped by the outbox, so within one interval a second pass
// reports the same number and enqueues nothing. That is the more useful figure
// of the two anyway — it is the size of the unmatched backlog, and a number
// that climbs and never falls means requests are going round and round without
// finding anybody.
func (r *Reconciler) Run(ctx context.Context) (int, error) {
	now := r.clock.Now()

	// Walked in batches with a keyset cursor rather than OFFSET: the set is
	// being read while other things insert into it, and OFFSET would skip or
	// repeat rows as it shifted underneath.
	//
	// Read by window rather than by status. A request whose time has passed is
	// only marked expired when the sweeper next runs, and reconsidering it in
	// between offers somebody a lift they no longer need.
	var (
		ids       []uuid.UUID
		afterTime = time.Time{}
		afterID   = uuid.Nil
	)
	for len(ids) < ReconcileCeiling {
		rows, err := r.pool.Query(ctx, `
			SELECT id, created_at FROM ride_requests
			WHERE status = 'open' AND window_end > $1
			  AND ($2::timestamptz IS NULL OR (created_at, id) > ($2, $3))
			ORDER BY created_at, id
			LIMIT $4`, now, nullableTime(afterTime), afterID, ReconcileBatch)
		if err != nil {
			return 0, fmt.Errorf("jobs: listing unmatched requests: %w", err)
		}

		var read int
		for rows.Next() {
			var id uuid.UUID
			var createdAt time.Time
			if err := rows.Scan(&id, &createdAt); err != nil {
				rows.Close()
				return 0, fmt.Errorf("jobs: scanning an unmatched request: %w", err)
			}
			ids = append(ids, id)
			afterTime, afterID = createdAt, id
			read++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("jobs: listing unmatched requests: %w", err)
		}
		if read < ReconcileBatch {
			break
		}
	}
	if len(ids) >= ReconcileCeiling {
		slog.Warn("the match reconciler stopped at its ceiling",
			"looked_at", len(ids),
			"note", "requests past this point were not reconciled in this pass")
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs: begin reconcile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One key per request per pass. Two passes overlapping — a slow one still
	// running when the timer fires again — then agree rather than double every
	// request's work, and a pass an hour later is a genuinely new look.
	pass := strconv.FormatInt(now.Truncate(ReconcileInterval).Unix(), 10)
	for _, id := range ids {
		payload, err := json.Marshal(map[string]any{"request_id": id})
		if err != nil {
			return 0, fmt.Errorf("jobs: encoding a reconcile ask: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "ask_next",
			IdempotencyKey: "ask_next:reconcile:" + pass + ":" + id.String(),
			Payload:        payload,
		}, now); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("jobs: commit reconcile: %w", err)
	}
	return len(ids), nil
}

// ReconcileInterval is how often the reconciler looks, and also the width of
// the bucket its idempotency keys fall into. The two are the same number on
// purpose: a shorter bucket than the interval lets one pass be queued twice,
// and a longer one silently drops a pass.
const ReconcileInterval = time.Hour

// nullableTime turns the zero time into a SQL NULL, so the first batch of a
// pass has no cursor to compare against rather than a year-zero one.
func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
