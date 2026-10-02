package live_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/live"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// departs is the fixture trip's earliest departure; it leaves by 07:30 UTC and
// takes twenty minutes.
var departs = time.Date(2026, time.August, 3, 7, 0, 0, 0, time.UTC)

type world struct {
	pool                         *pgxpool.Pool
	clock                        *clock.Fake
	svc                          *live.Service
	driver, rider, other, nobody uuid.UUID
	trip                         uuid.UUID
	otherBooking                 uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, clock: clock.NewFake(departs.Add(-10 * time.Minute))}
	w.svc = live.NewService(pool, w.clock)

	var region uuid.UUID
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO regions (name, boundary) VALUES ('Έβρος',
		ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326))) RETURNING id`).Scan(&region))
	person := func(phone, name string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO users (date_of_birth, phone, name, home_region_id)
			VALUES (DATE '1990-01-01', $1, $2, $3) RETURNING id`, phone, name, region).Scan(&id))
		return id
	}
	w.driver, w.rider, w.other, w.nobody =
		person("+306900000001", "Οδηγός"), person("+306900000002", "Επιβάτης"),
		person("+306900000003", "Άλλος"), person("+306900000004", "Κανείς")
	_, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`, w.driver)
	must(err)
	testdb.GiveEveryDriverACar(t, pool)

	var template uuid.UUID
	must(pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, w.driver).Scan(&template))
	must(pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-08-03', $2, $3, 3, 1, 1,
			ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, template, departs, departs.Add(30*time.Minute)).Scan(&w.trip))

	book := func(passenger uuid.UUID) uuid.UUID {
		var request, ask, booking uuid.UUID
		must(pool.QueryRow(ctx, `
			INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
			VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
				$2, $3, 'open', 'matched') RETURNING id`, passenger, departs.Add(-time.Hour), departs.Add(time.Hour)).Scan(&request))
		must(pool.QueryRow(ctx, `
			INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
				suggested_price_eur, itinerary_version, status, expires_at)
			VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
				2.5, 0, 'accepted', $3) RETURNING id`, request, w.trip, departs).Scan(&ask))
		must(pool.QueryRow(ctx, `
			INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur, pickup_point, dropoff_point)
			VALUES ($1, $2, $3, $4, 1, 2.5, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
			RETURNING id`, w.trip, request, passenger, ask).Scan(&booking))
		return booking
	}
	book(w.rider)
	w.otherBooking = book(w.other)
	return w
}

func names(ps []live.Position) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func (w *world) everybodyShares(t *testing.T) {
	t.Helper()
	for _, who := range []uuid.UUID{w.driver, w.rider, w.other} {
		if err := w.svc.Put(context.Background(), w.trip, who, 40.85, 25.9); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
}

// The driver sees the passengers and each passenger sees the driver. Two
// strangers in the back seat do not see each other.
func TestTheDriverSeesPassengersAndPassengersSeeOnlyTheDriver(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)

	got, err := w.svc.Visible(ctx, w.trip, w.driver)
	if err != nil || len(got) != 2 {
		t.Fatalf("the driver sees %v, %v; want both passengers", names(got), err)
	}
	got, err = w.svc.Visible(ctx, w.trip, w.rider)
	if err != nil || len(got) != 1 || got[0].UserID != w.driver {
		t.Fatalf("a passenger sees %v, %v; want only the driver", names(got), err)
	}
}

// Somebody not on the trip gets the same answer whether it exists or not, for
// reading and for writing.
func TestSomebodyNotOnTheTripCanNeitherSeeNorShare(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	for _, trip := range []uuid.UUID{w.trip, uuid.New()} {
		if _, err := w.svc.Visible(ctx, trip, w.nobody); !errors.Is(err, live.ErrNotOnTrip) {
			t.Errorf("Visible: %v, want ErrNotOnTrip", err)
		}
		if err := w.svc.Put(ctx, trip, w.nobody, 40.85, 25.9); !errors.Is(err, live.ErrNotOnTrip) {
			t.Errorf("Put: %v, want ErrNotOnTrip", err)
		}
	}
}

// A passenger whose seat was cancelled is off the trip: they stop seeing the
// driver, and the driver stops seeing them.
func TestACancelledSeatIsOffTheTrip(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	if _, err := w.pool.Exec(ctx, `UPDATE bookings SET status = 'cancelled_by_passenger' WHERE id = $1`, w.otherBooking); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Visible(ctx, w.trip, w.other); !errors.Is(err, live.ErrNotOnTrip) {
		t.Fatalf("the cancelled passenger reading: %v, want ErrNotOnTrip", err)
	}
	got, _ := w.svc.Visible(ctx, w.trip, w.driver)
	if len(got) != 1 || got[0].UserID != w.rider {
		t.Fatalf("the driver still sees %v", names(got))
	}
}

// Sharing opens half an hour before and closes an hour after the trip could
// have arrived. Outside that nothing is stored and nothing is shown.
func TestSharingIsOnlyOpenAroundTheTrip(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	w.clock.Set(departs.Add(-31 * time.Minute))
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.9); !errors.Is(err, live.ErrOutsideWindow) {
		t.Fatalf("31 minutes early: %v, want ErrOutsideWindow", err)
	}
	w.clock.Set(departs.Add(-29 * time.Minute))
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.9); err != nil {
		t.Fatalf("29 minutes early: %v", err)
	}

	// Latest departure 07:30, twenty minutes of road, an hour after: 08:50.
	w.clock.Set(departs.Add(110 * time.Minute))
	if got, _ := w.svc.Visible(ctx, w.trip, w.rider); len(got) != 0 {
		t.Fatalf("after the window a passenger still sees %v", names(got))
	}
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.9); !errors.Is(err, live.ErrOutsideWindow) {
		t.Fatalf("after the window: %v, want ErrOutsideWindow", err)
	}
}

func TestACalledOffTripSharesNothing(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	if _, err := w.pool.Exec(ctx, `UPDATE trip_occurrences SET status = 'cancelled' WHERE id = $1`, w.trip); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.svc.Visible(ctx, w.trip, w.rider); len(got) != 0 {
		t.Fatalf("a called-off trip still shows %v", names(got))
	}
}

// A position older than ten minutes is a phone that stopped sending, and a dot
// that old reads as "the car is here" when it is not.
func TestAStalePositionIsNotShown(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	w.clock.Advance(live.Fresh + time.Second)
	if got, _ := w.svc.Visible(ctx, w.trip, w.rider); len(got) != 0 {
		t.Fatalf("a stale position is shown: %v", names(got))
	}
}

func TestStoppingForgetsThePosition(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.everybodyShares(t)
	if err := w.svc.Stop(ctx, w.trip, w.driver); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got, _ := w.svc.Visible(ctx, w.trip, w.rider); len(got) != 0 {
		t.Fatalf("stopped, but still shown: %v", names(got))
	}
	var rows int
	_ = w.pool.QueryRow(ctx, `SELECT count(*) FROM trip_live_positions WHERE user_id = $1`, w.driver).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d rows left after stopping", rows)
	}
}

// One row, overwritten, and not more often than every five seconds.
func TestAPositionIsOverwrittenNotAccumulated(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_ = w.svc.Put(ctx, w.trip, w.driver, 40.80, 25.80)
	w.clock.Advance(2 * time.Second)
	_ = w.svc.Put(ctx, w.trip, w.driver, 40.81, 25.81) // too soon: ignored
	w.clock.Advance(4 * time.Second)
	_ = w.svc.Put(ctx, w.trip, w.driver, 40.82, 25.82)

	var rows int
	var lat float64
	_ = w.pool.QueryRow(ctx, `SELECT count(*), max(lat) FROM trip_live_positions`).Scan(&rows, &lat)
	if rows != 1 || lat != 40.82 {
		t.Fatalf("rows = %d, lat = %v; want one row at 40.82", rows, lat)
	}
}

func TestANonsensePositionIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, p := range [][2]float64{{math.NaN(), 25}, {40, math.Inf(1)}, {91, 25}, {40, -181}} {
		if err := w.svc.Put(context.Background(), w.trip, w.driver, p[0], p[1]); !errors.Is(err, live.ErrBadPosition) {
			t.Errorf("Put(%v): %v, want ErrBadPosition", p, err)
		}
	}
}

func storedFix(t *testing.T, w *world) (lat float64, accuracy *float64, observed *time.Time) {
	t.Helper()
	if err := w.pool.QueryRow(context.Background(), `
		SELECT lat, accuracy_m, observed_at FROM trip_live_positions WHERE user_id = $1`, w.driver).
		Scan(&lat, &accuracy, &observed); err != nil {
		t.Fatalf("reading the fix: %v", err)
	}
	return lat, accuracy, observed
}

// A fix says how good it is and when the phone took it. A batch that arrives
// out of order — a tunnel, a retry — never puts an older place over a newer
// one, and a phone whose clock runs ahead is held to the server's.
func TestAFixKeepsItsAccuracyAndTimeAndNeverGoesBackwards(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	now := w.clock.Now()
	acc := 25.0

	seen := now.Add(-20 * time.Second)
	if err := w.svc.PutFix(ctx, w.trip, w.driver, live.Fix{Lat: 40.82, Lon: 25.82, AccuracyM: &acc, ObservedAt: &seen}); err != nil {
		t.Fatalf("PutFix: %v", err)
	}
	lat, gotAcc, gotSeen := storedFix(t, w)
	if lat != 40.82 || gotAcc == nil || *gotAcc != 25 || gotSeen == nil || !gotSeen.Equal(seen) {
		t.Fatalf("stored %v / %v / %v", lat, gotAcc, gotSeen)
	}

	w.clock.Advance(10 * time.Second)
	older := now.Add(-40 * time.Second)
	_ = w.svc.PutFix(ctx, w.trip, w.driver, live.Fix{Lat: 40.70, Lon: 25.70, AccuracyM: &acc, ObservedAt: &older})
	if lat, _, _ := storedFix(t, w); lat != 40.82 {
		t.Fatalf("an older fix replaced a newer one: lat %v", lat)
	}

	w.clock.Advance(10 * time.Second)
	ahead := w.clock.Now().Add(time.Hour)
	_ = w.svc.PutFix(ctx, w.trip, w.driver, live.Fix{Lat: 40.83, Lon: 25.83, AccuracyM: &acc, ObservedAt: &ahead})
	if _, _, got := storedFix(t, w); got == nil || got.After(w.clock.Now()) {
		t.Fatalf("a fix from the future was stored as %v", got)
	}
}

// After «Τέλος» the driver's phone writes nothing more about this trip: a
// request still in flight would otherwise put the position back.
func TestNothingIsWrittenForADriveThatHasEnded(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `UPDATE trip_occurrences SET drive_ended_at = $2 WHERE id = $1`,
		w.trip, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.9); !errors.Is(err, live.ErrOutsideWindow) {
		t.Fatalf("Put after the drive ended: %v, want ErrOutsideWindow", err)
	}
	// A passenger still shares their own way to the pickup.
	if err := w.svc.Put(ctx, w.trip, w.rider, 40.85, 25.9); err != nil {
		t.Fatalf("a passenger after the drive ended: %v", err)
	}
}

// An upload already past its checks when «Τέλος» lands must not put the
// position back after «Τέλος» has deleted it.
func TestAnUploadRacingTheEndOfTheDriveIsNotWritten(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	end, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = end.Rollback(ctx) }()
	if _, err := end.Exec(ctx, `SELECT 1 FROM trip_occurrences WHERE id = $1 FOR UPDATE`, w.trip); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- w.svc.Put(ctx, w.trip, w.driver, 40.85, 25.9) }()
	time.Sleep(300 * time.Millisecond) // the upload is now waiting on the row

	if _, err := end.Exec(ctx, `UPDATE trip_occurrences SET drive_ended_at = $2 WHERE id = $1`,
		w.trip, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := end.Exec(ctx, `DELETE FROM trip_live_positions WHERE occurrence_id = $1`, w.trip); err != nil {
		t.Fatal(err)
	}
	if err := end.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; !errors.Is(err, live.ErrOutsideWindow) {
		t.Fatalf("the racing upload: %v, want ErrOutsideWindow", err)
	}
	var rows int
	_ = w.pool.QueryRow(ctx, `SELECT count(*) FROM trip_live_positions WHERE user_id = $1`, w.driver).Scan(&rows)
	if rows != 0 {
		t.Fatalf("the position came back after «Τέλος»")
	}
}

// A fix that does not say how good it is may not replace a fresh one that
// does: a foreground upload still in flight when the drive's background task
// took over would otherwise put an unqualified position over the car's.
func TestAnUnqualifiedFixDoesNotReplaceAFreshTrustedOne(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	acc := 20.0
	seen := w.clock.Now()
	if err := w.svc.PutFix(ctx, w.trip, w.driver, live.Fix{Lat: 40.82, Lon: 25.82, AccuracyM: &acc, ObservedAt: &seen}); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(10 * time.Second)
	_ = w.svc.Put(ctx, w.trip, w.driver, 40.70, 25.70)
	if lat, _, _ := storedFix(t, w); lat != 40.82 {
		t.Fatalf("an unqualified fix replaced a trusted one: lat %v", lat)
	}

	// Once the trusted one is stale, anything newer is better than nothing.
	w.clock.Advance(3 * time.Minute)
	_ = w.svc.Put(ctx, w.trip, w.driver, 40.70, 25.70)
	if lat, _, _ := storedFix(t, w); lat != 40.70 {
		t.Fatalf("a stale trusted fix was never replaced: lat %v", lat)
	}
}
