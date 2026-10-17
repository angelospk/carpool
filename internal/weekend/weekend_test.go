package weekend_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/match"
	"carpool/internal/notices"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
	"carpool/internal/weekend"
)

var athens = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		panic(err)
	}
	return loc
}()

type world struct {
	pool      *pgxpool.Pool
	clock     *clock.Fake
	svc       *weekend.Service
	passenger uuid.UUID
}

// newWorld starts on Wednesday 29 July 2026, 12:00 Athens: the coming weekend
// is the one beginning Friday 31 July.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, clock: clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, athens))}
	w.svc = weekend.NewService(pool, w.clock, athens)

	var region uuid.UUID
	must(t, pool.QueryRow(ctx, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&region))
	w.passenger = w.user(t, "+306900000001", region)
	return w
}

func (w *world) user(t *testing.T, phone string, region uuid.UUID) uuid.UUID {
	t.Helper()
	if region == uuid.Nil {
		must(t, w.pool.QueryRow(context.Background(), `SELECT id FROM regions LIMIT 1`).Scan(&region))
	}
	var id uuid.UUID
	must(t, w.pool.QueryRow(context.Background(), `INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Someone', $2) RETURNING id`, phone, region).Scan(&id))
	return id
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

// plan is «από Παρασκευή 15:00, πίσω Κυριακή 18:00», every week.
func fridayToSunday() weekend.Plan {
	return weekend.Plan{
		OriginLon: 25.82, OriginLat: 40.85, DestinationLon: 25.98, DestinationLat: 40.95,
		OriginLabel: "Αλεξανδρούπολη", DestinationLabel: "Θεσσαλονίκη",
		DepartDow: 5, DepartTime: "15:00",
		ReturnDow: ptr(0), ReturnTime: ptr("18:00"),
		Seats: 1, Luggage: "none", EveryWeek: true,
	}
}

func (w *world) create(t *testing.T, p weekend.Plan) uuid.UUID {
	t.Helper()
	got, err := w.svc.Create(context.Background(), w.passenger, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return got.ID
}

type req struct {
	ID          uuid.UUID
	Leg         string
	Start, End  time.Time
	Status      string
	AlternateOf *uuid.UUID
	WeekendOf   time.Time
}

func (w *world) requests(t *testing.T, plan uuid.UUID) []req {
	t.Helper()
	rows, err := w.pool.Query(context.Background(), `
		SELECT id, weekend_leg, window_start, window_end, status, alternative_of, weekend_of
		  FROM ride_requests WHERE weekend_plan_id = $1 ORDER BY weekend_leg DESC, window_start`, plan)
	must(t, err)
	defer rows.Close()
	var out []req
	for rows.Next() {
		var r req
		must(t, rows.Scan(&r.ID, &r.Leg, &r.Start, &r.End, &r.Status, &r.AlternateOf, &r.WeekendOf))
		out = append(out, r)
	}
	must(t, rows.Err())
	return out
}

func local(t time.Time) string { return t.In(athens).Format("Mon 2006-01-02 15:04") }

func TestCreateRefusesWhatIsNotAWeekendPlan(t *testing.T) {
	w := newWorld(t)
	cases := map[string]func(*weekend.Plan){
		"a Wednesday outbound":      func(p *weekend.Plan) { p.DepartDow = 3 },
		"a day beyond the week":     func(p *weekend.Plan) { p.DepartDow = 7 },
		"an unreadable time":        func(p *weekend.Plan) { p.DepartTime = "ΣΚ" },
		"a return before leaving":   func(p *weekend.Plan) { p.DepartDow, p.ReturnDow = 6, ptr(5) },
		"a return earlier same day": func(p *weekend.Plan) { p.ReturnDow, p.ReturnTime = ptr(5), ptr("14:00") },
		"a return day with no time": func(p *weekend.Plan) { p.ReturnTime = nil },
		"a return time with no day": func(p *weekend.Plan) { p.ReturnDow = nil },
		"no seats":                  func(p *weekend.Plan) { p.Seats = 0 },
		"five seats":                func(p *weekend.Plan) { p.Seats = 5 },
		"luggage of an unknown size": func(p *weekend.Plan) {
			p.Luggage, p.LuggageCount = "piano", 1
		},
		"a suitcase counted as none":   func(p *weekend.Plan) { p.Luggage, p.LuggageCount = "suitcase", 0 },
		"nothing counted as two":       func(p *weekend.Plan) { p.Luggage, p.LuggageCount = "none", 2 },
		"no pickup name":               func(p *weekend.Plan) { p.OriginLabel = "  " },
		"a destination outside Greece": func(p *weekend.Plan) { p.DestinationLon, p.DestinationLat = 10, 50 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := fridayToSunday()
			mutate(&p)
			_, err := w.svc.Create(context.Background(), w.passenger, p)
			var invalid *weekend.InvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("Create = %v, want an *InvalidError", err)
			}
		})
	}
	var n int
	must(t, w.pool.QueryRow(context.Background(), `SELECT count(*) FROM weekend_plans`).Scan(&n))
	if n != 0 {
		t.Fatalf("%d plans stored after refusals", n)
	}
}

func TestGenerateMakesTheComingWeekendOnce(t *testing.T) {
	w := newWorld(t)
	plan := w.create(t, fridayToSunday())
	// Create leaves the work to the job, or has already done it: either way
	// the hourly pass must leave exactly one set, and a second must add none.
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	again, err := w.svc.Generate(context.Background(), w.clock.Now())
	if err != nil {
		t.Fatalf("Generate again: %v", err)
	}
	if again != 0 {
		t.Errorf("a second pass made %d requests", again)
	}

	got := w.requests(t, plan)
	want := []struct{ leg, start, end string }{
		{"out", "Fri 2026-07-31 15:00", "Fri 2026-07-31 23:59"},
		{"out", "Sat 2026-08-01 06:00", "Sat 2026-08-01 23:59"},
		{"out", "Sun 2026-08-02 06:00", "Sun 2026-08-02 23:59"},
		{"back", "Sun 2026-08-02 18:00", "Sun 2026-08-02 23:59"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d requests, want %d: %+v", len(got), len(want), got)
	}
	// ORDER BY leg DESC puts 'out' first, then 'back'.
	for i, r := range got {
		if r.Leg != want[i].leg || local(r.Start) != want[i].start || local(r.End) != want[i].end {
			t.Errorf("request %d is %s %s - %s, want %s %s - %s", i,
				r.Leg, local(r.Start), local(r.End), want[i].leg, want[i].start, want[i].end)
		}
		if r.Status != "open" || r.WeekendOf.Format("2006-01-02") != "2026-07-31" {
			t.Errorf("request %d is %s for the weekend of %s", i, r.Status, r.WeekendOf)
		}
	}
	root := got[0].ID
	if got[0].AlternateOf != nil {
		t.Errorf("the first day is an alternative of something")
	}
	for _, r := range got[1:3] {
		if r.AlternateOf == nil || *r.AlternateOf != root {
			t.Errorf("%s is not an alternative of the first outbound day", local(r.Start))
		}
	}
	if got[3].AlternateOf != nil {
		t.Errorf("the return is an alternative of something")
	}

	// The request is the plan's, in every respect the ordinary form fills.
	var kind string
	var seats int
	var swapped bool
	must(t, w.pool.QueryRow(context.Background(), `
		SELECT kind, seats, ST_X(origin) = 25.98
		  FROM ride_requests WHERE id = $1`, got[3].ID).Scan(&kind, &seats, &swapped))
	if kind != "open" || seats != 1 || !swapped {
		t.Errorf("return request: kind %s seats %d swapped %v; it goes the other way", kind, seats, swapped)
	}
}

func TestGenerateAcrossTheEndOfSummerTime(t *testing.T) {
	w := newWorld(t)
	// Friday 23 October 2026; the clocks go back at 04:00 on Sunday the 25th.
	w.clock.Set(time.Date(2026, time.October, 21, 12, 0, 0, 0, athens))
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	got := w.requests(t, plan)
	if len(got) != 4 {
		t.Fatalf("%d requests", len(got))
	}
	if local(got[0].Start) != "Fri 2026-10-23 15:00" || local(got[2].Start) != "Sun 2026-10-25 06:00" ||
		local(got[3].Start) != "Sun 2026-10-25 18:00" {
		t.Errorf("wall times drifted: %s, %s, %s", local(got[0].Start), local(got[2].Start), local(got[3].Start))
	}
}

func TestTheComingWeekendIsTheOneNotOverYet(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		of   string
		days int
	}{
		{"Saturday evening is still this weekend", time.Date(2026, time.August, 1, 17, 0, 0, 0, athens), "2026-07-31", 2},
		{"Sunday night is still this weekend", time.Date(2026, time.August, 2, 22, 0, 0, 0, athens), "2026-07-31", 1},
		{"Monday is the next one", time.Date(2026, time.August, 3, 0, 30, 0, 0, athens), "2026-08-07", 3},
		{"Friday morning is this one", time.Date(2026, time.July, 31, 9, 0, 0, 0, athens), "2026-07-31", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			p := fridayToSunday()
			p.ReturnDow, p.ReturnTime = nil, nil
			w.clock.Set(time.Date(2026, time.July, 20, 12, 0, 0, 0, athens))
			plan := w.create(t, p)
			w.clock.Set(tc.now)
			if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
				t.Fatal(err)
			}
			got := w.requests(t, plan)
			// Days already over are not asked for, so the Saturday evening
			// pass has only the Saturday-night and Sunday windows left.
			var fresh []req
			for _, r := range got {
				if r.WeekendOf.Format("2006-01-02") == tc.of && r.End.After(tc.now) {
					fresh = append(fresh, r)
				}
			}
			if len(fresh) != tc.days {
				t.Errorf("%d live requests for the weekend of %s, want %d: %+v", len(fresh), tc.of, tc.days, got)
			}
		})
	}
}

func TestOnceOnlyPlansEndWithTheirWeekend(t *testing.T) {
	w := newWorld(t)
	p := fridayToSunday()
	p.EveryWeek = false
	plan := w.create(t, p)
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	before := len(w.requests(t, plan))

	w.clock.Set(time.Date(2026, time.August, 2, 23, 0, 0, 0, athens))
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if active := w.active(t, plan); !active {
		t.Fatal("the plan ended while its weekend was still on")
	}

	w.clock.Set(time.Date(2026, time.August, 3, 0, 5, 0, 0, athens))
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if w.active(t, plan) {
		t.Error("a once-only plan is still active after its weekend")
	}
	if after := len(w.requests(t, plan)); after != before {
		t.Errorf("%d requests became %d: the finished plan made more", before, after)
	}
}

func (w *world) active(t *testing.T, plan uuid.UUID) bool {
	t.Helper()
	var a bool
	must(t, w.pool.QueryRow(context.Background(), `SELECT active FROM weekend_plans WHERE id = $1`, plan).Scan(&a))
	return a
}

func TestEveryWeekPlansGoOnToTheNextWeekend(t *testing.T) {
	w := newWorld(t)
	plan := w.create(t, fridayToSunday())
	ctx := context.Background()
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	w.clock.Set(time.Date(2026, time.August, 3, 0, 5, 0, 0, athens))
	n, err := w.svc.Generate(ctx, w.clock.Now())
	if err != nil || n != 4 {
		t.Fatalf("Generate = %d, %v; want the next weekend's four requests", n, err)
	}
	if !w.active(t, plan) {
		t.Error("an every-week plan went inactive")
	}
}

func TestNoRequestsForAStoppedPlanOrASuspendedPassenger(t *testing.T) {
	w := newWorld(t)
	stopped := w.create(t, fridayToSunday())
	if err := w.svc.Stop(context.Background(), w.passenger, stopped); err != nil {
		t.Fatal(err)
	}
	other := w.user(t, "+306900000002", uuid.Nil)
	p, err := w.svc.Create(context.Background(), other, fridayToSunday())
	must(t, err)
	must(t, func() error {
		_, err := w.pool.Exec(context.Background(), `UPDATE users SET status = 'suspended' WHERE id = $1`, other)
		return err
	}())
	// Whatever Create made on the spot is the suspended passenger's own
	// business; what matters is that the job adds nothing for either.
	var before int
	must(t, w.pool.QueryRow(context.Background(), `SELECT count(*) FROM ride_requests`).Scan(&before))
	if _, err := w.svc.Generate(context.Background(), w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	var after int
	must(t, w.pool.QueryRow(context.Background(), `SELECT count(*) FROM ride_requests`).Scan(&after))
	_ = p
	if after != before {
		t.Errorf("the job made %d requests for people who are not travelling", after-before)
	}
}

func TestStopCancelsWhatIsStillLookingAndLeavesWhatIsBooked(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	got := w.requests(t, plan)
	matched, asking := got[3].ID, got[0].ID
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'matched' WHERE id = $1`, matched); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'asking' WHERE id = $1`, asking); err != nil {
		t.Fatal(err)
	}
	occ := w.departure(t)
	if _, err := w.pool.Exec(ctx, `INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		pickup_point, dropoff_point, suggested_price_eur, itinerary_version, status, expires_at, offered_by)
		VALUES ($1, $2, 1, 1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		2.5, 0, 'pending', $3, 'driver')`, asking, occ, w.clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := w.svc.Stop(ctx, w.passenger, plan); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if w.active(t, plan) {
		t.Error("the plan is still active")
	}
	for _, r := range w.requests(t, plan) {
		want := "cancelled"
		if r.ID == matched {
			want = "matched"
		}
		if r.Status != want {
			t.Errorf("request %s is %s, want %s", local(r.Start), r.Status, want)
		}
	}
	var pending, wakes int
	must(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM asks WHERE request_id = $1 AND status = 'pending'`, asking).Scan(&pending))
	must(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&wakes))
	if pending != 0 || wakes != 1 {
		t.Errorf("%d offers still pending and %d wakes for the freed seat, want 0 and 1", pending, wakes)
	}
	// And it is gone from the list, and the job leaves it alone.
	list, err := w.svc.List(ctx, w.passenger)
	must(t, err)
	if len(list) != 0 {
		t.Errorf("a stopped plan is still listed")
	}
	// Stopping again is the same answer.
	if err := w.svc.Stop(ctx, w.passenger, plan); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func (w *world) departure(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	driver := w.user(t, "+306900000099", uuid.Nil)
	_, err := w.pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`, driver)
	must(t, err)
	testdb.GiveEveryDriverACar(t, w.pool)
	var template, occ uuid.UUID
	must(t, w.pool.QueryRow(ctx, `INSERT INTO route_templates (driver_id, vehicle_id, origin, destination,
		origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
		'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, driver).Scan(&template))
	must(t, w.pool.QueryRow(ctx, `INSERT INTO trip_occurrences (template_id, vehicle_id, service_date,
		depart_earliest, depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
		route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-31',
		'2026-07-31T12:00:00Z', '2026-07-31T13:00:00Z', 3, 3, 1,
		ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, template).Scan(&occ))
	return occ
}

func TestStopAndThisWeekAnswerNotFoundForSomebodyElsesPlan(t *testing.T) {
	w := newWorld(t)
	plan := w.create(t, fridayToSunday())
	stranger := w.user(t, "+306900000003", uuid.Nil)
	if err := w.svc.Stop(context.Background(), stranger, plan); !errors.Is(err, weekend.ErrNotFound) {
		t.Errorf("Stop by a stranger = %v, want ErrNotFound", err)
	}
	if _, err := w.svc.ThisWeek(context.Background(), stranger, plan, false); !errors.Is(err, weekend.ErrNotFound) {
		t.Errorf("ThisWeek by a stranger = %v, want ErrNotFound", err)
	}
	if err := w.svc.Stop(context.Background(), w.passenger, uuid.New()); !errors.Is(err, weekend.ErrNotFound) {
		t.Errorf("Stop of nothing = %v, want ErrNotFound", err)
	}
	if !w.active(t, plan) {
		t.Error("a stranger stopped the plan")
	}
}

func TestThisWeekNoCancelsAndCountsAndThreeAsksToStop(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}

	got, err := w.svc.ThisWeek(ctx, w.passenger, plan, false)
	must(t, err)
	if got.ThisWeek != "cancelled" || got.DeclinesInRow != 1 || got.AskToStop {
		t.Errorf("after one no: %q, %d in a row, ask_to_stop %v", got.ThisWeek, got.DeclinesInRow, got.AskToStop)
	}
	for _, r := range w.requests(t, plan) {
		if r.Status != "cancelled" {
			t.Errorf("request %s is %s after saying no", local(r.Start), r.Status)
		}
	}

	// The same answer twice is one answer.
	got, err = w.svc.ThisWeek(ctx, w.passenger, plan, false)
	must(t, err)
	if got.DeclinesInRow != 1 {
		t.Errorf("pressing no twice counted %d", got.DeclinesInRow)
	}

	// Two more weekends of no.
	for _, day := range []int{3, 10} {
		w.clock.Set(time.Date(2026, time.August, day, 12, 0, 0, 0, athens))
		if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
			t.Fatal(err)
		}
		got, err = w.svc.ThisWeek(ctx, w.passenger, plan, false)
		must(t, err)
	}
	if got.DeclinesInRow != 3 || !got.AskToStop {
		t.Errorf("after three: %d in a row, ask_to_stop %v", got.DeclinesInRow, got.AskToStop)
	}

	// One yes sets it back.
	got, err = w.svc.ThisWeek(ctx, w.passenger, plan, true)
	must(t, err)
	if got.DeclinesInRow != 0 || got.AskToStop {
		t.Errorf("after yes: %d in a row, ask_to_stop %v", got.DeclinesInRow, got.AskToStop)
	}
}

func TestThisWeekNoLeavesABookedSeatAlone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	got := w.requests(t, plan)
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'matched' WHERE id = $1`, got[3].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.ThisWeek(ctx, w.passenger, plan, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range w.requests(t, plan) {
		if r.ID == got[3].ID && r.Status != "matched" {
			t.Errorf("the booked return became %s", r.Status)
		}
	}
}

func TestListShowsTheStateOfTheComingWeekend(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())

	state := func() string {
		list, err := w.svc.List(ctx, w.passenger)
		must(t, err)
		if len(list) != 1 || list[0].ID != plan {
			t.Fatalf("List = %+v", list)
		}
		return list[0].ThisWeek
	}
	// Create has made the requests, or the job has not run yet; make it so.
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != "open" {
		t.Errorf("fresh plan: %q, want open", got)
	}
	reqs := w.requests(t, plan)
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'matched' WHERE id = $1`, reqs[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != "matched" {
		t.Errorf("one day booked: %q, want matched", got)
	}
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'cancelled'`); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != "cancelled" {
		t.Errorf("all cancelled: %q, want cancelled", got)
	}
	if _, err := w.pool.Exec(ctx, `DELETE FROM ride_requests`); err != nil {
		t.Fatal(err)
	}
	if got := state(); got != "none" {
		t.Errorf("no requests: %q, want none", got)
	}

	// Somebody else's plans are not in the list.
	other := w.user(t, "+306900000004", uuid.Nil)
	list, err := w.svc.List(ctx, other)
	must(t, err)
	if len(list) != 0 {
		t.Errorf("a stranger sees %d plans", len(list))
	}
}

func (w *world) pushes(t *testing.T) []notices.Payload {
	t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT payload FROM outbox WHERE kind = 'push' ORDER BY id`)
	must(t, err)
	defer rows.Close()
	var out []notices.Payload
	for rows.Next() {
		var raw []byte
		must(t, rows.Scan(&raw))
		var p notices.Payload
		must(t, json.Unmarshal(raw, &p))
		out = append(out, p)
	}
	return out
}

func TestThursdayQuestionIsAskedOnceFromSixInTheEvening(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}

	step := func(when time.Time) int {
		w.clock.Set(when)
		n, err := w.svc.AskThursday(ctx, when)
		if err != nil {
			t.Fatalf("AskThursday at %s: %v", when, err)
		}
		return n
	}

	if n := step(time.Date(2026, time.July, 30, 17, 59, 0, 0, athens)); n != 0 || len(w.pushes(t)) != 0 {
		t.Fatalf("asked %d one minute early", n)
	}
	if n := step(time.Date(2026, time.July, 30, 18, 0, 0, 0, athens)); n != 1 {
		t.Fatalf("asked %d at six, want 1", n)
	}
	got := w.pushes(t)
	if len(got) != 1 || got[0].Event != notices.WeekendAsk || got[0].RecipientID != w.passenger ||
		got[0].PlanID == nil || *got[0].PlanID != plan {
		t.Fatalf("pushes = %+v", got)
	}
	if n := step(time.Date(2026, time.July, 30, 19, 0, 0, 0, athens)); n != 0 || len(w.pushes(t)) != 1 {
		t.Fatalf("asked again an hour later (%d)", n)
	}

	// The next weekend is a new question.
	w.clock.Set(time.Date(2026, time.August, 3, 1, 0, 0, 0, athens))
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := step(time.Date(2026, time.August, 6, 18, 5, 0, 0, athens)); n != 1 || len(w.pushes(t)) != 2 {
		t.Fatalf("the following Thursday asked %d", n)
	}
}

func TestThursdayQuestionIsNotAskedWhenItHasNoPoint(t *testing.T) {
	ctx := context.Background()
	thursday := time.Date(2026, time.July, 30, 18, 30, 0, 0, athens)

	t.Run("they said no already", func(t *testing.T) {
		w := newWorld(t)
		plan := w.create(t, fridayToSunday())
		_, _ = w.svc.Generate(ctx, w.clock.Now())
		_, err := w.svc.ThisWeek(ctx, w.passenger, plan, false)
		must(t, err)
		if n, err := w.svc.AskThursday(ctx, thursday); err != nil || n != 0 {
			t.Errorf("AskThursday = %d, %v", n, err)
		}
	})
	t.Run("a driver already has them", func(t *testing.T) {
		w := newWorld(t)
		plan := w.create(t, fridayToSunday())
		_, _ = w.svc.Generate(ctx, w.clock.Now())
		_, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'matched'
			WHERE id = (SELECT id FROM ride_requests WHERE weekend_plan_id = $1 ORDER BY window_start LIMIT 1)`, plan)
		must(t, err)
		if n, err := w.svc.AskThursday(ctx, thursday); err != nil || n != 0 {
			t.Errorf("AskThursday = %d, %v", n, err)
		}
	})
	t.Run("the outbound window has opened", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, fridayToSunday())
		_, _ = w.svc.Generate(ctx, w.clock.Now())
		friday := time.Date(2026, time.July, 31, 15, 30, 0, 0, athens)
		if n, err := w.svc.AskThursday(ctx, friday); err != nil || n != 0 {
			t.Errorf("AskThursday = %d, %v", n, err)
		}
	})
	t.Run("the plan did not exist yet", func(t *testing.T) {
		w := newWorld(t)
		w.clock.Set(time.Date(2026, time.July, 30, 20, 0, 0, 0, athens))
		w.create(t, fridayToSunday())
		_, _ = w.svc.Generate(ctx, w.clock.Now())
		if n, err := w.svc.AskThursday(ctx, w.clock.Now()); err != nil || n != 0 {
			t.Errorf("a plan made at eight was asked at once (%d, %v)", n, err)
		}
	})
	t.Run("a stopped plan", func(t *testing.T) {
		w := newWorld(t)
		plan := w.create(t, fridayToSunday())
		_, _ = w.svc.Generate(ctx, w.clock.Now())
		must(t, w.svc.Stop(ctx, w.passenger, plan))
		if n, err := w.svc.AskThursday(ctx, thursday); err != nil || n != 0 {
			t.Errorf("AskThursday = %d, %v", n, err)
		}
	})
}

// The cutoff is the outbound day and time of the plan, not the earliest window
// of whatever is still live: after Friday's request lapses Saturday's is the
// earliest, and a late job would ask a question about a weekend that has
// started.
func TestThursdayQuestionIsNotAskedOnceTheOutboundHasPassedEvenIfLaterDaysAreOpen(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	_, _ = w.svc.Generate(ctx, w.clock.Now())

	// Friday's request has run out; Saturday's and Sunday's are still open.
	_, err := w.pool.Exec(ctx, `UPDATE ride_requests SET status = 'expired'
		WHERE weekend_plan_id = $1 AND weekend_leg = 'out' AND window_start < $2`,
		plan, time.Date(2026, time.July, 31, 23, 0, 0, 0, athens))
	must(t, err)

	late := time.Date(2026, time.July, 31, 20, 0, 0, 0, athens)
	if n, err := w.svc.AskThursday(ctx, late); err != nil || n != 0 || len(w.pushes(t)) != 0 {
		t.Fatalf("AskThursday on Friday evening = %d, %v, %d pushes; want no question", n, err, len(w.pushes(t)))
	}
}

// A job that has read the plan as eligible and then waits behind the
// passenger's lock must read it again: an answer given in between ("not this
// week", or a seat taken) has made the question unwanted.
func TestThursdayQuestionIsCheckedAgainUnderThePassengersLock(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	_, _ = w.svc.Generate(ctx, w.clock.Now())

	holder, err := w.pool.Begin(ctx)
	must(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, match.PassengerLockSQL, w.passenger)
	must(t, err)

	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := w.svc.AskThursday(ctx, time.Date(2026, time.July, 30, 18, 30, 0, 0, athens))
		done <- result{n, err}
	}()

	// Wait until the job is parked behind the lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		must(t, w.pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			   AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting))
		if waiting > 0 {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("AskThursday finished without waiting for the passenger's lock: %+v", r)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AskThursday never waited for the passenger's lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The answer "not this week" lands first.
	_, err = holder.Exec(ctx, `UPDATE ride_requests SET status = 'cancelled' WHERE weekend_plan_id = $1`, plan)
	must(t, err)
	must(t, holder.Commit(ctx))

	r := <-done
	if r.err != nil || r.n != 0 || len(w.pushes(t)) != 0 {
		t.Fatalf("AskThursday = %d, %v with %d pushes; want no question after the answer", r.n, r.err, len(w.pushes(t)))
	}
}

// Two sends of one create, as a client that timed out makes them, are one plan
// with one set of requests.
func TestConcurrentCreatesWithOneKeyMakeOnePlan(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	key := uuid.New()

	type result struct {
		plan weekend.Plan
		err  error
	}
	start := make(chan struct{})
	out := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			p := fridayToSunday()
			p.ClientKey = &key
			<-start
			got, err := w.svc.Create(ctx, w.passenger, p)
			out <- result{got, err}
		}()
	}
	close(start)
	a, b := <-out, <-out
	must(t, a.err)
	must(t, b.err)
	if a.plan.ID != b.plan.ID {
		t.Fatalf("two plans, %s and %s, for one key", a.plan.ID, b.plan.ID)
	}
	if a.plan.Replayed == b.plan.Replayed {
		t.Errorf("replayed = %v and %v; exactly one of the two is the retry", a.plan.Replayed, b.plan.Replayed)
	}

	var plans, requests int
	must(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM weekend_plans`).Scan(&plans))
	must(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM ride_requests WHERE weekend_plan_id = $1`, a.plan.ID).Scan(&requests))
	if plans != 1 || requests != 4 {
		t.Errorf("%d plans and %d requests, want 1 and 4", plans, requests)
	}

	// Without a key every create is a new plan, as before.
	w.create(t, fridayToSunday())
	w.create(t, fridayToSunday())
	must(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM weekend_plans`).Scan(&plans))
	if plans != 3 {
		t.Errorf("%d plans after two creates without a key, want 3", plans)
	}
}

func TestThisWeekKeepsTheAnswerForTheWeekend(t *testing.T) {
	for _, going := range []bool{true, false} {
		w := newWorld(t)
		ctx := context.Background()
		plan := w.create(t, fridayToSunday())
		if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
			t.Fatal(err)
		}
		list, err := w.svc.List(ctx, w.passenger)
		must(t, err)
		if list[0].AnsweredThisWeek {
			t.Errorf("going=%v: answered before any answer", going)
		}
		got, err := w.svc.ThisWeek(ctx, w.passenger, plan, going)
		must(t, err)
		if !got.AnsweredThisWeek {
			t.Errorf("going=%v: the answer is not kept", going)
		}
		list, err = w.svc.List(ctx, w.passenger)
		must(t, err)
		if !list[0].AnsweredThisWeek {
			t.Errorf("going=%v: List forgot the answer", going)
		}
		// The next weekend asks again.
		w.clock.Set(time.Date(2026, time.August, 3, 12, 0, 0, 0, athens))
		list, err = w.svc.List(ctx, w.passenger)
		must(t, err)
		if list[0].AnsweredThisWeek {
			t.Errorf("going=%v: last weekend's answer counts for the next", going)
		}
	}
}

func TestKeepStartsTheCountOverAndTouchesNothingElse(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.create(t, fridayToSunday())
	if _, err := w.svc.Generate(ctx, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `UPDATE weekend_plans SET declines_in_row = 3`); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.Keep(ctx, w.passenger, plan)
	must(t, err)
	if got.DeclinesInRow != 0 || got.AskToStop {
		t.Errorf("after keep: %d in a row, ask_to_stop %v", got.DeclinesInRow, got.AskToStop)
	}
	if got.ThisWeek != "open" || got.AnsweredThisWeek {
		t.Errorf("keep changed the weekend: %q, answered %v", got.ThisWeek, got.AnsweredThisWeek)
	}
	if !w.active(t, plan) {
		t.Error("keep stopped the plan")
	}

	stranger := w.user(t, "+306900000003", uuid.Nil)
	if _, err := w.svc.Keep(ctx, stranger, plan); !errors.Is(err, weekend.ErrNotFound) {
		t.Errorf("Keep by a stranger = %v, want ErrNotFound", err)
	}
	if _, err := w.svc.Keep(ctx, w.passenger, uuid.New()); !errors.Is(err, weekend.ErrNotFound) {
		t.Errorf("Keep of nothing = %v, want ErrNotFound", err)
	}
}
