package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
)

// The privacy policy names a retention period for every category of data it
// collects, and until now nothing enforced any of them. A promise in a policy
// that the software does not keep is worse than no promise: it is a statement
// about the service that is not true.
//
// This implements the periods that are unambiguous. What is deliberately not
// here is recorded in docs/legal/README.md, because the remaining categories
// are product decisions rather than housekeeping — deleting a block after
// thirty-six months silently unblocks two people, and deleting trip history
// cascades into rows with their own, shorter clock.

// JanitorLockNamespace and JanitorLockID identify the advisory lock that keeps
// two janitors from sweeping at once. Advisory locks take a pair of int4s; the
// namespace is shared with the materializer's convention.
const (
	JanitorLockNamespace int32 = TemplateLockNamespace
	JanitorLockID        int32 = 1
)

// DefaultBatchSize bounds how many rows one delete statement removes.
//
// Every batch is its own statement, and so its own transaction. The first run
// against a database that has been collecting rows for a year would otherwise
// be a single delete holding locks over the whole table while the app waits.
const DefaultBatchSize = 1000

// maxBatchesPerRule stops one very large backlog from monopolising the database
// for an entire run. What is left over is picked up on the next tick, an hour
// later, which is soon enough for a retention window measured in months.
const maxBatchesPerRule = 200

// unlockTimeout bounds the advisory unlock on the way out.
const unlockTimeout = 5 * time.Second

// Janitor deletes data whose retention period has passed.
type Janitor struct {
	pool  *pgxpool.Pool
	clock clock.Clock

	// BatchSize is exported so a test can prove the loop continues past one
	// batch without inserting a thousand rows to find out.
	BatchSize int
}

// NewJanitor returns a janitor sweeping pool.
func NewJanitor(pool *pgxpool.Pool, c clock.Clock) *Janitor {
	return &Janitor{pool: pool, clock: c, BatchSize: DefaultBatchSize}
}

// rule is one retention period: a name for the log, and the statement that
// removes up to a batch of rows that have outlived it.
//
// The cutoff is expressed in SQL as an interval from an instant the caller
// passes in ($1), rather than from SQL's now(). Every other decision in this
// service comes from the injected clock, and a job that mixes the two behaves
// differently depending on which clock is ahead. The interval itself stays in
// SQL because Go's AddDate normalises 31 August minus six months into 3 March,
// while Postgres gives 28 February — and the second is what "six months" means
// to the person the policy is about.
type rule struct {
	name string
	sql  string
}

// rules are applied in order. Each is independent: nothing here deletes a row
// another rule is counting on.
func rules() []rule {
	// Rows are picked with SKIP LOCKED so the janitor never waits behind a
	// user's transaction. It means a pass can miss a locked row, which is
	// exactly right for a job that runs again in an hour.
	const batchDelete = `
		WITH doomed AS (
		    SELECT t.ctid FROM %[1]s t
		     WHERE %[2]s
		     ORDER BY t.ctid
		     FOR UPDATE SKIP LOCKED
		     LIMIT $2
		)
		DELETE FROM %[1]s t USING doomed WHERE t.ctid = doomed.ctid`

	batch := func(table, where string) string {
		return fmt.Sprintf(batchDelete, table, where)
	}

	return []rule{
		// "Κωδικοί μίας χρήσης και αρχεία επαλήθευσης — 6 μήνες", for fraud
		// investigation. Both tables are verification records: one proves a
		// number was reachable at sign-in, the other proves it at a change.
		{"one-time codes", batch("otp_codes",
			`t.created_at < $1::timestamptz - interval '6 months'`)},
		{"phone-change challenges", batch("phone_change_challenges",
			`t.created_at < $1::timestamptz - interval '6 months'`)},

		// Refresh tokens are not in the policy's table because they are not a
		// record of anything: they are credentials, and one that can no longer
		// be exchanged is nothing but a row that could leak. A month's grace
		// after it stops working, so that a support question about a session
		// that ended last week can still be answered.
		//
		// Two rules rather than one with an OR, because an OR across two
		// nullable timestamps cannot use either index.
		{"expired refresh tokens", batch("refresh_tokens",
			`t.expires_at < $1::timestamptz - interval '30 days'`)},
		{"revoked refresh tokens", batch("refresh_tokens",
			`t.revoked_at < $1::timestamptz - interval '30 days'`)},

		// "Μηνύματα — 12 μήνες από την ολοκλήρωση της κράτησης ή από το
		// τελευταίο μήνυμα". Both anchors, so a thread survives until the trip
		// itself is a year gone *and* nobody has written in a year.
		//
		// The unit is the thread, not the message. Deleting the older half of a
		// conversation leaves the rest misleading — an answer with no question
		// above it — and a dispute is read as a whole or not at all. So all of
		// a thread's messages share its anchor, which is a choice to document
		// rather than one to hide: under a maximum-retention policy, keeping
		// more is the direction that needs justifying, not less.
		//
		// The departure stands in for completion. A trip that has not run is
		// never eligible, however long ago the conversation about it started.
		{"messages on long-finished trips", batch("trip_messages",
			`t.occurrence_id IN (
			     SELECT o.id FROM trip_occurrences o
			      WHERE o.depart_earliest < $1::timestamptz - interval '12 months'
			        AND NOT EXISTS (
			              SELECT 1 FROM trip_messages m
			               WHERE m.occurrence_id = o.id
			                 AND m.created_at >= $1::timestamptz - interval '12 months'))`)},
	}
}

// Run applies every retention rule once.
//
// A rule that fails does not stop the ones after it. They are independent
// periods over unrelated tables, and letting the first failure starve the rest
// means one broken table quietly stops all retention.
func (j *Janitor) Run(ctx context.Context) error {
	// Only one janitor sweeps at a time. Two would not corrupt anything —
	// SKIP LOCKED sees to that — but they would scan the same tables and
	// double the load for no extra deletion.
	//
	// The lock is session-level and so must be taken and released on one
	// connection: released through the pool it would run on whichever session
	// came to hand, and unlock nothing.
	conn, err := j.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("jobs: acquiring a connection: %w", err)
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, $2)`,
		JanitorLockNamespace, JanitorLockID).Scan(&acquired); err != nil {
		return fmt.Errorf("jobs: taking the janitor lock: %w", err)
	}
	if !acquired {
		return nil // another replica is already sweeping
	}
	defer func() {
		// A fresh context, because the one above may be cancelled and the lock
		// has to come off the session before it goes back into the pool still
		// holding it. Bounded, because an unresponsive connection would
		// otherwise hang shutdown here instead of failing.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()

		var released bool
		err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`,
			JanitorLockNamespace, JanitorLockID).Scan(&released)
		if err == nil && released {
			return
		}

		// The lock is still held by this session and there is no way to ask it
		// again, so the session has to go: a session-level lock dies with its
		// backend. Left in the pool it would be a connection that silently
		// stops every future run.
		slog.Error("releasing the janitor lock; dropping the connection", "error", err, "released", released)
		if closeErr := conn.Conn().Close(unlockCtx); closeErr != nil {
			slog.Error("closing the janitor connection", "error", closeErr)
		}
	}()

	now := j.clock.Now()
	var errs []error
	for _, r := range rules() {
		// On the connection already held, not back through the pool. Asking for
		// a second connection while holding one makes the job depend on the
		// pool being larger than one, and a deployment that sets
		// pool_max_conns=1 would hang here forever rather than fail.
		deleted, err := j.sweep(ctx, conn, r, now)
		if deleted > 0 {
			slog.Info("retention sweep", "rule", r.name, "deleted", deleted)
		}
		if err != nil {
			if ctx.Err() != nil {
				// Shutdown, not failure. Stop rather than reporting every
				// remaining rule as broken.
				return errors.Join(append(errs, err)...)
			}
			errs = append(errs, fmt.Errorf("jobs: %s: %w", r.name, err))
		}
	}
	return errors.Join(errs...)
}

// sweep deletes in batches until a batch removes nothing, the run is cancelled,
// or the per-rule ceiling is reached.
func (j *Janitor) sweep(ctx context.Context, conn *pgxpool.Conn, r rule, now any) (int64, error) {
	size := j.BatchSize
	if size <= 0 {
		size = DefaultBatchSize
	}

	// No explicit cancellation check in the loop: pgx fails the query on a
	// cancelled context and the error ends the loop on the same tick. A second
	// check would be code no test can distinguish the absence of.
	var total int64
	for range maxBatchesPerRule {
		tag, err := conn.Exec(ctx, r.sql, now, size)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(size) {
			return total, nil
		}
	}
	slog.Warn("retention sweep hit its ceiling; the rest waits for the next run",
		"rule", r.name, "deleted", total)
	return total, nil
}
