package account_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/account"
	"carpool/internal/clock"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

var testNow = time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)

type world struct {
	pool                  *pgxpool.Pool
	svc                   *account.Service
	driverID, passengerID uuid.UUID
	occurrenceID          uuid.UUID
	templateID            uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := &world{pool: pool, svc: account.NewService(pool, clock.NewFake(testNow))}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	var regionID uuid.UUID
	scan(&regionID, `INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`)
	scan(&w.driverID, `INSERT INTO users (phone, name, home_region_id, photo_url, facebook_handle, gender)
		VALUES ('+306900000001','Οδηγός',$1,'https://example.gr/p.jpg',
		        'odigos','male') RETURNING id`, regionID)
	scan(&w.passengerID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002','Επιβάτης',$1) RETURNING id`, regionID)
	if _, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1,'Yaris','ΑΒΓ-1234')`, w.driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	scan(&w.templateID, `INSERT INTO route_templates (driver_id, origin, destination,
		origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
		'Αλεξ/πολη','Φέρες', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326),
		17, 20, 3, 'test') RETURNING id`, w.driverID)
	scan(&w.occurrenceID, `INSERT INTO trip_occurrences (template_id, service_date,
		depart_earliest, depart_latest, seats_total, seats_free, template_revision,
		route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, DATE '2026-08-04', '2026-08-04T06:30:00Z', '2026-08-04T07:00:00Z', 3, 2, 1,
		ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, w.templateID)

	var requestID, askID uuid.UUID
	scan(&requestID, `INSERT INTO ride_requests (passenger_id, origin, destination,
		window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		'2026-08-04T03:00:00Z','2026-08-04T07:00:00Z',1,'open','matched') RETURNING id`, w.passengerID)
	scan(&askID, `INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		pickup_point, dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1,$2,5,2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0,'accepted','2026-08-04T03:00:00Z')
		RETURNING id`, requestID, w.occurrenceID)
	if _, err := pool.Exec(ctx, `INSERT INTO bookings (occurrence_id, request_id, passenger_id,
		ask_id, seats, price_eur, pickup_point, dropoff_point)
		VALUES ($1,$2,$3,$4,1,2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326))`,
		w.occurrenceID, requestID, w.passengerID, askID); err != nil {
		t.Fatalf("seeding booking: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trip_messages (occurrence_id, sender_id, body)
		VALUES ($1,$2,'Θα είμαι στη στάση στις 07:35')`, w.occurrenceID, w.passengerID); err != nil {
		t.Fatalf("seeding message: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO push_tokens (token, user_id, platform)
		VALUES ('ExponentPushToken[x]',$1,'android')`, w.passengerID); err != nil {
		t.Fatalf("seeding push token: %v", err)
	}
	return w
}

func (w *world) column(t *testing.T, col string, id uuid.UUID) any {
	t.Helper()
	var v any
	if err := w.pool.QueryRow(context.Background(),
		`SELECT `+col+` FROM users WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("reading %s: %v", col, err)
	}
	return v
}

func TestErasureLeavesNothingThatNamesThePerson(t *testing.T) {
	w := newWorld(t)

	if err := w.svc.Delete(context.Background(), w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := w.column(t, "phone", w.driverID); got == "+306900000001" {
		t.Error("the phone number survived the erasure")
	}
	for _, col := range []string{"photo_url", "facebook_handle", "gender"} {
		if got := w.column(t, col, w.driverID); got != nil {
			t.Errorf("%s survived as %v", col, got)
		}
	}
	if got := w.column(t, "status", w.driverID); got != "deleted" {
		t.Errorf("status = %v", got)
	}
	// The car is as good as a name in a town this size.
	var model, plate string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT car_model, car_plate FROM driver_profiles WHERE user_id = $1`,
		w.driverID).Scan(&model, &plate); err != nil {
		t.Fatalf("reading the vehicle: %v", err)
	}
	if model == "Yaris" || plate == "ΑΒΓ-1234" {
		t.Errorf("the vehicle still identifies them: %s %s", model, plate)
	}
}

// A rating is somebody else's judgement and a report protected a third person.
// Erasing one account must not rewrite what other people said or relied on.
func TestErasureKeepsWhatBelongsToOtherPeople(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO ratings (booking_id, rater_id, ratee_id, stars)
		SELECT id, $2, $3, 5 FROM bookings WHERE occurrence_id = $1`,
		w.occurrenceID, w.passengerID, w.driverID); err != nil {
		t.Fatalf("seeding rating: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason)
		VALUES ($1,$2,'test')`, w.driverID, w.passengerID); err != nil {
		t.Fatalf("seeding report: %v", err)
	}

	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var ratings, reports, messages int
	if err := w.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM ratings  WHERE rater_id = $1),
		(SELECT count(*) FROM reports  WHERE reported_id = $1),
		(SELECT count(*) FROM trip_messages WHERE sender_id = $1)`, w.passengerID).
		Scan(&ratings, &reports, &messages); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if ratings != 1 {
		t.Error("a rating this person gave to somebody else was destroyed")
	}
	if reports != 1 {
		t.Error("a report about this person was destroyed")
	}
	if messages != 1 {
		t.Error("half of somebody else's conversation was destroyed")
	}
}

// A deleted person holding next Tuesday's seat is a seat nobody can use.
func TestErasureGivesBackTheSeatsAndStopsTheRides(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var free int
	if err := w.pool.QueryRow(ctx,
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, w.occurrenceID).Scan(&free); err != nil {
		t.Fatalf("reading seats: %v", err)
	}
	if free != 3 {
		t.Errorf("%d seats free, want the seat back", free)
	}

	if err := w.svc.Delete(ctx, w.driverID); err != nil {
		t.Fatalf("Delete driver: %v", err)
	}
	var active bool
	var occStatus string
	if err := w.pool.QueryRow(ctx, `
		SELECT t.active, o.status FROM route_templates t
		JOIN trip_occurrences o ON o.template_id = t.id
		WHERE t.id = $1`, w.templateID).Scan(&active, &occStatus); err != nil {
		t.Fatalf("reading the route: %v", err)
	}
	if active {
		t.Error("the deleted driver is still offering rides")
	}
	if occStatus != "cancelled" {
		t.Errorf("a future trip is %q; nobody is going to drive it", occStatus)
	}
}

func TestErasureRevokesEveryWayBackIn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var tokens, sessions int
	if err := w.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM push_tokens    WHERE user_id = $1),
		(SELECT count(*) FROM refresh_tokens WHERE user_id = $1)`, w.passengerID).
		Scan(&tokens, &sessions); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if tokens != 0 || sessions != 0 {
		t.Errorf("%d push tokens and %d sessions survived", tokens, sessions)
	}
}

func TestErasingTwiceIsTheSameAsOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	before := w.column(t, "phone", w.passengerID)
	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("second Delete: %v", err)
	}

	if after := w.column(t, "phone", w.passengerID); after != before {
		t.Errorf("the second erasure changed the tombstone from %v to %v", before, after)
	}
}

func TestTheExportContainsWhatThePersonWouldExpect(t *testing.T) {
	w := newWorld(t)

	body, err := w.svc.Export(context.Background(), w.passengerID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the export is not readable JSON: %v", err)
	}
	profile, ok := out["profile"].(map[string]any)
	if !ok || profile["phone"] != "+306900000002" {
		t.Fatalf("the profile is missing or wrong: %v", out["profile"])
	}
	for _, section := range []string{"trips_taken", "messages", "requests_made"} {
		items, ok := out[section].([]any)
		if !ok || len(items) == 0 {
			t.Errorf("%s is empty; the person cannot see what we hold", section)
		}
	}
}

// Being able to answer a request is not the same as being able to show that it
// was answered.
func TestBothRequestsAreWrittenDown(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if _, err := w.svc.Export(ctx, w.passengerID); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if err := w.svc.Delete(ctx, w.passengerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rows, err := w.pool.Query(ctx,
		`SELECT kind FROM data_requests WHERE user_id = $1 ORDER BY id`, w.passengerID)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		kinds = append(kinds, k)
	}
	if len(kinds) != 2 || kinds[0] != "access" || kinds[1] != "erasure" {
		t.Fatalf("the log holds %v, want an access then an erasure", kinds)
	}
}
