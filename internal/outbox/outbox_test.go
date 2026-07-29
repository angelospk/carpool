package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/outbox"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

func newOutbox(t *testing.T) (*pgxpool.Pool, *clock.Fake) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool, clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
}

// recorder is a handler that records what it was asked to deliver.
type recorder struct {
	mu       sync.Mutex
	messages []outbox.Message
	err      error
	// permanent marks the returned error as non-retryable.
	permanent bool
}

func (r *recorder) Handle(_ context.Context, m outbox.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
	if r.err != nil {
		if r.permanent {
			return errors.Join(outbox.ErrPermanent, r.err)
		}
		return r.err
	}
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

func enqueue(t *testing.T, pool *pgxpool.Pool, key string) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := outbox.Enqueue(context.Background(), tx, outbox.Message{
		Kind:           "push",
		IdempotencyKey: key,
		Payload:        []byte(`{"hello":"world"}`),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestDispatchDeliversAPendingMessage(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{}

	n, err := outbox.NewDispatcher(pool, c, h).DispatchOnce(context.Background())
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if n != 1 {
		t.Fatalf("dispatched %d messages, want 1", n)
	}
	if h.count() != 1 {
		t.Fatalf("handler saw %d messages, want 1", h.count())
	}
}

func TestDeliveredMessagesAreNotDeliveredAgain(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{}
	d := outbox.NewDispatcher(pool, c, h)
	ctx := context.Background()

	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	n, err := d.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("second dispatch: %v", err)
	}

	if n != 0 {
		t.Fatalf("dispatched %d on the second pass, want 0", n)
	}
}

// A rolled-back business transaction must leave no trace: the whole point of
// the outbox is that the effect and the state change commit together.
func TestARolledBackTransactionEnqueuesNothing(t *testing.T) {
	pool, c := newOutbox(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Message{
		Kind: "push", IdempotencyKey: "ask:1", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	h := &recorder{}
	n, err := outbox.NewDispatcher(pool, c, h).DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if n != 0 {
		t.Fatalf("dispatched %d messages, want 0 after a rollback", n)
	}
}

func TestEnqueuingTheSameIdempotencyKeyTwiceStoresOneMessage(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	enqueue(t, pool, "ask:1")

	h := &recorder{}
	n, err := outbox.NewDispatcher(pool, c, h).DispatchOnce(context.Background())
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	if n != 1 {
		t.Fatalf("dispatched %d messages, want 1 for a duplicate key", n)
	}
}

func TestAFailedDeliveryIsRetriedAfterBackoff(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{err: errors.New("push service is down")}
	d := outbox.NewDispatcher(pool, c, h)
	ctx := context.Background()

	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if h.count() != 1 {
		t.Fatalf("handler saw %d messages, want 1", h.count())
	}

	// Immediately afterwards the message is still in backoff, so the handler
	// must not be called again. (DispatchOnce reports successful deliveries, so
	// with a failing handler the retry has to be observed through the handler.)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("dispatch during backoff: %v", err)
	}
	if h.count() != 1 {
		t.Fatalf("handler saw %d messages during backoff, want still 1", h.count())
	}

	c.Advance(outbox.BaseBackoff + time.Second)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("dispatch after backoff: %v", err)
	}
	if h.count() != 2 {
		t.Fatalf("handler saw %d messages after the backoff elapsed, want 2", h.count())
	}
}

func TestBackoffGrowsWithEachAttempt(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{err: errors.New("still down")}
	d := outbox.NewDispatcher(pool, c, h)
	ctx := context.Background()

	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	c.Advance(outbox.BaseBackoff + time.Second)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if h.count() != 2 {
		t.Fatalf("handler saw %d messages, want 2 before the growth check", h.count())
	}

	// After two failures the wait is longer than the base delay, so advancing
	// by the base alone must not make it claimable again.
	c.Advance(outbox.BaseBackoff + time.Second)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if h.count() != 2 {
		t.Fatalf("handler saw %d messages; the second backoff should be longer than the first", h.count())
	}
}

func TestAMessageIsDeadLetteredAfterTheAttemptLimit(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{err: errors.New("permanently broken")}
	d := outbox.NewDispatcher(pool, c, h)
	ctx := context.Background()

	for range outbox.MaxAttempts {
		if _, err := d.DispatchOnce(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		c.Advance(24 * time.Hour) // well past any backoff
	}

	var dead bool
	if err := pool.QueryRow(ctx,
		`SELECT dead_at IS NOT NULL FROM outbox WHERE idempotency_key = 'ask:1'`).Scan(&dead); err != nil {
		t.Fatalf("reading message: %v", err)
	}
	if !dead {
		t.Fatal("the message should have been dead-lettered after the attempt limit")
	}

	if n, _ := d.DispatchOnce(ctx); n != 0 {
		t.Fatalf("dispatched %d; a dead-lettered message must not be retried", n)
	}
}

// An invalid push token will never succeed, so retrying it wastes the queue.
func TestAPermanentFailureIsDeadLetteredImmediately(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	h := &recorder{err: errors.New("DeviceNotRegistered"), permanent: true}
	ctx := context.Background()

	if _, err := outbox.NewDispatcher(pool, c, h).DispatchOnce(ctx); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	var dead bool
	var attempts int
	if err := pool.QueryRow(ctx,
		`SELECT dead_at IS NOT NULL, attempts FROM outbox WHERE idempotency_key = 'ask:1'`).
		Scan(&dead, &attempts); err != nil {
		t.Fatalf("reading message: %v", err)
	}
	if !dead {
		t.Fatal("a permanent failure should be dead-lettered on the first attempt")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

// One undeliverable message must not stall everything queued behind it.
func TestAPoisonMessageDoesNotBlockLaterOnes(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "poison")
	enqueue(t, pool, "healthy")

	var seen []string
	h := handlerFunc(func(_ context.Context, m outbox.Message) error {
		seen = append(seen, m.IdempotencyKey)
		if m.IdempotencyKey == "poison" {
			return errors.New("nope")
		}
		return nil
	})

	if _, err := outbox.NewDispatcher(pool, c, h).DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	var sawHealthy bool
	for _, k := range seen {
		if k == "healthy" {
			sawHealthy = true
		}
	}
	if !sawHealthy {
		t.Fatalf("the healthy message was not delivered; saw %v", seen)
	}
}

// Two dispatchers are normal during a rolling restart. FOR UPDATE SKIP LOCKED
// is what keeps them from both delivering the same message.
func TestConcurrentDispatchersDoNotDoubleDeliver(t *testing.T) {
	pool, c := newOutbox(t)
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		enqueue(t, pool, k)
	}

	h := &recorder{}
	const runners = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = outbox.NewDispatcher(pool, c, h).DispatchOnce(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	if h.count() != 6 {
		t.Fatalf("handler saw %d deliveries, want exactly 6", h.count())
	}
}

// A worker that dies mid-delivery leaves its lease behind. Once the lease
// expires the message must become claimable again, or it is stranded forever.
func TestAnExpiredLeaseIsReclaimed(t *testing.T) {
	pool, c := newOutbox(t)
	enqueue(t, pool, "ask:1")
	ctx := context.Background()

	// Simulate a crashed worker: claimed, leased, never completed.
	if _, err := pool.Exec(ctx, `
		UPDATE outbox SET locked_by = 'dead-worker', locked_until = $1, attempts = 1
		WHERE idempotency_key = 'ask:1'`, c.Now().Add(outbox.LeaseDuration)); err != nil {
		t.Fatalf("simulating a crashed worker: %v", err)
	}

	h := &recorder{}
	d := outbox.NewDispatcher(pool, c, h)

	if n, _ := d.DispatchOnce(ctx); n != 0 {
		t.Fatalf("dispatched %d while the lease was live, want 0", n)
	}

	c.Advance(outbox.LeaseDuration + time.Minute)
	if n, _ := d.DispatchOnce(ctx); n != 1 {
		t.Fatalf("dispatched %d after the lease expired, want 1", n)
	}
}

type handlerFunc func(context.Context, outbox.Message) error

func (f handlerFunc) Handle(ctx context.Context, m outbox.Message) error { return f(ctx, m) }
