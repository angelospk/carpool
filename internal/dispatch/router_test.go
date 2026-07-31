package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/dispatch"
	"carpool/internal/outbox"
	"carpool/internal/push"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// sender records what it was asked to deliver and fails whichever tokens the
// test names. push.Fake cannot express per-token outcomes, which is exactly
// what the classification of a fan-out failure turns on.
type sender struct {
	mu   sync.Mutex
	sent []push.Notification
	// errs maps a token to the error its send returns.
	errs map[string]error
}

func (s *sender) Send(_ context.Context, n push.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, n)
	return s.errs[n.Token]
}

func (s *sender) tokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, n := range s.sent {
		out = append(out, n.Token)
	}
	return out
}

func newRouter(t *testing.T) (*pgxpool.Pool, *sender, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID)
	if err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	var userID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001', 'Οδηγός', $1) RETURNING id`, regionID).Scan(&userID)
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}

	return pool, &sender{errs: map[string]error{}}, userID
}

func addToken(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, token string, disabled bool) {
	t.Helper()
	var at any
	if disabled {
		at = "2026-07-01T00:00:00Z"
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO push_tokens (token, user_id, platform, disabled_at)
		VALUES ($1, $2, 'android', $3)`, token, userID, at)
	if err != nil {
		t.Fatalf("seeding push token: %v", err)
	}
}

func askMessage(t *testing.T, driverID uuid.UUID) outbox.Message {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"ask_id": uuid.New(), "driver_id": driverID, "detour_min": 7.0,
	})
	if err != nil {
		t.Fatalf("marshalling payload: %v", err)
	}
	return outbox.Message{Kind: "push", Payload: payload, IdempotencyKey: "ask:1"}
}

func TestAPushReachesOnlyTheRecipientsLiveDevices(t *testing.T) {
	pool, snd, driverID := newRouter(t)

	var otherID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (phone, name, home_region_id)
		SELECT '+306900000002', 'Άλλος', home_region_id FROM users WHERE id = $1
		RETURNING id`, driverID).Scan(&otherID)
	if err != nil {
		t.Fatalf("seeding second user: %v", err)
	}

	addToken(t, pool, driverID, "live-phone", false)
	addToken(t, pool, driverID, "live-tablet", false)
	addToken(t, pool, driverID, "retired", true)
	addToken(t, pool, otherID, "someone-else", false)

	r := dispatch.NewRouter(pool, snd, func(context.Context, uuid.UUID) error { return nil })
	if err := r.Handle(context.Background(), askMessage(t, driverID)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got := snd.tokens()
	want := map[string]bool{"live-phone": true, "live-tablet": true}
	if len(got) != len(want) {
		t.Fatalf("delivered to %v, want exactly the two live devices", got)
	}
	for _, token := range got {
		if !want[token] {
			t.Errorf("delivered to %q, which is retired or belongs to another user", token)
		}
	}
	for _, n := range snd.sent {
		if n.Data["idempotency_key"] != "ask:1" {
			t.Errorf("notification carries %v, want the idempotency key so the app can deduplicate", n.Data["idempotency_key"])
		}
	}
}

func TestAPermanentFailureOnOneDeviceDoesNotCancelTheRetryForAnother(t *testing.T) {
	pool, snd, driverID := newRouter(t)
	addToken(t, pool, driverID, "broken", false)
	addToken(t, pool, driverID, "slow", false)

	// Expo classifies some ticket errors as permanent for that token alone.
	snd.errs["broken"] = fmt.Errorf("%w: expo rejected the message", outbox.ErrPermanent)
	snd.errs["slow"] = errors.New("expo timed out")

	r := dispatch.NewRouter(pool, snd, nil)
	err := r.Handle(context.Background(), askMessage(t, driverID))

	if err == nil {
		t.Fatal("Handle succeeded; the timed-out device never got the notification")
	}
	if errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("Handle returned a permanent failure (%v); that dead-letters the message and the "+
			"device that merely timed out is never retried", err)
	}
	if len(snd.tokens()) != 2 {
		t.Errorf("attempted %v, want both devices tried despite the first failing", snd.tokens())
	}
}

func TestADeadDeviceIsRetiredWithoutFailingTheMessage(t *testing.T) {
	pool, snd, driverID := newRouter(t)
	addToken(t, pool, driverID, "uninstalled", false)
	addToken(t, pool, driverID, "live", false)
	snd.errs["uninstalled"] = fmt.Errorf("%w: %w", outbox.ErrPermanent, push.ErrDeviceNotRegistered)

	r := dispatch.NewRouter(pool, snd, nil)
	if err := r.Handle(context.Background(), askMessage(t, driverID)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var disabled bool
	err := pool.QueryRow(context.Background(),
		`SELECT disabled_at IS NOT NULL FROM push_tokens WHERE token = 'uninstalled'`).Scan(&disabled)
	if err != nil {
		t.Fatalf("reading token: %v", err)
	}
	if !disabled {
		t.Error("the uninstalled device is still live; every later push will burn on it")
	}
}

func TestAMessageWithNoLiveDeviceIsNotAFailure(t *testing.T) {
	pool, snd, driverID := newRouter(t)

	r := dispatch.NewRouter(pool, snd, nil)
	if err := r.Handle(context.Background(), askMessage(t, driverID)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(snd.tokens()) != 0 {
		t.Errorf("sent %v with no registered device", snd.tokens())
	}
}

func TestAnUndeliverableMessageIsRejectedRatherThanRetried(t *testing.T) {
	pool, snd, driverID := newRouter(t)
	addToken(t, pool, driverID, "live", false)

	cases := []struct {
		name    string
		message outbox.Message
	}{
		{"an unknown kind", outbox.Message{Kind: "smoke-signal", Payload: []byte(`{}`)}},
		{"undecodable json", outbox.Message{Kind: "push", Payload: []byte(`{`)}},
		{"a push naming nobody", outbox.Message{Kind: "push", Payload: []byte(`{"detour_min":3}`)}},
		{"a push naming the zero user", outbox.Message{Kind: "push",
			Payload: []byte(`{"ask_id":"` + uuid.New().String() + `","driver_id":"` + uuid.Nil.String() + `"}`)}},
		{"an ask_next naming no request", outbox.Message{Kind: "ask_next", Payload: []byte(`{}`)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			r := dispatch.NewRouter(pool, snd, func(context.Context, uuid.UUID) error {
				called = true
				return nil
			})
			err := r.Handle(context.Background(), tc.message)
			if !errors.Is(err, outbox.ErrPermanent) {
				t.Fatalf("Handle returned %v, want a permanent failure: retrying cannot fix the payload", err)
			}
			if called {
				t.Error("the ask handler ran on a message that names no request")
			}
		})
	}
}

func TestAMisconfiguredAskHandlerIsNotWorthRetrying(t *testing.T) {
	pool, snd, _ := newRouter(t)

	r := dispatch.NewRouter(pool, snd, nil)
	payload, err := json.Marshal(map[string]any{"request_id": uuid.New()})
	if err != nil {
		t.Fatalf("marshalling payload: %v", err)
	}
	err = r.Handle(context.Background(),
		outbox.Message{Kind: "ask_next", Payload: payload, IdempotencyKey: "next:1"})

	if !errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("Handle returned %v, want a permanent failure: a nil handler will not appear on retry", err)
	}
}

func TestAValidAskNextReachesTheHandlerOnce(t *testing.T) {
	pool, snd, _ := newRouter(t)
	want := uuid.New()

	var got []uuid.UUID
	r := dispatch.NewRouter(pool, snd, func(_ context.Context, id uuid.UUID) error {
		got = append(got, id)
		return errors.New("no driver left to ask")
	})
	payload, err := json.Marshal(map[string]any{"request_id": want})
	if err != nil {
		t.Fatalf("marshalling payload: %v", err)
	}
	err = r.Handle(context.Background(),
		outbox.Message{Kind: "ask_next", Payload: payload, IdempotencyKey: "next:1"})

	if err == nil || errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("Handle returned %v, want the handler's own error passed through as retryable", err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("handler saw %v, want exactly one call for %v", got, want)
	}
}
