package groups_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/groups"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

type world struct {
	svc      *groups.Service
	pool     *pgxpool.Pool
	clock    *clock.Fake
	regionID uuid.UUID
	phones   int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, clock: clock.NewFake(time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC))}
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&w.regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	w.svc = groups.NewService(pool, w.clock)
	return w
}

func (w *world) person(t *testing.T) uuid.UUID {
	t.Helper()
	w.phones++
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Μέλος', $2) RETURNING id`,
		fmt.Sprintf("+30690000%04d", w.phones), w.regionID).Scan(&id); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return id
}

func TestCreatingAGroupMakesTheCreatorItsAdmin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	me := w.person(t)

	g, err := w.svc.Create(ctx, me, "  Νοσοκομείο   Αλεξανδρούπολης ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.Name != "Νοσοκομείο Αλεξανδρούπολης" || g.Role != "admin" || g.Members != 1 || len(g.Code) != 8 {
		t.Fatalf("got %+v", g)
	}
}

func TestAGroupNameThatHidesWhatItSaysIsRefused(t *testing.T) {
	for _, name := range []string{"", " ", "Α", "ΔΠΘ\u202e", "ΔΠ\x00Θ", string(make([]rune, 61))} {
		if _, err := groups.NormaliseName(name); !errors.Is(err, groups.ErrBadName) {
			t.Errorf("NormaliseName(%q) = %v, want ErrBadName", name, err)
		}
	}
}

// The code is what everybody else joins with, and a member joining twice is
// one membership: the second tap on a slow network is not a second person.
func TestJoiningWithTheCodeIsOneMembershipHoweverOftenItIsDone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	g, _ := w.svc.Create(ctx, w.person(t), "ΔΠΘ")
	me := w.person(t)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.svc.Join(ctx, me, " "+lower(g.Code)+" "); err != nil {
				t.Errorf("Join: %v", err)
			}
		}()
	}
	wg.Wait()

	mine, err := w.svc.Mine(ctx, me)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if len(mine) != 1 || mine[0].Members != 2 || mine[0].Role != "member" {
		t.Fatalf("got %+v", mine)
	}
	// A member is not shown the code: everybody who holds it can let anybody in.
	if mine[0].Code != "" {
		t.Errorf("a member was shown the code %q", mine[0].Code)
	}
}

// Wrong codes are counted, and past the ceiling a right one is refused too.
// If it were not, the limit would slow a search down and still tell it when it
// had found something.
func TestGuessingCodesStopsWorkingAfterTheDailyCeiling(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	g, _ := w.svc.Create(ctx, w.person(t), "ΔΠΘ")
	guesser := w.person(t)

	for i := 0; i < groups.FailuresPerDay; i++ {
		if _, err := w.svc.Join(ctx, guesser, "WRONGONE"); !errors.Is(err, groups.ErrCodeNotUsable) {
			t.Fatalf("guess %d: %v, want ErrCodeNotUsable", i, err)
		}
	}
	if _, err := w.svc.Join(ctx, guesser, g.Code); !errors.Is(err, groups.ErrTooManyAttempts) {
		t.Fatalf("the right code past the ceiling: %v, want ErrTooManyAttempts", err)
	}

	// A day later it is a new day.
	w.clock.Advance(25 * time.Hour)
	if _, err := w.svc.Join(ctx, guesser, g.Code); err != nil {
		t.Fatalf("a day later: %v", err)
	}
}

func TestANewCodeStopsTheOldOneAndKeepsEverybodyIn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	admin := w.person(t)
	g, _ := w.svc.Create(ctx, admin, "ΔΠΘ")
	member := w.person(t)
	if _, err := w.svc.Join(ctx, member, g.Code); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := w.svc.RotateCode(ctx, member, g.ID); !errors.Is(err, groups.ErrNotAdmin) {
		t.Fatalf("a member replacing the code: %v, want ErrNotAdmin", err)
	}
	if _, err := w.svc.RotateCode(ctx, w.person(t), g.ID); !errors.Is(err, groups.ErrNotMember) {
		t.Fatalf("an outsider replacing the code: %v, want ErrNotMember", err)
	}
	fresh, err := w.svc.RotateCode(ctx, admin, g.ID)
	if err != nil || fresh == g.Code {
		t.Fatalf("RotateCode = %q, %v", fresh, err)
	}

	if _, err := w.svc.Join(ctx, w.person(t), g.Code); !errors.Is(err, groups.ErrCodeNotUsable) {
		t.Fatalf("the old code: %v, want ErrCodeNotUsable", err)
	}
	if ok, _ := groups.IsMember(ctx, w.pool, member, g.ID); !ok {
		t.Fatal("the member already in was put out by the new code")
	}
}

// Leaving keeps the history, and an admin leaving hands the group on rather
// than leaving it with nobody who can see the code or pull the export.
func TestAnAdminLeavingHandsTheGroupToTheLongestStandingMember(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	admin := w.person(t)
	g, _ := w.svc.Create(ctx, admin, "ΔΠΘ")
	first, second := w.person(t), w.person(t)
	w.clock.Advance(time.Minute)
	_, _ = w.svc.Join(ctx, first, g.Code)
	w.clock.Advance(time.Minute)
	_, _ = w.svc.Join(ctx, second, g.Code)

	if err := w.svc.Leave(ctx, admin, g.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	mine, _ := w.svc.Mine(ctx, first)
	if len(mine) != 1 || mine[0].Role != "admin" || mine[0].Code == "" {
		t.Fatalf("the longest-standing member is not admin: %+v", mine)
	}
	if err := w.svc.Leave(ctx, admin, g.ID); !errors.Is(err, groups.ErrNotMember) {
		t.Fatalf("leaving twice: %v, want ErrNotMember", err)
	}

	var rows int
	_ = w.pool.QueryRow(ctx, `SELECT count(*) FROM group_members WHERE group_id = $1`, g.ID).Scan(&rows)
	if rows != 3 {
		t.Fatalf("membership rows = %d, want 3: leaving must end a membership, not erase it", rows)
	}

	// And coming back is a new membership beside the old one.
	if _, err := w.svc.Join(ctx, admin, mine[0].Code); err != nil {
		t.Fatalf("rejoining: %v", err)
	}
}

func TestTwoAdminsLeavingAtOnceStillLeaveOneBehind(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := w.person(t)
	g, _ := w.svc.Create(ctx, a, "ΔΠΘ")
	b, c := w.person(t), w.person(t)
	_, _ = w.svc.Join(ctx, b, g.Code)
	_, _ = w.svc.Join(ctx, c, g.Code)
	if _, err := w.pool.Exec(ctx, `UPDATE group_members SET role = 'admin' WHERE user_id = $1`, b); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, who := range []uuid.UUID{a, b} {
		wg.Add(1)
		go func(who uuid.UUID) {
			defer wg.Done()
			if err := w.svc.Leave(ctx, who, g.ID); err != nil {
				t.Errorf("Leave: %v", err)
			}
		}(who)
	}
	wg.Wait()

	mine, _ := w.svc.Mine(ctx, c)
	if len(mine) != 1 || mine[0].Role != "admin" {
		t.Fatalf("the member left behind is not admin: %+v", mine)
	}
}

func TestOnePersonCannotCollectGroups(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	me := w.person(t)
	for i := 0; i < groups.MaxGroupsPerPerson; i++ {
		if _, err := w.svc.Create(ctx, me, fmt.Sprintf("Ομάδα %d", i)); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	if _, err := w.svc.Create(ctx, me, "Μία ακόμα"); !errors.Is(err, groups.ErrTooManyGroups) {
		t.Fatalf("one too many: %v, want ErrTooManyGroups", err)
	}
}

func TestSharedNamesAreOnlyTheGroupsBothAreStillIn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a, b := w.person(t), w.person(t)
	both, _ := w.svc.Create(ctx, a, "ΔΠΘ")
	_, _ = w.svc.Join(ctx, b, both.Code)
	onlyA, _ := w.svc.Create(ctx, a, "Γυμναστήριο")
	_ = onlyA
	left, _ := w.svc.Create(ctx, a, "Ορειβατικός")
	_, _ = w.svc.Join(ctx, b, left.Code)
	_ = w.svc.Leave(ctx, b, left.ID)

	got, err := groups.SharedNames(ctx, w.pool, a, b)
	if err != nil {
		t.Fatalf("SharedNames: %v", err)
	}
	if len(got) != 1 || got[0] != "ΔΠΘ" {
		t.Fatalf("got %v, want [ΔΠΘ]", got)
	}
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}

// A group everybody left still has a code, and whoever uses it next becomes
// the one who can see it and replace it.
func TestJoiningAGroupWithNobodyLeftMakesYouItsAdmin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	admin := w.person(t)
	g, _ := w.svc.Create(ctx, admin, "ΔΠΘ")
	if err := w.svc.Leave(ctx, admin, g.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	joined, err := w.svc.Join(ctx, w.person(t), g.Code)
	if err != nil || joined.Role != "admin" || joined.Code != g.Code {
		t.Fatalf("Join = %+v, %v; want admin with the code", joined, err)
	}
}

// The last admin leaving while somebody joins: whichever order they land in,
// the group ends with an admin.
func TestAJoinRacingTheLastAdminLeavingStillLeavesAnAdmin(t *testing.T) {
	for i := 0; i < 5; i++ {
		w := newWorld(t)
		ctx := context.Background()
		admin := w.person(t)
		g, _ := w.svc.Create(ctx, admin, "ΔΠΘ")
		joiner := w.person(t)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = w.svc.Leave(ctx, admin, g.ID) }()
		go func() { defer wg.Done(); _, _ = w.svc.Join(ctx, joiner, g.Code) }()
		wg.Wait()

		mine, _ := w.svc.Mine(ctx, joiner)
		if len(mine) != 1 || mine[0].Role != "admin" {
			t.Fatalf("run %d: the joiner is %+v, want the group's admin", i, mine)
		}
	}
}
