package standing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/standing"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The clock sits on Saturday 1 August 2026, so every Tuesday the fixture
// creates is in the future and the arithmetic is easy to check by hand.
var testNow = time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)

type world struct {
	pool        *pgxpool.Pool
	svc         *standing.Service
	driverID    uuid.UUID
	passengerID uuid.UUID
	templateID  uuid.UUID
	// tuesdays are three consecutive Tuesdays inside the horizon.
	tuesdays []uuid.UUID
	// booking is a confirmed booking the passenger holds on the first Tuesday.
	booking uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, svc: standing.NewService(pool, clock.NewFake(testNow))}

	must := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	var regionID uuid.UUID
	must(&regionID, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`)
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES (NULL, 'petrol', 1.859, 'test'), (NULL, 'diesel', 1.729, 'test')`); err != nil {
		t.Fatalf("seeding fuel prices: %v", err)
	}
	must(&w.driverID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001', 'Οδηγός', $1) RETURNING id`, regionID)
	must(&w.passengerID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002', 'Επιβάτης', $1) RETURNING id`, regionID)
	if _, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Toyota Yaris', 'ΑΒΓ-1234')`, w.driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	testdb.GiveEveryDriverACar(t, pool)
	must(&w.templateID, `INSERT INTO route_templates
		(driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		 route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default), ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 'Αλεξ/πολη', 'Φέρες',
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 3, 'test') RETURNING id`, w.driverID)

	for _, day := range []string{"2026-08-04", "2026-08-11", "2026-08-18"} {
		var id uuid.UUID
		must(&id, `INSERT INTO trip_occurrences
			(template_id, vehicle_id, service_date, depart_earliest, depart_latest, seats_total, seats_free,
			 template_revision, route_geom, route_km, route_min, pricing_mode)
			VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), $2::date, ($2::date + time '04:30')::timestamptz,
			        ($2::date + time '05:30')::timestamptz, 3, 3, 1,
			        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
			        28.6, 25, 'formula') RETURNING id`, w.templateID, day)
		w.tuesdays = append(w.tuesdays, id)
	}

	// The ride they already took together, on the first Tuesday.
	var requestID, askID uuid.UUID
	must(&requestID, `INSERT INTO ride_requests
		(passenger_id, origin, destination, window_start, window_end, kind)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326),
		        '2026-08-04T03:00:00Z', '2026-08-04T07:00:00Z', 'open') RETURNING id`, w.passengerID)
	must(&askID, `INSERT INTO asks
		(request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		 suggested_price_eur, itinerary_version, expires_at, status)
		VALUES ($1, $2, 5.0, 3.0, ST_SetSRID(ST_MakePoint(25.90, 40.86), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 3.20, 0,
		        '2026-08-04T03:00:00Z', 'accepted') RETURNING id`, requestID, w.tuesdays[0])
	must(&w.booking, `INSERT INTO bookings
		(occurrence_id, request_id, passenger_id, ask_id, seats, price_eur, pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 3.20, ST_SetSRID(ST_MakePoint(25.90, 40.86), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326)) RETURNING id`,
		w.tuesdays[0], requestID, w.passengerID, askID)
	if _, err := pool.Exec(ctx,
		`UPDATE trip_occurrences SET seats_free = seats_free - 1 WHERE id = $1`, w.tuesdays[0]); err != nil {
		t.Fatalf("seeding seat: %v", err)
	}
	return w
}

func (w *world) seatsFree(t *testing.T, occurrenceID uuid.UUID) int {
	t.Helper()
	var free int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, occurrenceID).Scan(&free); err != nil {
		t.Fatalf("reading seats: %v", err)
	}
	return free
}

func (w *world) status(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM standing_arrangements WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("reading arrangement: %v", err)
	}
	return s
}

func TestRepeatingARideRecordsTheWeekdayAndWaitsForTheDriver(t *testing.T) {
	w := newWorld(t)

	id, err := w.svc.Repeat(context.Background(), w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}

	var weekday, seats int
	var price float64
	var status string
	var until time.Time
	err = w.pool.QueryRow(context.Background(), `
		SELECT weekday, seats, price_eur, status, until_date
		FROM standing_arrangements WHERE id = $1`, id).Scan(&weekday, &seats, &price, &status, &until)
	if err != nil {
		t.Fatalf("reading arrangement: %v", err)
	}

	if weekday != 2 {
		t.Errorf("weekday = %d, want 2: 4 August 2026 is a Tuesday in Athens", weekday)
	}
	if price != 3.20 {
		t.Errorf("price = %.2f, want the 3.20 they already agreed", price)
	}
	if seats != 1 {
		t.Errorf("seats = %d, want the 1 the source booking held", seats)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending until the driver approves", status)
	}
	if !until.After(testNow) {
		t.Errorf("until_date = %v, which is not in the future", until)
	}

	// Nothing is booked yet: the driver has not agreed to anything.
	if free := w.seatsFree(t, w.tuesdays[1]); free != 3 {
		t.Errorf("next Tuesday already lost a seat (%d free); approval is what books", free)
	}
}

func TestRepeatingIsIdempotent(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	first, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("first Repeat: %v", err)
	}
	second, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("second Repeat: %v", err)
	}

	// A double tap is one agreement, and must not ask the driver twice.
	if first != second {
		t.Fatalf("a second tap made arrangement %v alongside %v", second, first)
	}
	var asks int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'push'`).Scan(&asks); err != nil {
		t.Fatalf("counting pushes: %v", err)
	}
	if asks != 1 {
		t.Errorf("%d pushes queued, want 1", asks)
	}
}

func TestOnlyThePassengerCanRepeatTheirOwnRide(t *testing.T) {
	w := newWorld(t)

	_, err := w.svc.Repeat(context.Background(), w.booking, w.driverID)

	if !errors.Is(err, standing.ErrNotYours) {
		t.Fatalf("Repeat by the driver returned %v, want ErrNotYours", err)
	}
}

func TestApprovalBooksEveryRemainingWeekAtOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if got := w.status(t, id); got != "active" {
		t.Fatalf("status = %q, want active", got)
	}
	// The first Tuesday is the ride they already took; the other two are new.
	for _, occ := range w.tuesdays[1:] {
		if free := w.seatsFree(t, occ); free != 2 {
			t.Errorf("occurrence %v has %d seats free, want 2: the seat was not reserved", occ, free)
		}
	}
	var booked int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE arrangement_id = $1`, id).Scan(&booked); err != nil {
		t.Fatalf("counting bookings: %v", err)
	}
	if booked != 2 {
		t.Fatalf("%d weeks booked, want 2", booked)
	}
}

func TestApprovalTakesNothingIfAnyWeekIsFull(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	// The middle Tuesday fills up while the driver is deciding.
	if _, err := w.pool.Exec(ctx,
		`UPDATE trip_occurrences SET seats_free = 0 WHERE id = $1`, w.tuesdays[1]); err != nil {
		t.Fatalf("filling the week: %v", err)
	}

	err = w.svc.Approve(ctx, id, w.driverID)

	var conflict standing.ErrWeekFull
	if !errors.As(err, &conflict) {
		t.Fatalf("Approve returned %v, want the dates that would not fit", err)
	}
	if len(conflict.Dates) != 1 || conflict.Dates[0] != "2026-08-11" {
		t.Errorf("conflict names %v, want just 2026-08-11", conflict.Dates)
	}
	// Half an agreement is worse than none: nothing may be reserved.
	if free := w.seatsFree(t, w.tuesdays[2]); free != 3 {
		t.Errorf("the last Tuesday lost a seat (%d free) although the approval failed", free)
	}
	if got := w.status(t, id); got != "pending" {
		t.Errorf("status = %q, want it left pending so the driver can try again", got)
	}
}

func TestOnlyTheDriverCanApprove(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}

	if err := w.svc.Approve(ctx, id, w.passengerID); !errors.Is(err, standing.ErrNotYours) {
		t.Fatalf("the passenger approved their own arrangement: %v", err)
	}
}

func TestApprovingTwiceBooksOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("first Approve: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("second Approve: %v", err)
	}

	for _, occ := range w.tuesdays[1:] {
		if free := w.seatsFree(t, occ); free != 2 {
			t.Errorf("occurrence %v has %d seats free; a retried approval took the seat twice", occ, free)
		}
	}
}

func TestDecliningLeavesEveryWeekAlone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Decline(ctx, id, w.driverID); err != nil {
		t.Fatalf("Decline: %v", err)
	}

	if got := w.status(t, id); got != "declined" {
		t.Errorf("status = %q, want declined", got)
	}
	if free := w.seatsFree(t, w.tuesdays[1]); free != 3 {
		t.Errorf("a declined arrangement took a seat anyway (%d free)", free)
	}
	// A declined Tuesday is not a permanent no. Asking again must be possible.
	again, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("repeating after a decline: %v", err)
	}
	if again == id {
		t.Error("the declined arrangement was handed back instead of a fresh request")
	}
}

func TestCancellingTheArrangementReleasesEveryWeekAhead(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Either side may end it; here the driver does.
	if err := w.svc.Cancel(ctx, id, w.driverID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := w.status(t, id); got != "cancelled" {
		t.Errorf("status = %q, want cancelled", got)
	}
	for _, occ := range w.tuesdays[1:] {
		if free := w.seatsFree(t, occ); free != 3 {
			t.Errorf("occurrence %v still holds a seat (%d free) after the arrangement ended", occ, free)
		}
	}
	// The ride they already took is history, not a reservation to release.
	if free := w.seatsFree(t, w.tuesdays[0]); free != 2 {
		t.Errorf("cancelling the arrangement gave back the seat of the ride they had already taken (%d free)", free)
	}
}

func TestAStrangerCannotEndSomeoneElsesArrangement(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	var strangerID uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		SELECT '+306900000009', 'Ξένος', home_region_id FROM users WHERE id = $1
		RETURNING id`, w.passengerID).Scan(&strangerID); err != nil {
		t.Fatalf("seeding a stranger: %v", err)
	}

	if err := w.svc.Cancel(ctx, id, strangerID); !errors.Is(err, standing.ErrNotYours) {
		t.Fatalf("a stranger cancelled the arrangement: %v", err)
	}
}

// Declining is an answer to a question, and once the weeks are booked the
// question has been answered. Ending an active arrangement is a cancellation,
// which releases the seats; a decline would leave them held by an arrangement
// that says it was refused.
func TestAnActiveArrangementCannotBeDeclined(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	id, err := w.svc.Repeat(ctx, w.booking, w.passengerID)
	if err != nil {
		t.Fatalf("Repeat: %v", err)
	}
	if err := w.svc.Approve(ctx, id, w.driverID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if err := w.svc.Decline(ctx, id, w.driverID); !errors.Is(err, standing.ErrNotRepeatable) {
		t.Fatalf("Decline on an active arrangement returned %v; the booked weeks would keep their seats", err)
	}
}
