package bookings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/bookings"
	"carpool/internal/clock"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

var testNow = time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)

type world struct {
	pool                  *pgxpool.Pool
	svc                   *bookings.Service
	driverID, passengerID uuid.UUID
	occurrenceID          uuid.UUID
	// fromAsk is a booking made the ordinary way; fromArrangement is one a
	// standing agreement produced. Cancelling is the same act for both, which
	// is the thing worth proving.
	fromAsk, fromArrangement uuid.UUID
	arrangementID            uuid.UUID
	secondOccurrence         uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, svc: bookings.NewService(pool, clock.NewFake(testNow))}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	var regionID, templateID uuid.UUID
	scan(&regionID, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`)
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES (NULL, 'petrol', 1.859, 'test'), (NULL, 'diesel', 1.729, 'test')`); err != nil {
		t.Fatalf("seeding fuel prices: %v", err)
	}
	scan(&w.driverID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001','Οδηγός',$1) RETURNING id`, regionID)
	scan(&w.passengerID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002','Επιβάτης',$1) RETURNING id`, regionID)
	if _, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1,'Yaris','ΑΒΓ-1234')`, w.driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	scan(&templateID, `INSERT INTO route_templates (driver_id, origin, destination,
		origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
		'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, w.driverID)

	occ := func(dest *uuid.UUID, date string) {
		scan(dest, `INSERT INTO trip_occurrences (template_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode)
			VALUES ($1, $2::date, ($2::date + time '06:30')::timestamptz,
			        ($2::date + time '07:00')::timestamptz, 3, 2, 1,
			        ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
			RETURNING id`, templateID, date)
	}
	occ(&w.occurrenceID, "2026-08-04")
	occ(&w.secondOccurrence, "2026-08-11")

	var requestID, askID uuid.UUID
	scan(&requestID, `INSERT INTO ride_requests (passenger_id, origin, destination,
		window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		'2026-08-04T03:00:00Z','2026-08-04T07:00:00Z', 1, 'open', 'matched') RETURNING id`, w.passengerID)
	scan(&askID, `INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		pickup_point, dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1,$2,5,2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted','2026-08-04T03:00:00Z')
		RETURNING id`, requestID, w.occurrenceID)
	scan(&w.fromAsk, `INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id,
		seats, price_eur, pickup_point, dropoff_point)
		VALUES ($1,$2,$3,$4,1,2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326)) RETURNING id`,
		w.occurrenceID, requestID, w.passengerID, askID)

	scan(&w.arrangementID, `INSERT INTO standing_arrangements (template_id, passenger_id, weekday,
		seats, pickup_point, dropoff_point, price_eur, until_date, status)
		VALUES ($1,$2,2,1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, DATE '2026-08-22', 'active')
		RETURNING id`, templateID, w.passengerID)
	scan(&w.fromArrangement, `INSERT INTO bookings (occurrence_id, passenger_id, arrangement_id,
		seats, price_eur, pickup_point, dropoff_point)
		VALUES ($1,$2,$3,1,2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326)) RETURNING id`,
		w.secondOccurrence, w.passengerID, w.arrangementID)

	return w
}

func (w *world) seatsFree(t *testing.T, occurrenceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&n); err != nil {
		t.Fatalf("reading seats: %v", err)
	}
	return n
}

func (w *world) bookingStatus(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM bookings WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("reading booking: %v", err)
	}
	return s
}

func TestAPassengerGivesBackTheirOwnSeat(t *testing.T) {
	w := newWorld(t)

	if err := w.svc.Cancel(context.Background(), w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := w.bookingStatus(t, w.fromAsk); got != "cancelled_by_passenger" {
		t.Errorf("status = %q, want the record to say who cancelled", got)
	}
	if free := w.seatsFree(t, w.occurrenceID); free != 3 {
		t.Errorf("%d seats free, want the seat back", free)
	}
}

// A driver who cannot make it has to be able to say so, and the record must
// distinguish it from the passenger dropping out — the two mean different
// things to whoever reads the history later.
func TestADriverCanCancelAPassengerAndTheRecordSaysSo(t *testing.T) {
	w := newWorld(t)

	if err := w.svc.Cancel(context.Background(), w.fromAsk, w.driverID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := w.bookingStatus(t, w.fromAsk); got != "cancelled_by_driver" {
		t.Errorf("status = %q, want cancelled_by_driver", got)
	}
}

func TestCancellingTwiceGivesBackOneSeat(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("second Cancel: %v", err)
	}

	if free := w.seatsFree(t, w.occurrenceID); free != 3 {
		t.Fatalf("%d seats free of 3; the seat came back twice", free)
	}
}

// The same act, whichever way the booking was made — and one week of a standing
// agreement going away is not the agreement going away.
func TestCancellingAStandingWeekLeavesTheAgreementStanding(t *testing.T) {
	w := newWorld(t)

	if err := w.svc.Cancel(context.Background(), w.fromArrangement, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if free := w.seatsFree(t, w.secondOccurrence); free != 3 {
		t.Errorf("%d seats free, want the seat back", free)
	}
	var status string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM standing_arrangements WHERE id = $1`, w.arrangementID).Scan(&status); err != nil {
		t.Fatalf("reading the arrangement: %v", err)
	}
	if status != "active" {
		t.Errorf("the arrangement is %q; one missed week ended the whole thing", status)
	}
}

func TestATripThatHasLeftCannotBeCancelled(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `
		UPDATE trip_occurrences SET depart_earliest = $2::timestamptz - interval '2 hours'
		WHERE id = $1`, w.occurrenceID, testNow); err != nil {
		t.Fatalf("moving the trip into the past: %v", err)
	}

	err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID)

	if !errors.Is(err, bookings.ErrDeparted) {
		t.Fatalf("Cancel returned %v, want ErrDeparted", err)
	}
	if free := w.seatsFree(t, w.occurrenceID); free != 2 {
		t.Errorf("a seat came back on a trip that has already left (%d free)", free)
	}
}

func TestAStrangerCannotCancelSomeoneElsesSeat(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var strangerID uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		SELECT '+306900000009','Ξένος', home_region_id FROM users WHERE id = $1
		RETURNING id`, w.passengerID).Scan(&strangerID); err != nil {
		t.Fatalf("seeding a stranger: %v", err)
	}

	if err := w.svc.Cancel(ctx, w.fromAsk, strangerID); !errors.Is(err, bookings.ErrNotYours) {
		t.Fatalf("a stranger cancelled a booking: %v", err)
	}
}

// The other side has to hear about it, or a driver stops for nobody.
func TestTheOtherSideIsToldOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("second Cancel: %v", err)
	}

	var queued int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'push'`).Scan(&queued); err != nil {
		t.Fatalf("counting pushes: %v", err)
	}
	if queued != 1 {
		t.Fatalf("%d notifications queued, want exactly 1", queued)
	}
}
