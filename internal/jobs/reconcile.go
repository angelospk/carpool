package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/outbox"
)

// ReconcileBatch bounds one pass. The rest wait for the next one; the ordering
// is by how long each has waited, so waiting is the thing that gets you served
// rather than the thing that gets you skipped.
const ReconcileBatch = 100

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
// It returns how many it queued, which is what a test asserts on and what a
// deployment can watch: a number that climbs and never falls means requests are
// going round and round without being matched.
func (r *Reconciler) Run(ctx context.Context) (int, error) {
	now := r.clock.Now()

	// Read by window rather than by status. A request whose time has passed is
	// only marked expired when the sweeper next runs, and reconsidering it in
	// between offers somebody a lift they no longer need.
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM ride_requests
		WHERE status = 'open' AND window_end > $1
		ORDER BY created_at, id
		LIMIT $2`, now, ReconcileBatch)
	if err != nil {
		return 0, fmt.Errorf("jobs: listing unmatched requests: %w", err)
	}

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("jobs: scanning an unmatched request: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("jobs: listing unmatched requests: %w", err)
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
