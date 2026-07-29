package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/auth"
	"carpool/internal/clock"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// testSecret mirrors the one in the internal token tests; the two test packages
// cannot share it.
const testSecret = "0123456789abcdef0123456789abcdef"

func newSessions(t *testing.T) (*auth.SessionService, *pgxpool.Pool, *clock.Fake, uuid.UUID) {
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
		INSERT INTO users (phone, name, home_region_id) VALUES ($1, 'Test', $2) RETURNING id`,
		testPhone, regionID).Scan(&userID)
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}

	c := clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
	return auth.NewSessionService(pool, c, auth.NewTokenIssuer(testSecret, c)), pool, c, userID
}

func TestCreateIssuesAUsableAccessToken(t *testing.T) {
	svc, _, c, userID := newSessions(t)

	pair, err := svc.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := auth.NewTokenIssuer(testSecret, c).ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("the issued access token does not parse: %v", err)
	}
	if got != userID {
		t.Fatalf("access token identifies %v, want %v", got, userID)
	}
	if pair.RefreshToken == "" {
		t.Fatal("no refresh token was issued")
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if second.RefreshToken == first.RefreshToken {
		t.Fatal("Refresh must issue a new refresh token, not return the same one")
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken); err != nil {
		t.Fatalf("the rotated token should itself be usable: %v", err)
	}
}

func TestRefreshRejectsAnUnknownToken(t *testing.T) {
	svc, _, _, _ := newSessions(t)

	if _, err := svc.Refresh(context.Background(), "not-a-real-token"); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh", err)
	}
}

func TestRefreshRejectsAnExpiredToken(t *testing.T) {
	svc, _, c, userID := newSessions(t)
	ctx := context.Background()
	pair, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c.Advance(auth.RefreshTokenTTL + time.Hour)

	if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh", err)
	}
}

// Replaying a rotated token is the signature of a stolen token: the legitimate
// client has already exchanged it. The whole family is revoked rather than just
// the replayed token, because we cannot tell which side is the attacker.
func TestReplayingARotatedTokenRevokesTheWholeFamily(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The attacker replays the already-used token.
	if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("replay returned %v, want ErrInvalidRefresh", err)
	}

	// The legitimate client's current token must now be dead too.
	if _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("after a replay the live token returned %v, want ErrInvalidRefresh", err)
	}
}

func TestRevokeEndsTheSession(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	pair, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Revoke(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("error = %v, want ErrInvalidRefresh after revocation", err)
	}
}

func TestSessionsAreIndependentAcrossDevices(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	phone, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create (phone): %v", err)
	}
	tablet, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create (tablet): %v", err)
	}

	if err := svc.Revoke(ctx, phone.RefreshToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Logging out on one device must not sign the user out everywhere.
	if _, err := svc.Refresh(ctx, tablet.RefreshToken); err != nil {
		t.Fatalf("the other device's session was ended too: %v", err)
	}
}

// Two app instances refreshing at once (a common race when several queries get
// a 401 together) must not both mint a session, or one silently invalidates the
// other and the user is logged out.
func TestConcurrentRefreshOfOneTokenSucceedsExactlyOnce(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	pair, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const racers = 4
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Refresh(ctx, pair.RefreshToken)
		}()
	}
	close(start)
	wg.Wait()

	var ok int
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d of %d concurrent refreshes succeeded, want exactly 1", ok, racers)
	}
}
