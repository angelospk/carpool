package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/jobs"
	"carpool/internal/match"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

const corridorWKT = "LINESTRING(25.80 40.85, 26.00 40.85)"

type fixture struct {
	t           *testing.T
	pool        *pgxpool.Pool
	clock       *clock.Fake
	driverID    uuid.UUID
	passengerID uuid.UUID
	templateID  uuid.UUID
}

// newFixture seeds a driver with a Monday-only recurring route.
func newFixture(t *testing.T, weekdays ...time.Weekday) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	newUser := func(phone, name string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (phone, name, home_region_id) VALUES ($1, $2, $3) RETURNING id`,
			phone, name, regionID).Scan(&id); err != nil {
			t.Fatalf("seeding user %s: %v", name, err)
		}
		return id
	}

	driverID := newUser("+306900000001", "Driver")
	passengerID := newUser("+306900000002", "Passenger")

	if _, err := pool.Exec(ctx, `
		INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`,
		driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}

	if len(weekdays) == 0 {
		weekdays = []time.Weekday{time.Monday}
	}

	templateID, err := store.CreateRouteTemplate(ctx, pool, store.CreateRouteTemplateParams{
		DriverID:         driverID,
		VehicleID:        testdb.DefaultCar(t, pool, driverID),
		Origin:           geo.Point{Lon: 25.80, Lat: 40.85},
		Destination:      geo.Point{Lon: 26.00, Lat: 40.85},
		OriginLabel:      "A",
		DestinationLabel: "B",
		RouteWKT:         corridorWKT,
		RouteKm:          17,
		RouteMin:         20,
		Seats:            3,
		PricingMode:      domain.PricingFormula,
		OSRMDataVersion:  "test",
		Detour:           match.Budget{AllowStops: true, Km: 10, Min: 10},
		Schedule: domain.Schedule{
			Kind:           domain.ScheduleRecurring,
			Weekdays:       weekdays,
			DepartEarliest: domain.TimeOfDay{Hour: 7, Minute: 30},
			DepartLatest:   domain.TimeOfDay{Hour: 8, Minute: 0},
			EffectiveFrom:  domain.Date{Year: 2026, Month: time.July, Day: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateRouteTemplate: %v", err)
	}

	// 2026-07-06 is a Monday.
	c := clock.NewFake(time.Date(2026, time.July, 6, 3, 0, 0, 0, time.UTC))
	return &fixture{t: t, pool: pool, clock: c, driverID: driverID, passengerID: passengerID, templateID: templateID}
}

func (f *fixture) materializer() *jobs.Materializer {
	return jobs.NewMaterializer(f.pool, f.clock, athens(f.t))
}

func athens(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		t.Fatalf("loading Europe/Athens: %v", err)
	}
	return loc
}

func (f *fixture) serviceDates() []string {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT service_date::text FROM trip_occurrences
		 WHERE status <> 'cancelled' ORDER BY service_date`)
	if err != nil {
		f.t.Fatalf("listing occurrences: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			f.t.Fatalf("scanning: %v", err)
		}
		out = append(out, d)
	}
	return out
}

func TestMaterializeCreatesOccurrencesAcrossTheHorizon(t *testing.T) {
	f := newFixture(t)

	if err := f.materializer().Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A 21-day half-open horizon from Monday 6 July covers the 6th, 13th, 20th.
	got := f.serviceDates()
	want := []string{"2026-07-06", "2026-07-13", "2026-07-20"}
	if len(got) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("occurrence %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestMaterializeIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := range 3 {
		if err := f.materializer().Run(ctx); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if got := f.serviceDates(); len(got) != 3 {
		t.Fatalf("got %d occurrences after three runs, want 3: %v", len(got), got)
	}
}

func TestMaterializeStoresConcreteAthensDepartureInstants(t *testing.T) {
	f := newFixture(t)

	if err := f.materializer().Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var earliest, latest time.Time
	if err := f.pool.QueryRow(context.Background(), `
		SELECT depart_earliest, depart_latest FROM trip_occurrences
		WHERE service_date = '2026-07-06'`).Scan(&earliest, &latest); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}

	// July is EEST (UTC+3), so a 07:30 local departure is 04:30 UTC.
	wantEarliest := time.Date(2026, time.July, 6, 4, 30, 0, 0, time.UTC)
	if !earliest.Equal(wantEarliest) {
		t.Errorf("depart_earliest = %v, want %v", earliest.UTC(), wantEarliest)
	}
	if !latest.Equal(time.Date(2026, time.July, 6, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("depart_latest = %v, want 05:00 UTC", latest.UTC())
	}
}

func TestMaterializeSnapshotsTheTemplateOntoTheOccurrence(t *testing.T) {
	f := newFixture(t)

	if err := f.materializer().Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var routeKm float64
	var revision int
	var sameGeom bool
	if err := f.pool.QueryRow(context.Background(), `
		SELECT o.route_km, o.template_revision, ST_Equals(o.route_geom, t.route_geom)
		FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
		LIMIT 1`).Scan(&routeKm, &revision, &sameGeom); err != nil {
		t.Fatalf("reading snapshot: %v", err)
	}

	if routeKm != 17 {
		t.Errorf("route_km = %v, want 17", routeKm)
	}
	if revision != 1 {
		t.Errorf("template_revision = %d, want 1", revision)
	}
	if !sameGeom {
		t.Error("the occurrence did not snapshot the template geometry")
	}
}

func TestMaterializeAdvancesTheHorizonAsTimePasses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// A week later the horizon should have rolled forward to include 27 July.
	f.clock.Advance(7 * 24 * time.Hour)
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	got := f.serviceDates()
	if len(got) == 0 || got[len(got)-1] != "2026-07-27" {
		t.Fatalf("occurrences = %v, want the horizon to reach 2026-07-27", got)
	}
}

func TestMaterializeSkipsExceptionDates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET exceptions = ARRAY['2026-07-13']::date[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("setting exception: %v", err)
	}

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, d := range f.serviceDates() {
		if d == "2026-07-13" {
			t.Fatal("the excepted date was materialized")
		}
	}
}

// A schedule edit must reshape future occurrences that nobody is depending on.
func TestMaterializeCancelsOccurrencesTheScheduleNoLongerWants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Move the route from Mondays to Tuesdays.
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET weekdays = ARRAY[2]::int[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("changing weekdays: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	got := f.serviceDates()
	want := []string{"2026-07-07", "2026-07-14", "2026-07-21"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want the Tuesdays %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("occurrence %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// The core safety rule: an occurrence someone has booked is frozen. Rewriting
// or cancelling it would silently change what a passenger already agreed to.
func TestMaterializeNeverTouchesABookedOccurrence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	occurrenceID := f.bookOccurrence("2026-07-13")

	// Move the route to Tuesdays: the booked Monday must survive anyway.
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET weekdays = ARRAY[2]::int[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("changing weekdays: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&status); err != nil {
		t.Fatalf("reading booked occurrence: %v", err)
	}
	if status != "scheduled" {
		t.Fatalf("the booked occurrence is now %q; it must stay scheduled", status)
	}
}

// Same rule for an occurrence with a pending ask: a driver is being asked about
// it right now, and accepting must not land on a cancelled trip.
func TestMaterializeNeverTouchesAnOccurrenceWithAPendingAsk(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	occurrenceID := f.addPendingAsk("2026-07-13")

	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET weekdays = ARRAY[2]::int[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("changing weekdays: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&status); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	if status != "scheduled" {
		t.Fatalf("the occurrence under a pending ask is now %q; it must stay scheduled", status)
	}
}

func TestMaterializeLeavesPastOccurrencesAlone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Roll forward past the first two Mondays and re-run.
	f.clock.Advance(14 * 24 * time.Hour)
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var count int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM trip_occurrences WHERE service_date < '2026-07-20' AND status = 'cancelled'`).
		Scan(&count); err != nil {
		t.Fatalf("counting cancelled past occurrences: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d past occurrences were cancelled; history must not be rewritten", count)
	}
}

func TestMaterializeSkipsAnInactiveTemplate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_templates SET active = false WHERE id = $1`, f.templateID); err != nil {
		t.Fatalf("deactivating template: %v", err)
	}

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := f.serviceDates(); len(got) != 0 {
		t.Fatalf("got %v, want no occurrences for an inactive template", got)
	}
}

// Two materializers can overlap (a scheduled tick and an edit-triggered run).
// They must not race into duplicate rows.
func TestConcurrentMaterializersProduceNoDuplicates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const runners = 4
	errs := make([]error, runners)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = f.materializer().Run(ctx)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("runner %d: %v", i, err)
		}
	}
	if got := f.serviceDates(); len(got) != 3 {
		t.Fatalf("got %d occurrences %v, want 3", len(got), got)
	}
}

// bookOccurrence attaches a confirmed booking to the occurrence on the date.
func (f *fixture) bookOccurrence(serviceDate string) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()

	var occurrenceID uuid.UUID
	if err := f.pool.QueryRow(ctx,
		`SELECT id FROM trip_occurrences WHERE service_date = $1::date`, serviceDate).Scan(&occurrenceID); err != nil {
		f.t.Fatalf("finding occurrence for %s: %v", serviceDate, err)
	}

	requestID, askID := f.addAsk(occurrenceID, "accepted")

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
		                      pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50,
		        ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(25.98, 40.85), 4326))`,
		occurrenceID, requestID, f.passengerID, askID); err != nil {
		f.t.Fatalf("inserting booking: %v", err)
	}
	return occurrenceID
}

// addPendingAsk attaches a pending ask to the occurrence on the date.
func (f *fixture) addPendingAsk(serviceDate string) uuid.UUID {
	f.t.Helper()
	var occurrenceID uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id FROM trip_occurrences WHERE service_date = $1::date`, serviceDate).Scan(&occurrenceID); err != nil {
		f.t.Fatalf("finding occurrence for %s: %v", serviceDate, err)
	}
	f.addAsk(occurrenceID, "pending")
	return occurrenceID
}

func (f *fixture) addAsk(occurrenceID uuid.UUID, status string) (requestID, askID uuid.UUID) {
	f.t.Helper()
	ctx := context.Background()

	if err := f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1,
		        ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(25.98, 40.85), 4326),
		        now(), now() + interval '2 hours', 'open', 'asking')
		RETURNING id`, f.passengerID).Scan(&requestID); err != nil {
		f.t.Fatalf("inserting ride request: %v", err)
	}

	if err := f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		                  pickup_point, dropoff_point, suggested_price_eur,
		                  itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
		        ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(25.98, 40.85), 4326),
		        2.50, 0, $3, now() + interval '30 minutes')
		RETURNING id`, requestID, occurrenceID, status).Scan(&askID); err != nil {
		f.t.Fatalf("inserting ask: %v", err)
	}
	return requestID, askID
}

// --- regression tests for issues found in adversarial review ---

// Cancellation must be bounded by the same horizon as planning. Without an
// upper bound, every occurrence beyond the horizon looks "unwanted".
func TestMaterializeLeavesOccurrencesBeyondTheHorizonAlone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// A Monday further out than the 21-day horizon, as a longer horizon or an
	// earlier run would have left behind.
	var beyondID uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		SELECT template_id, vehicle_id, '2026-08-24', '2026-08-24T04:30:00Z', '2026-08-24T05:00:00Z',
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode
		FROM trip_occurrences LIMIT 1
		RETURNING id`).Scan(&beyondID); err != nil {
		t.Fatalf("seeding an out-of-horizon occurrence: %v", err)
	}

	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM trip_occurrences WHERE id = $1`, beyondID).Scan(&status); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	if status != "scheduled" {
		t.Fatalf("an occurrence beyond the horizon is now %q; it is simply not in scope yet", status)
	}
}

// A date cancelled by an exception must come back when the exception is lifted.
// Identity is (template_id, service_date), so a tombstone would otherwise block
// the row forever.
func TestMaterializeRestoresAnOccurrenceWhenItsExceptionIsLifted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Materialize first so the date exists, then except it: that is what leaves
	// a cancelled tombstone on the (template, service_date) key.
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET exceptions = ARRAY['2026-07-13']::date[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("setting exception: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("run with exception: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`SELECT 1 FROM trip_occurrences WHERE service_date = '2026-07-13' AND status = 'cancelled'`); err != nil {
		t.Fatalf("checking the tombstone: %v", err)
	}

	// The driver changes their mind and removes the exception.
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET exceptions = '{}'::date[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("clearing exception: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("run without exception: %v", err)
	}

	for _, d := range f.serviceDates() {
		if d == "2026-07-13" {
			return
		}
	}
	t.Fatalf("2026-07-13 was not restored after the exception was lifted; got %v", f.serviceDates())
}

// Editing the departure window must reshape occurrences nothing depends on.
func TestMaterializeAppliesAScheduleTimeChangeToAnUntouchedOccurrence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `
		UPDATE route_schedules SET depart_earliest = make_time(9,0,0), depart_latest = make_time(9,30,0)
		WHERE template_id = $1`, f.templateID); err != nil {
		t.Fatalf("changing the departure window: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var earliest time.Time
	if err := f.pool.QueryRow(ctx,
		`SELECT depart_earliest FROM trip_occurrences WHERE service_date = '2026-07-13'`).
		Scan(&earliest); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	// 09:00 Athens in July (EEST, UTC+3) is 06:00 UTC.
	want := time.Date(2026, time.July, 13, 6, 0, 0, 0, time.UTC)
	if !earliest.Equal(want) {
		t.Fatalf("depart_earliest = %v, want the edited %v", earliest.UTC(), want)
	}
}

// A booked occurrence must keep its original times even when the schedule moves.
func TestMaterializeDoesNotRetimeABookedOccurrence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	occurrenceID := f.bookOccurrence("2026-07-13")

	if _, err := f.pool.Exec(ctx, `
		UPDATE route_schedules SET depart_earliest = make_time(9,0,0), depart_latest = make_time(9,30,0)
		WHERE template_id = $1`, f.templateID); err != nil {
		t.Fatalf("changing the departure window: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var earliest time.Time
	if err := f.pool.QueryRow(ctx,
		`SELECT depart_earliest FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&earliest); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	want := time.Date(2026, time.July, 13, 4, 30, 0, 0, time.UTC) // the original 07:30 Athens
	if !earliest.Equal(want) {
		t.Fatalf("a booked occurrence was retimed to %v; it must stay at %v", earliest.UTC(), want)
	}
}

// A trip that already departed today is history, even if the schedule changed.
func TestMaterializeDoesNotCancelATripThatAlreadyDepartedToday(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// It is now the afternoon of the first service date; that morning's trip ran.
	f.clock.Advance(12 * time.Hour)
	if _, err := f.pool.Exec(ctx,
		`UPDATE route_schedules SET weekdays = ARRAY[2]::int[] WHERE template_id = $1`,
		f.templateID); err != nil {
		t.Fatalf("changing weekdays: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM trip_occurrences WHERE service_date = '2026-07-06'`).Scan(&status); err != nil {
		t.Fatalf("reading occurrence: %v", err)
	}
	if status != "scheduled" {
		t.Fatalf("a trip that already departed is now %q; history must not be rewritten", status)
	}
}

// A driver who edits how far they will go out of their way expects that to
// reach the trips they have already published. The template is where the answer
// is edited and the occurrence is where matching reads it, so a template edit
// that does not reach the occurrences changes nothing at all.
func TestEditingTheDetourReachesOccurrencesNobodyHasBookedYet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`UPDATE route_templates SET allow_stops = false, detour_km = 3.5, detour_min = 4 WHERE id = $1`,
		f.templateID); err != nil {
		t.Fatalf("editing the template: %v", err)
	}
	if err := f.materializer().Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var allow bool
	var km float64
	var min int
	if err := f.pool.QueryRow(ctx, `
		SELECT allow_stops, detour_km, detour_min FROM trip_occurrences
		WHERE template_id = $1 ORDER BY service_date LIMIT 1`, f.templateID).
		Scan(&allow, &km, &min); err != nil {
		t.Fatalf("reading the occurrence: %v", err)
	}
	if allow || km != 3.5 || min != 4 {
		t.Errorf("occurrence carries allow_stops=%v km=%v min=%v, want the edited template's terms",
			allow, km, min)
	}
}
