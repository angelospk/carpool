package invites_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/invites"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

var testNow = time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)

func newWorld(t *testing.T, required bool) (*invites.Service, *pgxpool.Pool, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var regionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	return invites.NewService(pool, clock.NewFake(testNow), required), pool, regionID
}

// seedCode writes a code the way the pilot is started: by hand, belonging to
// nobody inside the system yet.
func seedCode(t *testing.T, pool *pgxpool.Pool, code string, maxUses int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO invite_codes (code, max_uses) VALUES ($1, $2)`, code, maxUses); err != nil {
		t.Fatalf("seeding the code: %v", err)
	}
}

func TestANewAccountNeedsACodeWhenThePilotIsClosed(t *testing.T) {
	svc, _, regionID := newWorld(t, true)

	_, err := svc.Admit(context.Background(), "+306900000001", "Γιάννης", regionID, "")

	if !errors.Is(err, invites.ErrCodeRequired) {
		t.Fatalf("Admit returned %v, want ErrCodeRequired", err)
	}
}

func TestAValidCodeAdmitsAndRecordsWhoLetThemIn(t *testing.T) {
	svc, pool, regionID := newWorld(t, true)
	seedCode(t, pool, "SEEDAAAA", 5)
	ctx := context.Background()

	user, err := svc.Admit(ctx, "+306900000001", "Γιάννης", regionID, "SEEDAAAA")
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	var code string
	if err := pool.QueryRow(ctx,
		`SELECT code FROM invite_redemptions WHERE user_id = $1`, user.ID).Scan(&code); err != nil {
		t.Fatalf("the chain back to whoever vouched is missing: %v", err)
	}
	if code != "SEEDAAAA" {
		t.Errorf("redeemed %q", code)
	}
	var used int
	if err := pool.QueryRow(ctx,
		`SELECT used_count FROM invite_codes WHERE code = 'SEEDAAAA'`).Scan(&used); err != nil {
		t.Fatalf("reading the code: %v", err)
	}
	if used != 1 {
		t.Errorf("used_count = %d, want 1", used)
	}
}

// Switching the pilot to invite-only must not lock out the people already in
// it: they are signing in, not joining.
func TestAnExistingMemberSignsInWithoutACode(t *testing.T) {
	open, pool, regionID := newWorld(t, false)
	ctx := context.Background()
	if _, err := open.Admit(ctx, "+306900000001", "Γιάννης", regionID, ""); err != nil {
		t.Fatalf("joining while the pilot was open: %v", err)
	}

	// The pilot closes behind them, against the same database.
	closed := invites.NewService(pool, clock.NewFake(testNow), true)

	if _, err := closed.Admit(ctx, "+306900000001", "Γιάννης", regionID, ""); err != nil {
		t.Fatalf("an existing member was locked out of their own account: %v", err)
	}
}

func TestACodeCannotBeSpentPastItsLimit(t *testing.T) {
	svc, pool, regionID := newWorld(t, true)
	seedCode(t, pool, "ONEONLYX", 1)
	ctx := context.Background()

	if _, err := svc.Admit(ctx, "+306900000001", "Πρώτος", regionID, "ONEONLYX"); err != nil {
		t.Fatalf("first Admit: %v", err)
	}

	_, err := svc.Admit(ctx, "+306900000002", "Δεύτερος", regionID, "ONEONLYX")

	if !errors.Is(err, invites.ErrCodeNotUsable) {
		t.Fatalf("Admit returned %v, want the code refused", err)
	}
	var users int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("counting users: %v", err)
	}
	if users != 1 {
		t.Errorf("%d accounts exist; the refused signup left one behind", users)
	}
}

// Two people racing for the last use of a code: exactly one gets in. Without
// the row lock both would read used_count = 0 and both would pass.
func TestTwoPeopleRacingForTheLastUseOnlyOneGetsIn(t *testing.T) {
	svc, pool, regionID := newWorld(t, true)
	seedCode(t, pool, "RACEONEX", 1)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]error, 2)
	phones := []string{"+306900000001", "+306900000002"}
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = svc.Admit(ctx, phones[i], "Κάποιος", regionID, "RACEONEX")
		}(i)
	}
	wg.Wait()

	admitted := 0
	for _, err := range results {
		if err == nil {
			admitted++
		} else if !errors.Is(err, invites.ErrCodeNotUsable) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if admitted != 1 {
		t.Fatalf("%d of 2 were admitted on a code with one use", admitted)
	}
	var used int
	if err := pool.QueryRow(ctx,
		`SELECT used_count FROM invite_codes WHERE code = 'RACEONEX'`).Scan(&used); err != nil {
		t.Fatalf("reading the code: %v", err)
	}
	if used != 1 {
		t.Errorf("used_count = %d, want 1", used)
	}
}

func TestACodeThatIsSpentExpiredOrWithdrawnFailsTheSameWay(t *testing.T) {
	svc, pool, regionID := newWorld(t, true)
	ctx := context.Background()
	seedCode(t, pool, "EXPIREDX", 5)
	seedCode(t, pool, "STOPPEDX", 5)
	if _, err := pool.Exec(ctx, `
		UPDATE invite_codes SET expires_at = $1 WHERE code = 'EXPIREDX'`,
		testNow.Add(-time.Hour)); err != nil {
		t.Fatalf("expiring: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE invite_codes SET disabled_at = $1 WHERE code = 'STOPPEDX'`, testNow); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	// One error for all of them: a distinct message per cause would tell someone
	// guessing codes which guesses were close.
	for i, code := range []string{"EXPIREDX", "STOPPEDX", "NOSUCHXX"} {
		phone := "+30690000000" + string(rune('1'+i))
		if _, err := svc.Admit(ctx, phone, "Κάποιος", regionID, code); !errors.Is(err, invites.ErrCodeNotUsable) {
			t.Errorf("%s returned %v, want ErrCodeNotUsable", code, err)
		}
	}
}

func TestEveryMemberGetsOneCodeAndKeepsIt(t *testing.T) {
	svc, pool, regionID := newWorld(t, false)
	ctx := context.Background()
	user, err := svc.Admit(ctx, "+306900000001", "Γιάννης", regionID, "")
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	first, err := svc.Mine(ctx, user.ID)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	second, err := svc.Mine(ctx, user.ID)
	if err != nil {
		t.Fatalf("Mine again: %v", err)
	}

	if first.Code != second.Code {
		t.Fatalf("the code changed between asks: %q then %q", first.Code, second.Code)
	}
	if len(first.Code) != 8 {
		t.Errorf("code %q is not eight characters", first.Code)
	}
	// Read out over the phone, so no characters that sound or look alike.
	for _, r := range first.Code {
		if strings := "BIOSZ01258"; containsRune(strings, r) {
			t.Errorf("code %q contains %q, which is misheard or mistyped", first.Code, r)
		}
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM invite_codes WHERE created_by = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("counting codes: %v", err)
	}
	if count != 1 {
		t.Errorf("%d codes exist for one member", count)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
