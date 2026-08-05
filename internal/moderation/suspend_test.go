package moderation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/moderation"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

var testNow = time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)

type world struct {
	t     *testing.T
	pool  *pgxpool.Pool
	svc   *moderation.Service
	clock *clock.Fake

	driver, passenger, moderator uuid.UUID
	// tomorrow is a departure that has not happened; yesterday is one that has.
	tomorrow, yesterday          uuid.UUID
	tomorrowBooking, pastBooking uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	fake := clock.NewFake(testNow)
	w := &world{t: t, pool: pool, svc: moderation.NewService(pool, fake), clock: fake}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding %q: %v", sql, err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding %q: %v", sql, err)
		}
	}

	var regionID uuid.UUID
	scan(&regionID, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`)

	scan(&w.driver, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001','Οδηγός',$1) RETURNING id`, regionID)
	scan(&w.passenger, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002','Επιβάτης',$1) RETURNING id`, regionID)
	scan(&w.moderator, `INSERT INTO users (phone, name, home_region_id, is_moderator)
		VALUES ('+306900000003','Επόπτης',$1,true) RETURNING id`, regionID)

	exec(`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris','ΑΒΓ-1234')`, w.driver)
	testdb.GiveEveryDriverACar(t, pool)

	var templateID uuid.UUID
	scan(&templateID, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default), ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, w.driver)

	occurrence := func(dest *uuid.UUID, date string, departs time.Time) {
		scan(dest, `
			INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
			VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), $2, $3::timestamptz, $3::timestamptz + interval '30 minutes', 3, 2, 1,
				ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
			RETURNING id`, templateID, date, departs)
	}
	occurrence(&w.tomorrow, "2026-07-30", testNow.Add(24*time.Hour))
	occurrence(&w.yesterday, "2026-07-28", testNow.Add(-24*time.Hour))

	// A booking has to come from somewhere: the constraint insists on an
	// accepted ask or a standing arrangement, never both and never neither.
	booking := func(dest *uuid.UUID, occurrenceID uuid.UUID, status string) {
		var requestID, askID uuid.UUID
		scan(&requestID, `
			INSERT INTO ride_requests (passenger_id, origin, destination,
				window_start, window_end, seats, kind, status)
			VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
				ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
				$2::timestamptz, $2::timestamptz + interval '2 hours', 1, 'open', 'matched')
			RETURNING id`, w.passenger, testNow)
		scan(&askID, `
			INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
				pickup_point, dropoff_point, suggested_price_eur, itinerary_version,
				status, expires_at)
			VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
				ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted', $3::timestamptz)
			RETURNING id`, requestID, occurrenceID, testNow.Add(time.Hour))
		scan(dest, `
			INSERT INTO bookings (occurrence_id, request_id, ask_id, passenger_id,
				seats, price_eur, status, pickup_point, dropoff_point)
			VALUES ($1, $2, $3, $4, 1, 2.50, $5,
				ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
			RETURNING id`, occurrenceID, requestID, askID, w.passenger, status)
	}
	booking(&w.tomorrowBooking, w.tomorrow, "confirmed")
	booking(&w.pastBooking, w.yesterday, "completed")

	return w
}

func (w *world) status(userID uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM users WHERE id = $1`, userID).Scan(&s); err != nil {
		w.t.Fatalf("reading status: %v", err)
	}
	return s
}

func (w *world) bookingStatus(id uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM bookings WHERE id = $1`, id).Scan(&s); err != nil {
		w.t.Fatalf("reading booking status: %v", err)
	}
	return s
}

func (w *world) occurrenceStatus(id uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM trip_occurrences WHERE id = $1`, id).Scan(&s); err != nil {
		w.t.Fatalf("reading occurrence status: %v", err)
	}
	return s
}

func (w *world) pushCount() int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'push'`).Scan(&n); err != nil {
		w.t.Fatalf("counting pushes: %v", err)
	}
	return n
}

func (w *world) suspend() {
	w.t.Helper()
	if err := w.svc.Suspend(context.Background(), w.driver, w.moderator, "harassment"); err != nil {
		w.t.Fatalf("Suspend: %v", err)
	}
}

// The point of the whole thing. A flag that leaves confirmed trips running is
// not a suspension: four people still turn up tomorrow morning expecting the
// person the app has just decided is not allowed to drive them.
func TestSuspendingADriverCallsOffWhatHasNotHappenedYet(t *testing.T) {
	w := newWorld(t)
	w.suspend()

	if got := w.status(w.driver); got != "suspended" {
		t.Errorf("driver status = %q, want suspended", got)
	}
	if got := w.occurrenceStatus(w.tomorrow); got != "cancelled" {
		t.Errorf("tomorrow's departure = %q, want cancelled", got)
	}
	if got := w.bookingStatus(w.tomorrowBooking); got != "cancelled_by_driver" {
		t.Errorf("tomorrow's booking = %q, want cancelled_by_driver", got)
	}
	if w.pushCount() == 0 {
		t.Error("nobody was told their ride is off")
	}
}

// And it leaves the past alone. A trip that ran last week ran; rewriting it
// would be inventing history, and the ratings and the receipt hang off it.
func TestSuspensionDoesNotReachBackwards(t *testing.T) {
	w := newWorld(t)
	w.suspend()

	if got := w.occurrenceStatus(w.yesterday); got == "cancelled" {
		t.Error("a departure that already happened was cancelled")
	}
	if got := w.bookingStatus(w.pastBooking); got != "completed" {
		t.Errorf("a finished booking became %q", got)
	}
}

// A suspended passenger is the mirror image: their seat goes back so the driver
// can sell it, and the driver is told rather than finding out at the kerb.
func TestSuspendingAPassengerGivesTheSeatBack(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	var before int
	if err := w.pool.QueryRow(ctx,
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, w.tomorrow).Scan(&before); err != nil {
		t.Fatalf("reading seats: %v", err)
	}

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "no-shows"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	if got := w.bookingStatus(w.tomorrowBooking); got != "cancelled_by_passenger" {
		t.Errorf("booking = %q, want cancelled_by_passenger", got)
	}
	var after int
	if err := w.pool.QueryRow(ctx,
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, w.tomorrow).Scan(&after); err != nil {
		t.Fatalf("reading seats: %v", err)
	}
	if after != before+1 {
		t.Errorf("seats_free went %d -> %d, want the seat back", before, after)
	}
	// The driver's own departure is not their problem to answer for.
	if got := w.occurrenceStatus(w.tomorrow); got == "cancelled" {
		t.Error("suspending a passenger called off the driver's trip")
	}
}

// Middleware reads the status on every request, so a live session is refused
// within milliseconds. Revoking the tokens as well is the belt to that
// bracer — and it is what stops a refresh from minting a new access token.
func TestSuspensionEndsEverySession(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if _, err := w.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at)
		VALUES ($1, 'live-hash', gen_random_uuid(), $2)`,
		w.driver, testNow.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("seeding a session: %v", err)
	}

	w.suspend()

	var live int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`,
		w.driver).Scan(&live); err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	if live != 0 {
		t.Errorf("%d sessions survived the suspension", live)
	}
}

// Every suspension is somebody's decision, and somebody appealing one needs to
// know whose and why.
func TestSuspensionIsRecordedWithItsReasonAndItsAuthor(t *testing.T) {
	w := newWorld(t)
	w.suspend()

	var action, reason string
	var actor uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		SELECT action, reason, actor_id FROM account_actions WHERE user_id = $1`,
		w.driver).Scan(&action, &reason, &actor); err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if action != "suspend" || reason != "harassment" || actor != w.moderator {
		t.Errorf("recorded %q by %v for %q", action, actor, reason)
	}
}

// Two moderators reaching the same conclusion is one suspension, and must not
// send everybody a second notification saying their ride is off again.
func TestSuspendingTwiceChangesNothingTheSecondTime(t *testing.T) {
	w := newWorld(t)
	w.suspend()
	first := w.pushCount()

	if err := w.svc.Suspend(context.Background(), w.driver, w.moderator, "harassment"); err != nil {
		t.Fatalf("second Suspend: %v", err)
	}

	if again := w.pushCount(); again != first {
		t.Errorf("pushes went %d -> %d on a repeat suspension", first, again)
	}
	var actions int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM account_actions WHERE user_id = $1`, w.driver).Scan(&actions); err != nil {
		t.Fatalf("counting actions: %v", err)
	}
	if actions != 1 {
		t.Errorf("%d suspension records, want 1", actions)
	}
}

// Reinstating gives the account back. It does not give back the trips: those
// passengers have made other arrangements, and un-cancelling a ride somebody
// was told was off would be worse than leaving it cancelled.
func TestReinstatingRestoresTheAccountButNotTheCancelledTrips(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.suspend()

	if err := w.svc.Reinstate(ctx, w.driver, w.moderator, "appeal upheld"); err != nil {
		t.Fatalf("Reinstate: %v", err)
	}

	if got := w.status(w.driver); got != "active" {
		t.Errorf("status = %q, want active", got)
	}
	if got := w.occurrenceStatus(w.tomorrow); got != "cancelled" {
		t.Errorf("tomorrow's departure came back as %q", got)
	}

	var actions int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM account_actions WHERE user_id = $1 AND action = 'reinstate'`,
		w.driver).Scan(&actions); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if actions != 1 {
		t.Errorf("%d reinstatement records, want 1", actions)
	}
}

// A deleted account is not a suspended one, and must not be quietly resurrected
// into 'active' by a moderator acting on an old report.
func TestADeletedAccountIsNotReinstated(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `UPDATE users SET status = 'deleted' WHERE id = $1`, w.driver); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	if err := w.svc.Reinstate(ctx, w.driver, w.moderator, "oops"); err == nil {
		t.Fatal("Reinstate on a deleted account returned no error")
	}
	if got := w.status(w.driver); got != "deleted" {
		t.Errorf("status = %q, want deleted", got)
	}
}
