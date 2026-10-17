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
		// Live positions are meant to be gone when the sharing stops. The app
		// deletes its own row when it stops, and a trip that ends does too; this
		// is for the phone that died on the way. An hour is past any update the
		// screen would still show (it drops them at ten minutes).
		{"live positions", batch("trip_live_positions",
			`t.updated_at < $1::timestamptz - interval '1 hour'`)},
		// Wrong group codes are counted over a day. Kept two, so the count is
		// never short; after that they are only a record of somebody's typos.
		{"group join failures", batch("group_join_failures",
			`t.at < $1::timestamptz - interval '2 days'`)},

		// "Κωδικοί μίας χρήσης και αρχεία επαλήθευσης — 6 μήνες", for fraud
		// investigation. Both tables are verification records: one proves a
		// number was reachable at sign-in, the other proves it at a change.
		{"one-time codes", batch("otp_codes",
			`t.created_at < $1::timestamptz - interval '6 months'`)},
		{"phone-change challenges", batch("phone_change_challenges",
			`t.created_at < $1::timestamptz - interval '6 months'`)},
		{"account-deletion codes", batch("account_delete_codes",
			`t.created_at < $1::timestamptz - interval '6 months'`)},

		// Refresh tokens are not in the policy's table because they are not a
		// record of anything: they are credentials, and one that can no longer
		// be exchanged is nothing but a row that could leak. A month's grace
		// after it stops working, so that a support question about a session
		// that ended last week can still be answered.
		//
		// Two rules rather than one with an OR, because an OR across two
		// nullable timestamps cannot use either index.
		// A signup ticket is the same kind of thing as a refresh token and a
		// shorter-lived one: it is spendable for ten minutes and worthless
		// after. It is not a verification record — the otp_codes row that
		// produced it already is that, and keeps its six months — so nothing is
		// lost by dropping these the day after they die.
		{"spent and expired signup tickets", batch("signup_tickets",
			`t.expires_at < $1::timestamptz - interval '1 day'`)},

		{"expired refresh tokens", batch("refresh_tokens",
			`t.expires_at < $1::timestamptz - interval '30 days'`)},
		{"revoked refresh tokens", batch("refresh_tokens",
			`t.revoked_at < $1::timestamptz - interval '30 days'`)},

		// "Διαδρομές, αιτήματα, γεωμετρία — ιστορικό 24 μήνες".
		//
		// Anchored on the departure, not on when the route was created or last
		// touched. A trip planned long ago for next week is not history, and an
		// edit to an old route must not grant it another two years.
		//
		// Two things hold a departure back, both bounded, so neither turns into
		// indefinite retention:
		//
		//   Messages have their own twelve-month clock, anchored on the last
		//   message as well as on the departure. Cascading them away with the
		//   trip would break that promise for a thread somebody wrote in last
		//   month. Waiting instead means the message rule clears them first and
		//   the trip goes at a later run.
		//
		//   A report is kept for thirty-six months, twelve longer than the trip
		//   it is about. Holding the trip keeps the evidence attached for the
		//   whole life of the report rather than leaving an accusation with a
		//   null where the booking was.
		//
		// Ratings are NOT in that list, and must not be: they live as long as
		// the account, so waiting for them would make popular drivers' trips
		// immortal. Migration 00028 broke that cascade instead — the rating
		// survives the booking and keeps everything the app reads off it.
		{"departures from more than two years ago", batch("trip_occurrences",
			`t.depart_earliest < $1::timestamptz - interval '24 months'
			   AND NOT EXISTS (SELECT 1 FROM trip_messages m WHERE m.occurrence_id = t.id)
			   AND NOT EXISTS (
			         SELECT 1 FROM reports r
			           JOIN bookings b ON b.id = r.booking_id
			          WHERE b.occurrence_id = t.id)`)},

		// The passenger's half of the same sentence. Anchored on the travel
		// window rather than on creation, for the same reason: a request made in
		// January for a trip in December is not two years old in March.
		//
		// A request that is still open is left alone whatever its window says —
		// an expired window with an open status is a bug elsewhere, and deleting
		// the evidence of it would hide it.
		{"ride requests from more than two years ago", batch("ride_requests",
			`t.window_end < $1::timestamptz - interval '24 months'
			   AND t.status <> 'open'`)},

		// "Tokens ειδοποιήσεων — μέχρι την απενεργοποίηση, το πολύ 24 μήνες από
		// την τελευταία χρήση".
		//
		// COALESCE rather than a plain comparison, because last_used_at is
		// nullable and `null < cutoff` is unknown rather than true: a bare
		// predicate would make every token that predates the column immortal,
		// which is the failure mode of a retention rule that looks correct.
		// A token nobody has ever used ages from when it was registered, which
		// is the only other fact there is about it.
		// The policy is two promises in one line, and the age rule only kept the
		// second. A token the provider has rejected is dead the moment the
		// dispatcher retires it: nothing reads a disabled token again, so making
		// it wait two years keeps a row naming a device somebody threw away.
		// Compared against the clock rather than tested for NULL, which also
		// gives the parameter a type: every rule is handed the same instant, and
		// one that never mentions it leaves Postgres unable to infer what $1 is.
		{"retired push tokens", batch("push_tokens",
			`t.disabled_at <= $1::timestamptz`)},

		{"push tokens unused for two years", batch("push_tokens",
			`COALESCE(t.last_used_at, t.created_at) < $1::timestamptz - interval '24 months'`)},

		// "Αναφορές και αποκλεισμοί — 36 μήνες", from when the report was filed.
		//
		// Filed, not reviewed. Retention is about the event; a moderator opening
		// a three-year-old report to close it must not thereby grant it another
		// three years, or a queue nobody works becomes a queue that never
		// empties.
		{"reports filed three years ago", batch("reports",
			`t.created_at < $1::timestamptz - interval '36 months'`)},

		// "Κωδικοί προσκλήσεων — 24 μήνες από την τελευταία χρήση".
		//
		// Last use is the last redemption, and a code nobody redeemed ages from
		// when it was made. The redemptions themselves go with it: they are the
		// record of who invited whom, which is the same category and the same
		// period, and the foreign key cascades.
		{"invite codes unused for two years", batch("invite_codes",
			`COALESCE((SELECT max(ir.redeemed_at) FROM invite_redemptions ir WHERE ir.code = t.code),
			          t.created_at) < $1::timestamptz - interval '24 months'`)},

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
