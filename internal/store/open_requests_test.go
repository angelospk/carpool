package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The passenger posts a request and the app says it is asking drivers. When
// there is no driver on that road at all it says the same thing, for ever, and
// the request quietly ages out. "We are asking" and "there is nobody to ask"
// are not the same news, and the second one is the one that lets somebody go
// and find another way to Ορεστιάδα.
//
// So OpenRequest carries how many drivers have a published route that could
// ever serve this corridor — dates, seats and luggage deliberately ignored,
// because the question is whether anybody drives this road, not whether they
// can take this particular trip.

var corridorNow = time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)

type corridorWorld struct {
	pool      *pgxpool.Pool
	region    uuid.UUID
	passenger uuid.UUID
	drivers   int
}

func newCorridorWorld(t *testing.T) *corridorWorld {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &corridorWorld{pool: pool}
	if err := pool.QueryRow(ctx, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&w.region); err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', '+306900000099', 'Επιβάτης', $1) RETURNING id`,
		w.region).Scan(&w.passenger); err != nil {
		t.Fatalf("seeding passenger: %v", err)
	}
	// One open request along Αλεξανδρούπολη → Φέρες, inside its window.
	if _, err := pool.Exec(ctx, `INSERT INTO ride_requests
		(passenger_id, origin, destination, origin_label, destination_label,
		 window_start, window_end, kind)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326),
		        'Αλεξανδρούπολη', 'Φέρες', $2, $3, 'open')`,
		w.passenger, corridorNow.Add(2*time.Hour), corridorNow.Add(4*time.Hour)); err != nil {
		t.Fatalf("seeding request: %v", err)
	}
	return w
}

// route publishes a template along the given line for a new driver, with an
// open-ended recurring schedule on top of it — the ordinary case, and what a
// completed publish leaves behind. Tests about schedules that have run out, or
// about a template with none, use template() and schedule() directly.
func (w *corridorWorld) route(t *testing.T, line string, active bool) uuid.UUID {
	t.Helper()
	id := w.template(t, line, active)
	w.schedule(t, id, "recurring", nil, nil, nil)
	return id
}

func (w *corridorWorld) template(t *testing.T, line string, active bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	w.drivers++
	var driverID uuid.UUID
	if err := w.pool.QueryRow(ctx, `INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Οδηγός', $2) RETURNING id`,
		"+30690000010"+string(rune('0'+w.drivers%10)), w.region).Scan(&driverID); err != nil {
		t.Fatalf("seeding driver: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Toyota Yaris', 'ΑΒΓ-1234')`, driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	testdb.GiveEveryDriverACar(t, w.pool)

	var id uuid.UUID
	if err := w.pool.QueryRow(ctx, `INSERT INTO route_templates
		(driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		 route_geom, route_km, route_min, osrm_data_version, active)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		        ST_StartPoint(ST_SetSRID(ST_GeomFromText($2), 4326)),
		        ST_EndPoint(ST_SetSRID(ST_GeomFromText($2), 4326)),
		        'Από', 'Προς',
		        ST_SetSRID(ST_GeomFromText($2), 4326), 28.6, 25, 'test', $3)
		RETURNING id`, driverID, line, active).Scan(&id); err != nil {
		t.Fatalf("seeding route template: %v", err)
	}
	return id
}

func onlyRequest(t *testing.T, w *corridorWorld) store.OpenRequest {
	t.Helper()
	got, err := store.OpenRequests(context.Background(), w.pool, w.passenger, corridorNow, 20)
	if err != nil {
		t.Fatalf("OpenRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d open requests, want 1", len(got))
	}
	return got[0]
}

// The state the app could not say out loud.
func TestNobodyDrivesThisRoadIsAnAnswer(t *testing.T) {
	w := newCorridorWorld(t)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// The ordinary case: somebody does drive it, and the waiting is just waiting.
func TestADriverOnTheRoadIsCounted(t *testing.T) {
	w := newCorridorWorld(t)
	w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 1 {
		t.Errorf("drivers on corridor = %d, want 1", got)
	}
}

// Drivers, not routes. One person publishing their morning and their evening is
// one driver on the corridor, and saying two would overstate how much cover
// there is.
func TestTwoRoutesFromOneDriverAreOneDriver(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)

	var driverID uuid.UUID
	if err := w.pool.QueryRow(context.Background(),
		`SELECT driver_id FROM route_templates WHERE id = $1`, id).Scan(&driverID); err != nil {
		t.Fatalf("reading the driver: %v", err)
	}
	var second uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO route_templates
		(driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		 route_geom, route_km, route_min, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 'Από', 'Προς',
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 'test')
		RETURNING id`, driverID).Scan(&second); err != nil {
		t.Fatalf("seeding a second route: %v", err)
	}
	w.schedule(t, second, "recurring", nil, nil, nil)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 1 {
		t.Errorf("drivers on corridor = %d, want 1", got)
	}
}

// The road that goes the other way is not cover for this journey, and the
// matcher already refuses it. This count has to refuse it too, or the passenger
// is told to keep waiting for somebody who is driving away from them.
func TestTheReturnLegIsNotADriverOnThisCorridor(t *testing.T) {
	w := newCorridorWorld(t)
	w.route(t, "LINESTRING(26.17 40.90, 25.87 40.85)", true)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// A road that passes nowhere near either end.
func TestARouteSomewhereElseIsNotOnThisCorridor(t *testing.T) {
	w := newCorridorWorld(t)
	w.route(t, "LINESTRING(25.10 41.50, 25.20 41.60)", true)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// A retired route is a route nobody is offering.
func TestARetiredRouteIsNotADriverOnTheCorridor(t *testing.T) {
	w := newCorridorWorld(t)
	w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", false)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// A suspended driver cannot be asked, so they are not cover either.
func TestASuspendedDriverIsNotCover(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	if _, err := w.pool.Exec(context.Background(), `
		UPDATE users SET status = 'suspended'
		WHERE id = (SELECT driver_id FROM route_templates WHERE id = $1)`, id); err != nil {
		t.Fatalf("suspending the driver: %v", err)
	}

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// The passenger's own published route must not count as cover for their own
// request: being told "there is a driver" and having it be yourself is the
// worst version of this screen.
func TestYourOwnRouteIsNotADriverForYourOwnRequest(t *testing.T) {
	w := newCorridorWorld(t)
	if _, err := w.pool.Exec(context.Background(), `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Toyota Yaris', 'ΑΒΓ-9999')`, w.passenger); err != nil {
		t.Fatalf("giving the passenger a driver profile: %v", err)
	}
	testdb.GiveEveryDriverACar(t, w.pool)
	var mine uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO route_templates
		(driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		 route_geom, route_km, route_min, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 'Από', 'Προς',
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 'test')
		RETURNING id`, w.passenger).Scan(&mine); err != nil {
		t.Fatalf("seeding the passenger's own route: %v", err)
	}
	w.schedule(t, mine, "recurring", nil, nil, nil)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// schedule attaches a schedule to a template, which is what decides whether the
// route can ever run again.
func (w *corridorWorld) schedule(t *testing.T, templateID uuid.UUID, kind string,
	oneoffDate, from, until any) {
	t.Helper()
	var weekdays any
	if kind == "recurring" {
		weekdays = []int{1, 2, 3, 4, 5}
	}
	if _, err := w.pool.Exec(context.Background(), `
		INSERT INTO route_schedules
		(template_id, kind, oneoff_date, weekdays, depart_earliest, depart_latest,
		 effective_from, effective_until)
		VALUES ($1, $2, $3, $4, '07:30', '08:00', coalesce($5::date, current_date), $6)`,
		templateID, kind, oneoffDate, weekdays, from, until); err != nil {
		t.Fatalf("seeding schedule: %v", err)
	}
}

// A route that cannot run again is not somebody to wait for.
//
// `active` is the retire flag, and nothing sets it when a schedule simply runs
// out: a one-off whose date has passed, or a recurring route past its
// effective_until, stays active for ever. Counting those suppressed the "nobody
// drives this road" sentence on the strength of a route that will never produce
// another departure, which is the one thing that sentence exists to say.
func TestAScheduleThatCannotRunAgainIsNotADriverOnTheCorridor(t *testing.T) {
	for _, tc := range []struct {
		name               string
		kind               string
		oneoff, from, till any
	}{
		{"a one-off whose day has gone", "oneoff", "2026-09-01", "2026-08-01", nil},
		{"a recurring route past its last day", "recurring", nil, "2026-08-01", "2026-09-10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newCorridorWorld(t)
			id := w.template(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
			w.schedule(t, id, tc.kind, tc.oneoff, tc.from, tc.till)

			if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
				t.Errorf("drivers on corridor = %d, want 0", got)
			}
		})
	}
}

// And the ordinary cases still count, so the exclusion above has not simply
// turned the number off.
func TestAScheduleThatStillRunsIsADriverOnTheCorridor(t *testing.T) {
	for _, tc := range []struct {
		name               string
		kind               string
		oneoff, from, till any
	}{
		{"a one-off still to come", "oneoff", "2026-09-30", "2026-09-01", nil},
		{"a recurring route with no end", "recurring", nil, "2026-08-01", nil},
		{"a recurring route ending later", "recurring", nil, "2026-08-01", "2026-12-31"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newCorridorWorld(t)
			id := w.template(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
			w.schedule(t, id, tc.kind, tc.oneoff, tc.from, tc.till)

			if got := onlyRequest(t, w).DriversOnCorridor; got != 1 {
				t.Errorf("drivers on corridor = %d, want 1", got)
			}
		})
	}
}

// A template with no schedule row at all. It cannot produce a departure either,
// and it is the state a half-finished publish leaves behind.
func TestATemplateWithNoScheduleIsNotADriverOnTheCorridor(t *testing.T) {
	w := newCorridorWorld(t)
	w.template(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)

	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("drivers on corridor = %d, want 0", got)
	}
}

// A route its driver offered to one group is not cover for somebody outside
// it: the matcher will never offer it to them, and counting it would hide the
// sentence that says nobody drives their road. Joining the group makes it
// count, because then it is.
func TestARouteForAGroupIsCoverOnlyForItsMembers(t *testing.T) {
	w := newCorridorWorld(t)
	ctx := context.Background()
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)

	var groupID, passengerID uuid.UUID
	if err := w.pool.QueryRow(ctx,
		`INSERT INTO groups (name, join_code) VALUES ('ΔΠΘ', 'ACDEFGHJ') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("seeding group: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `UPDATE route_templates SET group_id = $1 WHERE id = $2`, groupID, id); err != nil {
		t.Fatalf("restricting: %v", err)
	}
	if got := onlyRequest(t, w).DriversOnCorridor; got != 0 {
		t.Errorf("an outsider is told %d drivers are on the road, want 0", got)
	}

	if err := w.pool.QueryRow(ctx,
		`SELECT passenger_id FROM ride_requests LIMIT 1`).Scan(&passengerID); err != nil {
		t.Fatalf("finding the passenger: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id, role, joined_at) VALUES ($1, $2, 'member', now())`,
		groupID, passengerID); err != nil {
		t.Fatalf("joining: %v", err)
	}
	if got := onlyRequest(t, w).DriversOnCorridor; got != 1 {
		t.Errorf("a member is told %d drivers are on the road, want 1", got)
	}
}

// passed gives the template a departure and records that its driver was
// asked about this request and said no.
func (w *corridorWorld) passed(t *testing.T, templateID uuid.UUID) {
	t.Helper()
	if _, err := w.pool.Exec(context.Background(), `
		WITH occ AS (
			INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
				depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
				route_min, pricing_mode)
			SELECT id, vehicle_id, $2::date, $2, $2 + interval '30 minutes', 3, 3, 1,
			       route_geom, route_km, route_min, 'formula'
			  FROM route_templates WHERE id = $1
			RETURNING id)
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point,
		                  dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		SELECT (SELECT id FROM ride_requests), occ.id, 0, 0, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		       ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 2.00, 1, 'declined', $2
		  FROM occ`, templateID, corridorNow.Add(2*time.Hour)); err != nil {
		t.Fatalf("seeding a declined ask: %v", err)
	}
}

// The app says "every driver on this road has passed" from this count. It is
// the drivers still on the road who have not, which the two counts beside it
// cannot give: a driver who said no and then retired is in one and not the
// other (QA 2026-10-16, Codex review).
func TestDriversNotYetTriedLeavesOutTheOnesWhoPassed(t *testing.T) {
	w := newCorridorWorld(t)
	first := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.passed(t, first)
	if got := onlyRequest(t, w).DriversUntried; got != 0 {
		t.Fatalf("untried = %d after the only driver passed, want 0", got)
	}

	w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	if got := onlyRequest(t, w).DriversUntried; got != 1 {
		t.Fatalf("untried = %d with a second driver nobody asked, want 1", got)
	}

	// The first one retires: still not somebody to wait for, and the second
	// still is, though declined (1) now equals the corridor (1).
	if _, err := w.pool.Exec(context.Background(),
		`UPDATE route_templates SET active = false WHERE id = $1`, first); err != nil {
		t.Fatalf("retiring: %v", err)
	}
	if got := onlyRequest(t, w).DriversUntried; got != 1 {
		t.Fatalf("untried = %d after the passed driver retired, want 1", got)
	}
}

// A driver who passed on one route and publishes another (an edit makes a new
// one) is somebody to ask again: the matcher excludes trips it asked about,
// not people (Codex review, round 2).
func TestADriversOtherRouteIsNotYetTried(t *testing.T) {
	w := newCorridorWorld(t)
	first := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.passed(t, first)

	var second uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		INSERT INTO route_templates
		SELECT (jsonb_populate_record(t, jsonb_build_object('id', gen_random_uuid()))).*
		  FROM route_templates t WHERE t.id = $1
		RETURNING id`, first).Scan(&second); err != nil {
		t.Fatalf("seeding the second route: %v", err)
	}
	w.schedule(t, second, "recurring", nil, nil, nil)

	if got := onlyRequest(t, w).DriversUntried; got != 1 {
		t.Fatalf("untried = %d, want 1: the driver's other route was never asked", got)
	}
}

// A route the driver passed on today that runs again tomorrow, inside a
// two-day window, is still somebody to ask: the matcher excludes the trip it
// asked about, not the route (Codex review).
func TestARouteThatRunsAgainInTheWindowIsNotYetTried(t *testing.T) {
	w := newCorridorWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET window_end = window_end + interval '2 days'`); err != nil {
		t.Fatalf("widening the window: %v", err)
	}
	first := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.passed(t, first)
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO trip_occurrences
		SELECT (jsonb_populate_record(o, jsonb_build_object(
		          'id', gen_random_uuid(),
		          'service_date', o.service_date + 1,
		          'depart_earliest', o.depart_earliest + interval '1 day',
		          'depart_latest', o.depart_latest + interval '1 day'))).*
		  FROM trip_occurrences o`); err != nil {
		t.Fatalf("seeding tomorrow: %v", err)
	}

	if got := onlyRequest(t, w).DriversUntried; got != 1 {
		t.Fatalf("untried = %d, want 1: tomorrow's departure was never asked", got)
	}
}

// A departure in the window that has already left is not somebody to ask
// (Codex review, round 2).
func TestADepartureThatHasLeftIsNotYetToTry(t *testing.T) {
	w := newCorridorWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `UPDATE ride_requests SET window_start = $1`,
		corridorNow.Add(-3*time.Hour)); err != nil {
		t.Fatalf("opening the window earlier: %v", err)
	}
	first := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.passed(t, first)
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO trip_occurrences
		SELECT (jsonb_populate_record(o, jsonb_build_object(
		          'id', gen_random_uuid(),
		          'service_date', o.service_date - 1,
		          'depart_earliest', $1::timestamptz,
		          'depart_latest', $1::timestamptz + interval '30 minutes'))).*
		  FROM trip_occurrences o`, corridorNow.Add(-time.Hour)); err != nil {
		t.Fatalf("seeding a departure that has left: %v", err)
	}

	if got := onlyRequest(t, w).DriversUntried; got != 0 {
		t.Fatalf("untried = %d, want 0: the only unasked departure has left", got)
	}
}

// Four seats asked for, and every car on the road has three: nobody can ever
// be asked, and the app said only that nobody had been yet (QA 2026-10-16,
// U13). The most seats on the road lets it say why.
func TestTheMostSeatsOnTheRoadAreCounted(t *testing.T) {
	w := newCorridorWorld(t)
	small := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	larger := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `UPDATE route_templates SET seats = CASE WHEN id = $1 THEN 2 ELSE 3 END
		WHERE id IN ($1, $2)`, small, larger); err != nil {
		t.Fatalf("seats: %v", err)
	}

	if got := onlyRequest(t, w).MostSeatsOnCorridor; got == nil || *got != 3 {
		t.Errorf("most seats on corridor = %v, want 3", got)
	}
}

func TestAnEmptyRoadHasNoMostSeats(t *testing.T) {
	w := newCorridorWorld(t)
	if got := onlyRequest(t, w).MostSeatsOnCorridor; got != nil {
		t.Errorf("most seats on an empty road = %d, want none", *got)
	}
}

// departure gives the template one scheduled departure at the given instant.
func (w *corridorWorld) departure(t *testing.T, templateID uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := w.pool.Exec(context.Background(), `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode)
		SELECT id, vehicle_id, $3::date, $2::timestamptz, $2::timestamptz + interval '30 minutes', 3, 3, 1,
		       route_geom, route_km, route_min, 'formula'
		  FROM route_templates WHERE id = $1`, templateID, at, at.Format("2006-01-02")); err != nil {
		t.Fatalf("seeding a departure: %v", err)
	}
}

// Somebody drives the road, just not then. The app said "nobody has been
// asked yet" and the passenger waited for a car that leaves in the afternoon
// (QA 2026-10-16, U3). The nearest departure lets it say when one does.
func TestADriverWhoLeavesAtAnotherHourIsTheNearestDeparture(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	afternoon := corridorNow.Add(7 * time.Hour)
	w.departure(t, id, afternoon)
	w.departure(t, id, corridorNow.Add(30*time.Hour))

	got := onlyRequest(t, w)
	if got.DriversDeparting != 0 {
		t.Errorf("departing = %d, want 0: nobody leaves inside the window", got.DriversDeparting)
	}
	if got.NearestDeparture == nil || !got.NearestDeparture.Equal(afternoon) {
		t.Errorf("nearest departure = %v, want %v", got.NearestDeparture, afternoon)
	}
}

func TestADriverWhoLeavesInTheWindowIsDeparting(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.departure(t, id, corridorNow.Add(3*time.Hour))

	if got := onlyRequest(t, w).DriversDeparting; got != 1 {
		t.Errorf("departing = %d, want 1", got)
	}
}

// A day away is not "near": the passenger asked for this morning.
func TestNoDepartureThatDayIsNoNearestDeparture(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.departure(t, id, corridorNow.Add(30*time.Hour))

	if got := onlyRequest(t, w).NearestDeparture; got != nil {
		t.Errorf("nearest departure = %v, want none within twelve hours", got)
	}
}

// A departure between its earliest and latest minute has left, as the matcher
// judges it: nobody is asked about it, so it is not a car leaving then.
func TestADepartureThatHasStartedIsNotDeparting(t *testing.T) {
	w := newCorridorWorld(t)
	id := w.route(t, "LINESTRING(25.87 40.85, 26.17 40.90)", true)
	w.departure(t, id, corridorNow.Add(-10*time.Minute))

	got := onlyRequest(t, w)
	if got.DriversDeparting != 0 || got.NearestDeparture != nil {
		t.Errorf("departing = %d, nearest = %v; want neither for a car that has left",
			got.DriversDeparting, got.NearestDeparture)
	}
}
