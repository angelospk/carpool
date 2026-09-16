package store_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// A driver who has published nothing has no reason to publish anything, and the
// app never told them that three people are waiting on the road they drive
// every morning. The rows were already in ride_requests; nobody looked.
//
// The whole of what may leave the table is a corridor and a count. These tests
// are written around that rather than around the numbers being right, because a
// name or a departure time escaping here is the failure that cannot be taken
// back afterwards.

var demandNow = time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)

type demandWorld struct {
	pool   *pgxpool.Pool
	region uuid.UUID
	people int
}

func newDemandWorld(t *testing.T) *demandWorld {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &demandWorld{pool: pool}
	if err := pool.QueryRow(ctx, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&w.region); err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	return w
}

// passenger returns a new active user. Each one is a different person, which is
// the unit the count is in.
func (w *demandWorld) passenger(t *testing.T, status string) uuid.UUID {
	t.Helper()
	w.people++
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		INSERT INTO users (date_of_birth, phone, name, home_region_id, status)
		VALUES (DATE '1990-01-01', $1, $2, $3, $4) RETURNING id`,
		"+3069000000"+string(rune('0'+w.people%10))+string(rune('0'+w.people/10%10)),
		"Επιβάτης", w.region, status).Scan(&id); err != nil {
		t.Fatalf("seeding passenger: %v", err)
	}
	return id
}

type req struct {
	passenger  uuid.UUID
	from, to   string
	start, end time.Time
	status     string
}

func (w *demandWorld) request(t *testing.T, r req) uuid.UUID {
	t.Helper()
	if r.status == "" {
		r.status = "open"
	}
	if r.start.IsZero() {
		r.start = demandNow.Add(2 * time.Hour)
	}
	if r.end.IsZero() {
		r.end = demandNow.Add(4 * time.Hour)
	}
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests
		(passenger_id, origin, destination, origin_label, destination_label,
		 window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326),
		        $2, $3, $4, $5, 'open', $6)
		RETURNING id`, r.passenger, r.from, r.to, r.start, r.end, r.status).Scan(&id); err != nil {
		t.Fatalf("seeding request: %v", err)
	}
	return id
}

// crowd puts enough different people on a corridor to clear DemandFloor, so a
// test about some other property does not have to be about the floor as well.
func (w *demandWorld) crowd(t *testing.T, from, to string) {
	t.Helper()
	for i := 0; i < store.DemandFloor; i++ {
		w.request(t, req{passenger: w.passenger(t, "active"), from: from, to: to})
	}
}

// demand asks as a driver who has posted nothing, which is who the screen is
// for. Cases about the caller's own requests call store.Demand directly.
func demand(t *testing.T, w *demandWorld) []store.Corridor {
	t.Helper()
	viewer := w.passenger(t, "active")
	got, err := store.Demand(context.Background(), w.pool, viewer, demandNow, 7*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("Demand: %v", err)
	}
	return got
}

// The sentence this exists to make possible: "three people are looking for
// Αλεξανδρούπολη → Ορεστιάδα this week".
func TestDemandCountsThePeopleWaitingOnACorridor(t *testing.T) {
	w := newDemandWorld(t)
	for i := 0; i < 3; i++ {
		w.request(t, req{passenger: w.passenger(t, "active"), from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})
	}

	got := demand(t, w)
	if len(got) != 1 {
		t.Fatalf("got %d corridors, want 1: %+v", len(got), got)
	}
	if got[0].OriginLabel != "Αλεξανδρούπολη" || got[0].DestinationLabel != "Ορεστιάδα" {
		t.Errorf("corridor = %q → %q", got[0].OriginLabel, got[0].DestinationLabel)
	}
	if got[0].People != 3 {
		t.Errorf("people = %d, want 3", got[0].People)
	}
}

// People, not requests. Somebody who asks for the same road on Monday and again
// on Tuesday is one person looking for it, and counting them twice would tell a
// driver the corridor is busier than it is.
func TestDemandCountsPeopleRatherThanRequests(t *testing.T) {
	w := newDemandWorld(t)
	w.crowd(t, "Αλεξανδρούπολη", "Ορεστιάδα")
	// A fourth request from somebody who already asked once. Four rows, three
	// people.
	keen := w.passenger(t, "active")
	w.request(t, req{passenger: keen, from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})
	w.request(t, req{passenger: keen, from: "Αλεξανδρούπολη", to: "Ορεστιάδα",
		start: demandNow.Add(26 * time.Hour), end: demandNow.Add(28 * time.Hour)})

	got := demand(t, w)
	if len(got) != 1 {
		t.Fatalf("got %d corridors, want 1: %+v", len(got), got)
	}
	if got[0].People != store.DemandFloor+1 {
		t.Fatalf("people = %d, want %d: the repeat was counted twice",
			got[0].People, store.DemandFloor+1)
	}
}

// A request that has found its driver, been called off, or run out of time is
// not somebody waiting. Showing it would send a driver to publish for a journey
// that is already settled or already missed.
func TestDemandLeavesOutRequestsNobodyIsWaitingOn(t *testing.T) {
	w := newDemandWorld(t)
	for _, status := range []string{"matched", "cancelled", "expired"} {
		w.request(t, req{passenger: w.passenger(t, "active"),
			from: "Αλεξανδρούπολη", to: "Ορεστιάδα", status: status})
	}
	// Open, but the window closed an hour ago.
	w.request(t, req{passenger: w.passenger(t, "active"),
		from: "Αλεξανδρούπολη", to: "Ορεστιάδα",
		start: demandNow.Add(-3 * time.Hour), end: demandNow.Add(-1 * time.Hour)})

	if got := demand(t, w); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// A window beyond the period asked for is real demand, but not demand for this
// week, and the sentence on the screen says "this week".
func TestDemandStopsAtThePeriodAskedFor(t *testing.T) {
	w := newDemandWorld(t)
	w.crowd(t, "Αλεξανδρούπολη", "Ορεστιάδα")
	for i := 0; i < store.DemandFloor; i++ {
		w.request(t, req{passenger: w.passenger(t, "active"),
			from: "Αλεξανδρούπολη", to: "Σουφλί",
			start: demandNow.Add(30 * 24 * time.Hour), end: demandNow.Add(31 * 24 * time.Hour)})
	}

	got := demand(t, w)
	if len(got) != 1 || got[0].DestinationLabel != "Ορεστιάδα" {
		t.Fatalf("got %+v, want only the corridor inside the period", got)
	}
}

// A suspended account's request stays in the table until the cascade withdraws
// it. Counting it would advertise demand that cannot be served by anybody.
func TestDemandLeavesOutPeopleWhoCannotTravel(t *testing.T) {
	w := newDemandWorld(t)
	w.request(t, req{passenger: w.passenger(t, "suspended"), from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})

	if got := demand(t, w); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// The busiest road first: with a limit in play this decides what a driver is
// told about at all.
func TestDemandPutsTheBusiestCorridorFirst(t *testing.T) {
	w := newDemandWorld(t)
	w.crowd(t, "Αλεξανδρούπολη", "Σουφλί")
	w.crowd(t, "Αλεξανδρούπολη", "Ορεστιάδα")
	// One more on Ορεστιάδα, so the two corridors are both above the floor and
	// the ordering is what decides which comes first.
	w.request(t, req{passenger: w.passenger(t, "active"),
		from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})

	got := demand(t, w)
	if len(got) != 2 {
		t.Fatalf("got %d corridors, want 2: %+v", len(got), got)
	}
	if got[0].DestinationLabel != "Ορεστιάδα" || got[1].DestinationLabel != "Σουφλί" {
		t.Errorf("order = %q then %q", got[0].DestinationLabel, got[1].DestinationLabel)
	}
}

// A request posted before labels were recorded has nothing a driver could read.
// It is real demand and it is unreportable, and an empty arrow on the screen is
// worse than one fewer line.
func TestDemandLeavesOutCorridorsWithNoName(t *testing.T) {
	w := newDemandWorld(t)
	w.request(t, req{passenger: w.passenger(t, "active"), from: "", to: "Ορεστιάδα"})

	if got := demand(t, w); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// The privacy contract, asserted against the type rather than against a
// sentence in a comment. Demand is the one place in this codebase where one
// person's stated journey is shown to somebody who is not part of it, so the
// only defensible answer is that nothing identifying can be in the struct to
// leak: no passenger id, no name, no departure time, no coordinates.
//
// A field added here is a field that reaches every signed-in user.
func TestACorridorCarriesNothingThatNamesAnybody(t *testing.T) {
	want := map[string]bool{"OriginLabel": true, "DestinationLabel": true, "People": true}

	got := reflect.TypeOf(store.Corridor{})
	for i := 0; i < got.NumField(); i++ {
		if !want[got.Field(i).Name] {
			t.Errorf("Corridor has an unexpected field %q: demand must carry a corridor and a count",
				got.Field(i).Name)
		}
	}
	if got.NumField() != len(want) {
		t.Errorf("Corridor has %d fields, want %d", got.NumField(), len(want))
	}
}

// Your own request is not a reason for you to publish a route.
//
// "3 άνθρωποι ψάχνουν Αλεξανδρούπολη → Ορεστιάδα" with yourself as one of the
// three is an encouragement built partly out of your own demand, which is the
// one number on that screen nobody should have to discount in their head. It
// also quietly tells a user with a single request on a quiet corridor exactly
// which of the figures is theirs.
func TestDemandLeavesOutTheCallersOwnRequests(t *testing.T) {
	w := newDemandWorld(t)
	me := w.passenger(t, "active")
	w.request(t, req{passenger: me, from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})
	w.crowd(t, "Αλεξανδρούπολη", "Ορεστιάδα")

	got, err := store.Demand(context.Background(), w.pool, me, demandNow, 7*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("Demand: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d corridors, want 1: %+v", len(got), got)
	}
	if got[0].People != store.DemandFloor {
		t.Errorf("people = %d, want %d: the caller's own request was counted",
			got[0].People, store.DemandFloor)
	}
}

// And a corridor where the caller is the only one waiting disappears entirely
// rather than reading as a road somebody else wants.
func TestACorridorOnlyTheCallerWantsIsNotDemand(t *testing.T) {
	w := newDemandWorld(t)
	me := w.passenger(t, "active")
	w.request(t, req{passenger: me, from: "Αλεξανδρούπολη", to: "Ορεστιάδα"})

	got, err := store.Demand(context.Background(), w.pool, me, demandNow, 7*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("Demand: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// The privacy floor, and the reason it is a floor rather than a preference.
//
// A corridor is two settlement names, and in a region of villages "one person
// wants Πέπλος -> Ορεστιάδα this week" is close to naming them. Worse, the
// endpoint can be polled: a count that moves from two to three tells a watcher
// that somebody new is travelling, and a count that appears at all tells them
// the corridor became busy this week. Counts alone do not protect anybody once
// they can be watched over time.
//
// So a corridor is reported only once enough different people are waiting on it
// that the line is about a road rather than about a person. This is the number
// that decides whether the feature says anything at all in a cold-start market,
// and it is set on the safe side deliberately: a driver told nothing has lost
// nothing, and a passenger identified cannot be un-identified.
func TestACorridorIsNotReportedUntilEnoughPeopleWantIt(t *testing.T) {
	for people := 1; people <= store.DemandFloor+1; people++ {
		t.Run(fmt.Sprintf("%d waiting", people), func(t *testing.T) {
			w := newDemandWorld(t)
			for i := 0; i < people; i++ {
				w.request(t, req{passenger: w.passenger(t, "active"),
					from: "Πέπλος", to: "Ορεστιάδα"})
			}

			got := demand(t, w)
			if people < store.DemandFloor {
				if len(got) != 0 {
					t.Fatalf("%d people produced %+v, want nothing below a floor of %d",
						people, got, store.DemandFloor)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("%d people produced %d corridors, want 1", people, len(got))
			}
			if got[0].People != people {
				t.Errorf("people = %d, want %d", got[0].People, people)
			}
		})
	}
}

// The floor is counted on the people who are actually reported, which is the
// people other than whoever is asking. Otherwise a driver who is also waiting
// on the road pushes it over the line with themselves.
func TestTheFloorDoesNotCountTheCallerTowardsItself(t *testing.T) {
	w := newDemandWorld(t)
	me := w.passenger(t, "active")
	w.request(t, req{passenger: me, from: "Πέπλος", to: "Ορεστιάδα"})
	for i := 0; i < store.DemandFloor-1; i++ {
		w.request(t, req{passenger: w.passenger(t, "active"), from: "Πέπλος", to: "Ορεστιάδα"})
	}

	got, err := store.Demand(context.Background(), w.pool, me, demandNow, 7*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("Demand: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v: the caller was counted towards the floor", got)
	}
}
