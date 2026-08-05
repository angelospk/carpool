package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/places"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// Αλεξανδρούπολη, whose harvested points are the ones worth correcting: two
// urban bus-line termini and a port halt, and no ΚΤΕΛ.
const alexandroupoli = "node/139260064"

func pointsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Fresh(t)
	if err := store.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pool
}

func names(points []places.MeetingPoint) []string {
	out := make([]string, 0, len(points))
	for _, p := range points {
		out = append(out, p.Name)
	}
	return out
}

// Nothing overridden, nothing changed. The harvested list is the answer and its
// order is the answer's order.
func TestWithNoOverridesTheHarvestedListIsReturnedAsItIs(t *testing.T) {
	pool := pointsPool(t)

	got, err := store.MeetingPointsFor(context.Background(), pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}

	want := places.PointsFor(alexandroupoli)
	if len(got) != len(want) {
		t.Fatalf("got %v, want the harvested %v", names(got), names(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("position %d is %q, want %q: the order is the ranking", i, got[i].Name, want[i].Name)
		}
	}
}

func hide(t *testing.T, pool *pgxpool.Pool, place, target string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO meeting_point_overrides (target_id, place_id, action)
		VALUES ($1, $2, 'hide')`, target, place); err != nil {
		t.Fatalf("hiding %s: %v", target, err)
	}
}

// A point somebody has to be able to take away: a ticket agency tagged as a bus
// station is worse than nothing, because it looks like an answer.
func TestAHiddenPointIsGoneAndOnlyThatOne(t *testing.T) {
	pool := pointsPool(t)
	harvested := places.PointsFor(alexandroupoli)
	if len(harvested) < 2 {
		t.Skip("this settlement has too few points to prove the 'only that one' half")
	}

	hide(t, pool, alexandroupoli, harvested[0].ID)

	got, err := store.MeetingPointsFor(context.Background(), pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if len(got) != len(harvested)-1 {
		t.Fatalf("got %v, want everything but %q", names(got), harvested[0].Name)
	}
	for _, p := range got {
		if p.ID == harvested[0].ID {
			t.Errorf("%q is still there", p.Name)
		}
	}
}

// Renaming keeps the point's identity. A moderator fixing "Τ.Σ. Ν.Σ. ΣΤΑΘΜΟΣ"
// is correcting a label, not replacing a place, and anything already pointing
// at it must still find it.
func TestARenamedPointKeepsItsIdentity(t *testing.T) {
	pool := pointsPool(t)
	harvested := places.PointsFor(alexandroupoli)

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO meeting_point_overrides (target_id, place_id, action, name, kind)
		VALUES ($1, $2, 'update', 'ΚΤΕΛ Έβρου', 'bus_station')`,
		harvested[0].ID, alexandroupoli); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	got, err := store.MeetingPointsFor(context.Background(), pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if got[0].ID != harvested[0].ID {
		t.Errorf("id changed from %q to %q", harvested[0].ID, got[0].ID)
	}
	if got[0].Name != "ΚΤΕΛ Έβρου" {
		t.Errorf("name = %q, want the corrected one", got[0].Name)
	}
	// The coordinate was not part of the correction, so it stays.
	if got[0].Lon != harvested[0].Lon {
		t.Errorf("the coordinate moved without being asked to")
	}
}

// The reason a moderator would add anything at all: a city with three bad
// automatic points needs the right one above them, not behind them.
func TestACuratedPointCanBePutAboveTheAutomaticOnes(t *testing.T) {
	pool := pointsPool(t)

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO meeting_point_overrides (target_id, place_id, action, name, kind, point, priority)
		VALUES ('curated/aaaa', $1, 'add', 'ΚΤΕΛ Αλεξανδρούπολης', 'bus_station',
		        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326), -1)`, alexandroupoli); err != nil {
		t.Fatalf("adding: %v", err)
	}

	got, err := store.MeetingPointsFor(context.Background(), pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if got[0].Name != "ΚΤΕΛ Αλεξανδρούπολης" {
		t.Errorf("first is %q, want the curated one: %v", got[0].Name, names(got))
	}
	if got[0].ID != "curated/aaaa" {
		t.Errorf("id = %q, want a curated identity that cannot collide with OSM", got[0].ID)
	}
}

// A curated point is a point like any other, so it can be taken away again.
// The single-table design could not express this, which is why there are two.
func TestACuratedPointCanBeHiddenLater(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO meeting_point_overrides (target_id, place_id, action, name, kind, point)
		VALUES ('curated/bbbb', $1, 'add', 'Κάπου', 'other',
		        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326))`, alexandroupoli); err != nil {
		t.Fatalf("adding: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE meeting_point_overrides SET action = 'hide', name = NULL, kind = NULL, point = NULL
		WHERE target_id = 'curated/bbbb'`); err != nil {
		t.Fatalf("hiding: %v", err)
	}

	got, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	for _, p := range got {
		if p.ID == "curated/bbbb" {
			t.Error("a curated point that was hidden is still being offered")
		}
	}
}

// An override belongs to one settlement. Applying it anywhere else would hide a
// point in a town nobody was looking at.
func TestAnOverrideDoesNotLeakIntoAnotherSettlement(t *testing.T) {
	pool := pointsPool(t)
	const orestiada = "node/299263482"
	harvested := places.PointsFor(orestiada)
	if len(harvested) == 0 {
		t.Skip("Ορεστιάδα has no harvested points")
	}

	hide(t, pool, alexandroupoli, harvested[0].ID)

	got, err := store.MeetingPointsFor(context.Background(), pool, orestiada)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if len(got) != len(harvested) {
		t.Errorf("hiding a point in Αλεξανδρούπολη changed Ορεστιάδα: %v", names(got))
	}
}

// A settlement with nothing harvested still gets nothing. This is the promise
// the whole dataset is built around and an override table is exactly the sort
// of thing that would quietly break it.
func TestASettlementWithNothingStillGetsNothing(t *testing.T) {
	pool := pointsPool(t)

	got, err := store.MeetingPointsFor(context.Background(), pool, "node/1")
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("invented %v for a settlement with no landmark", names(got))
	}
}

// If the database cannot be read, the answer is an error rather than the
// harvested list. Falling back would put a point somebody removed for a reason
// back in front of people, at exactly the moment nobody is watching.
func TestADatabaseFailureDoesNotResurrectAHiddenPoint(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)
	harvested := places.PointsFor(alexandroupoli)
	hide(t, pool, alexandroupoli, harvested[0].ID)

	pool.Close() // the database is now unreachable

	got, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err == nil {
		t.Fatalf("got %v with no error; a failure must not fall back to the harvested list", names(got))
	}
	if got != nil {
		t.Errorf("returned %v alongside the error", names(got))
	}
}

// The cap is applied to the merged list, so curating a city does not quietly
// give it more points than a city is allowed.
func TestTheCapCountsCuratedPointsToo(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)

	for i, id := range []string{"curated/c1", "curated/c2", "curated/c3", "curated/c4"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO meeting_point_overrides (target_id, place_id, action, name, kind, point, priority)
			VALUES ($1, $2, 'add', $3, 'other',
			        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326), $4)`,
			id, alexandroupoli, "Σημείο "+id, -10+i); err != nil {
			t.Fatalf("adding %s: %v", id, err)
		}
	}

	got, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if len(got) > store.MaxMeetingPoints {
		t.Errorf("returned %d points, above the cap of %d: %v", len(got), store.MaxMeetingPoints, names(got))
	}
}

func TestModeratorGrantsAreRecorded(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)
	_, userID := seedDriver(t, pool, "+306900000701")

	if err := store.SetModerator(ctx, pool, userID, true, uuid.Nil, "the first one"); err != nil {
		t.Fatalf("SetModerator: %v", err)
	}

	var is bool
	if err := pool.QueryRow(ctx, `SELECT is_moderator FROM users WHERE id = $1`, userID).Scan(&is); err != nil {
		t.Fatalf("reading the flag: %v", err)
	}
	if !is {
		t.Error("the flag was not set")
	}

	var granted bool
	var note string
	if err := pool.QueryRow(ctx,
		`SELECT granted, note FROM moderator_grants WHERE user_id = $1`, userID).Scan(&granted, &note); err != nil {
		t.Fatalf("no grant was recorded: %v", err)
	}
	if !granted || note != "the first one" {
		t.Errorf("recorded granted=%v note=%q", granted, note)
	}
}

// Revoking has to be recorded too, and has to take effect at once: the flag is
// read on every moderated request rather than carried in a token.
func TestRevokingIsRecordedAndImmediate(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)
	_, userID := seedDriver(t, pool, "+306900000702")

	if err := store.SetModerator(ctx, pool, userID, true, uuid.Nil, ""); err != nil {
		t.Fatalf("granting: %v", err)
	}
	if err := store.SetModerator(ctx, pool, userID, false, uuid.Nil, "no longer"); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	var is bool
	if err := pool.QueryRow(ctx, `SELECT is_moderator FROM users WHERE id = $1`, userID).Scan(&is); err != nil {
		t.Fatalf("reading the flag: %v", err)
	}
	if is {
		t.Error("still a moderator")
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM moderator_grants WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("counting grants: %v", err)
	}
	if n != 2 {
		t.Errorf("%d grant records, want both the grant and the revocation", n)
	}
}

// A correction that fixes a name must not wipe the coordinate somebody set last
// week. The upsert used to overwrite every column with whatever the latest
// request happened to carry.
func TestACorrectionThatNamesOneThingKeepsTheRest(t *testing.T) {
	ctx := context.Background()
	pool := pointsPool(t)
	_, mod := seedDriver(t, pool, "+306900000720")

	if err := store.ApplyModeratorEdit(ctx, pool, store.ModeratorEdit{
		PlaceID: alexandroupoli, Action: "add", Name: "ΚΤΕΛ Έβρου",
		Kind: "bus_station", Lon: 25.8600, Lat: 40.8600, Priority: -1, By: mod,
	}); err != nil {
		t.Fatalf("adding: %v", err)
	}

	added, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if added[0].Name != "ΚΤΕΛ Έβρου" || added[0].Lon == 0 {
		t.Fatalf("the fixture is wrong: %+v", added[0])
	}

	// Now only the name, as somebody fixing a typo would send.
	if err := store.ApplyModeratorEdit(ctx, pool, store.ModeratorEdit{
		PlaceID: alexandroupoli, Action: "update", TargetID: added[0].ID,
		Name: "ΚΤΕΛ Αλεξανδρούπολης", Priority: -1, By: mod,
	}); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	got, err := store.MeetingPointsFor(ctx, pool, alexandroupoli)
	if err != nil {
		t.Fatalf("MeetingPointsFor: %v", err)
	}
	if got[0].Name != "ΚΤΕΛ Αλεξανδρούπολης" {
		t.Errorf("name = %q, want the corrected one", got[0].Name)
	}
	if got[0].Lon == 0 || got[0].Kind == "" {
		t.Errorf("fixing the name wiped the rest: %+v", got[0])
	}
}
