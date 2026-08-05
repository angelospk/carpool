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
		INSERT INTO users (date_of_birth, phone, name, home_region_id) VALUES (DATE '1990-01-01', $1, 'Test', $2) RETURNING id`,
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

	second, err := svc.Refresh(ctx, first.RefreshToken, uuid.Nil)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if second.RefreshToken == first.RefreshToken {
		t.Fatal("Refresh must issue a new refresh token, not return the same one")
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken, uuid.Nil); err != nil {
		t.Fatalf("the rotated token should itself be usable: %v", err)
	}
}

func TestRefreshRejectsAnUnknownToken(t *testing.T) {
	svc, _, _, _ := newSessions(t)

	if _, err := svc.Refresh(context.Background(), "not-a-real-token", uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
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

	if _, err := svc.Refresh(ctx, pair.RefreshToken, uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
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
	second, err := svc.Refresh(ctx, first.RefreshToken, uuid.Nil)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The attacker replays the already-used token.
	if _, err := svc.Refresh(ctx, first.RefreshToken, uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("replay returned %v, want ErrInvalidRefresh", err)
	}

	// The legitimate client's current token must now be dead too.
	if _, err := svc.Refresh(ctx, second.RefreshToken, uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
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

	if _, err := svc.Refresh(ctx, pair.RefreshToken, uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
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
	if _, err := svc.Refresh(ctx, tablet.RefreshToken, uuid.Nil); err != nil {
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
			_, errs[i] = svc.Refresh(ctx, pair.RefreshToken, uuid.Nil)
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

// The app persists a new refresh token only after the response arrives. If it
// dies in between — or the response is lost — the token it still holds is
// already spent on the server, and the strict rule above would log the user out
// through no fault of their own.
//
// The client therefore stores a random attempt id *before* sending, and replays
// the request with the same id. A used token presented with the attempt id that
// consumed it is a retry of one logical request, not a replay of a stolen one,
// so it gets the same successor back.
func TestRetryingARefreshWithTheSameAttemptIDReturnsTheSameToken(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	attempt := uuid.New()
	second, err := svc.Refresh(ctx, first.RefreshToken, attempt)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The response never reached the client, so it retries with the same id.
	retried, err := svc.Refresh(ctx, first.RefreshToken, attempt)
	if err != nil {
		t.Fatalf("retrying with the same attempt id failed: %v", err)
	}
	if retried.RefreshToken != second.RefreshToken {
		t.Fatal("a retry must return the same refresh token, not a different one")
	}
	if retried.AccessToken == "" {
		t.Fatal("a retry must still return a usable access token")
	}
	// And the session is intact: the family was not burned.
	if _, err := svc.Refresh(ctx, retried.RefreshToken, uuid.New()); err != nil {
		t.Fatalf("the session did not survive the retry: %v", err)
	}
}

// A retry proves nothing about a *different* request. Presenting a spent token
// with any other attempt id is still the signature of a stolen token.
func TestReplayingWithADifferentAttemptIDStillBurnsTheFamily(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := svc.Refresh(ctx, first.RefreshToken, uuid.New())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if _, err := svc.Refresh(ctx, first.RefreshToken, uuid.New()); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("replay with a fresh attempt id returned %v, want ErrInvalidRefresh", err)
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken, uuid.New()); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("after a replay the live token returned %v, want the family revoked", err)
	}
}

// A retry that arrives after the client has already moved on must fail closed —
// but it must not take the working session down with it.
func TestALateRetryFailsWithoutEndingTheSession(t *testing.T) {
	svc, _, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	attempt := uuid.New()
	second, err := svc.Refresh(ctx, first.RefreshToken, attempt)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	third, err := svc.Refresh(ctx, second.RefreshToken, uuid.New())
	if err != nil {
		t.Fatalf("Refresh (second rotation): %v", err)
	}

	// The successor has since been spent, so there is nothing to hand back.
	if _, err := svc.Refresh(ctx, first.RefreshToken, attempt); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("late retry returned %v, want ErrInvalidRefresh", err)
	}
	if _, err := svc.Refresh(ctx, third.RefreshToken, uuid.New()); err != nil {
		t.Fatalf("a late retry ended the live session: %v", err)
	}
}

// Duplicate delivery of one logical refresh must never let one successful
// response invalidate another. Every racer sees the same token, and the family
// gains exactly one successor.
func TestConcurrentRetriesOfOneAttemptAllReturnTheSameToken(t *testing.T) {
	svc, pool, _, userID := newSessions(t)
	ctx := context.Background()
	first, err := svc.Create(ctx, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const racers = 6
	attempt := uuid.New()
	pairs := make([]auth.TokenPair, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			pairs[i], errs[i] = svc.Refresh(ctx, first.RefreshToken, attempt)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v", i, err)
		}
		if pairs[i].RefreshToken != pairs[0].RefreshToken {
			t.Fatalf("racer %d got a different refresh token", i)
		}
	}

	var tokens int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1`, userID).Scan(&tokens); err != nil {
		t.Fatalf("counting tokens: %v", err)
	}
	if tokens != 2 {
		t.Fatalf("%d refresh tokens exist, want 2 (the original and one successor)", tokens)
	}
}

// Revoking a family is an UPDATE over the rows that exist when it runs. A
// rotation committing at the same moment inserts a row that update never saw,
// so "the whole family is revoked" could leave one live token behind — exactly
// the token a thief would be holding.
func TestBurningAFamilyRacesNoRotation(t *testing.T) {
	svc, pool, _, userID := newSessions(t)
	ctx := context.Background()

	for round := range 30 {
		spent, err := svc.Create(ctx, userID)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		live, err := svc.Refresh(ctx, spent.RefreshToken, uuid.Nil)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		// One goroutine rotates the live token; the other replays the spent one
		// and burns the family.
		var wg sync.WaitGroup
		start := make(chan struct{})
		var rotated auth.TokenPair
		var rotErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			rotated, rotErr = svc.Refresh(ctx, live.RefreshToken, uuid.Nil)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _ = svc.Refresh(ctx, spent.RefreshToken, uuid.Nil)
		}()
		close(start)
		wg.Wait()

		// Whatever the interleaving, nothing in that family may still work.
		if rotErr == nil {
			if _, err := svc.Refresh(ctx, rotated.RefreshToken, uuid.Nil); !errors.Is(err, auth.ErrInvalidRefresh) {
				t.Fatalf("round %d: a token survived the family revocation (err=%v)", round, err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
}
