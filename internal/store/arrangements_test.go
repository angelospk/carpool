package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// fixture seeds the smallest world a booking can exist in: a region, a driver
// with a car, a passenger, one template, one occurrence, and one request with
// its ask. Both ways of arriving at a booking are then exercised against it.
type fixture struct {
	pool                   *pgxpool.Pool
	driverID, passengerID  uuid.UUID
	templateID, occurrence uuid.UUID
	requestID, askID       uuid.UUID
	// nextWeek is a second occurrence, so a test can hold two bookings for the
	// same passenger without tripping bookings' UNIQUE (occurrence, passenger).
	nextWeek uuid.UUID
}

func seed(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	f := fixture{pool: pool}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding (%s): %v", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0], err)
		}
	}

	var regionID uuid.UUID
	scan(&regionID, `INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`)
	scan(&f.driverID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001', 'Οδηγός', $1) RETURNING id`, regionID)
	scan(&f.passengerID, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002', 'Επιβάτης', $1) RETURNING id`, regionID)

	if _, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Toyota Yaris', 'ΑΒΓ-1234')`, f.driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	testdb.GiveEveryDriverACar(t, pool)

	scan(&f.templateID, `INSERT INTO route_templates
		(driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		 route_geom, route_km, route_min, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default), ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326),
		        'Αλεξανδρούπολη', 'Φέρες',
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 'test')
		RETURNING id`, f.driverID)

	scan(&f.occurrence, `INSERT INTO trip_occurrences
		(template_id, vehicle_id, service_date, depart_earliest, depart_latest, seats_total, seats_free,
		 template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), DATE '2026-08-04', '2026-08-04T04:30:00Z', '2026-08-04T05:30:00Z', 3, 3, 1,
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 'formula')
		RETURNING id`, f.templateID)

	scan(&f.nextWeek, `INSERT INTO trip_occurrences
		(template_id, vehicle_id, service_date, depart_earliest, depart_latest, seats_total, seats_free,
		 template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), DATE '2026-08-11', '2026-08-11T04:30:00Z', '2026-08-11T05:30:00Z', 3, 3, 1,
		        ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
		        28.6, 25, 'formula')
		RETURNING id`, f.templateID)

	scan(&f.requestID, `INSERT INTO ride_requests
		(passenger_id, origin, destination, window_start, window_end, kind)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		            ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326),
		        '2026-08-04T03:00:00Z', '2026-08-04T07:00:00Z', 'open')
		RETURNING id`, f.passengerID)

	scan(&f.askID, `INSERT INTO asks
		(request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		 suggested_price_eur, itinerary_version, expires_at)
		VALUES ($1, $2, 5.0, 3.0, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 3.20, 0, '2026-08-04T03:00:00Z')
		RETURNING id`, f.requestID, f.occurrence)

	return f
}

func (f fixture) arrangement(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO standing_arrangements
		(template_id, passenger_id, weekday, seats, pickup_point, dropoff_point,
		 price_eur, until_date, source_booking_id)
		VALUES ($1, $2, 2, 1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 3.20, DATE '2026-09-01', NULL)
		RETURNING id`, f.templateID, f.passengerID).Scan(&id)
	if err != nil {
		t.Fatalf("seeding arrangement: %v", err)
	}
	return id
}

// insertBooking writes a booking with whichever provenance columns the caller
// names, so the invariant can be probed from every direction.
func (f fixture) insertBooking(occurrenceID uuid.UUID, askID, requestID, arrangementID any) error {
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO bookings
		(occurrence_id, request_id, passenger_id, ask_id, arrangement_id, seats, price_eur,
		 pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, $5, 1, 3.20,
		        ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326))`,
		occurrenceID, requestID, f.passengerID, askID, arrangementID)
	return err
}

// A booking arrives either from an ask a driver accepted or from a standing
// arrangement they approved once. Anything else — both, or neither — is a
// booking whose provenance nobody can answer for.
func TestABookingComesFromAnAskOrAnArrangementButNeverBoth(t *testing.T) {
	f := seed(t)
	arrangementID := f.arrangement(t)

	t.Run("from an accepted ask", func(t *testing.T) {
		if err := f.insertBooking(f.occurrence, f.askID, f.requestID, nil); err != nil {
			t.Fatalf("the existing ask-based booking is no longer valid: %v", err)
		}
	})

	t.Run("from a standing arrangement", func(t *testing.T) {
		if err := f.insertBooking(f.nextWeek, nil, nil, arrangementID); err != nil {
			t.Fatalf("an arrangement booking was refused: %v", err)
		}
	})

	rejected := []struct {
		name                      string
		ask, request, arrangement any
	}{
		{"from nothing at all", nil, nil, nil},
		{"from both at once", f.askID, f.requestID, arrangementID},
		{"an ask with no request", f.askID, nil, nil},
		{"a request with no ask", nil, f.requestID, nil},
		{"an arrangement carrying an ask", f.askID, nil, arrangementID},
		{"an arrangement carrying a request", nil, f.requestID, arrangementID},
	}
	for i, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh occurrence each time, so a rejection can only come from
			// the provenance rule and never from the passenger/occurrence key.
			var occ uuid.UUID
			if err := f.pool.QueryRow(context.Background(), `
				INSERT INTO trip_occurrences
				(template_id, vehicle_id, service_date, depart_earliest, depart_latest, seats_total, seats_free,
				 template_revision, route_geom, route_km, route_min, pricing_mode)
				VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), DATE '2026-09-01' + $2::int, '2026-09-01T04:30:00Z', '2026-09-01T05:30:00Z',
				        3, 3, 1, ST_SetSRID(ST_GeomFromText('LINESTRING(25.87 40.85, 26.17 40.90)'), 4326),
				        28.6, 25, 'formula')
				RETURNING id`, f.templateID, i).Scan(&occ); err != nil {
				t.Fatalf("seeding occurrence: %v", err)
			}
			if err := f.insertBooking(occ, tc.ask, tc.request, tc.arrangement); err == nil {
				t.Fatal("the database accepted a booking that came from nowhere coherent")
			}
		})
	}
}

// Cancelling one week must not invite that week back. Without a row that
// survives the cancellation, any pass that books "dates with no booking yet"
// would recreate the day the passenger just called off.
func TestACancelledWeekIsNotBookedAgain(t *testing.T) {
	f := seed(t)
	arrangementID := f.arrangement(t)

	if err := f.insertBooking(f.occurrence, nil, nil, arrangementID); err != nil {
		t.Fatalf("first booking: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE bookings SET status = 'cancelled_by_passenger' WHERE arrangement_id = $1`,
		arrangementID); err != nil {
		t.Fatalf("cancelling: %v", err)
	}

	if err := f.insertBooking(f.occurrence, nil, nil, arrangementID); err == nil {
		t.Fatal("the same arrangement booked the same occurrence twice; a cancelled week comes back")
	}
}

// One live arrangement per passenger, template and weekday. A second switch for
// the same Tuesday is the same agreement, not another one.
func TestAPassengerHasOneLiveArrangementPerWeekday(t *testing.T) {
	f := seed(t)
	f.arrangement(t)

	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO standing_arrangements
		(template_id, passenger_id, weekday, seats, pickup_point, dropoff_point,
		 price_eur, until_date)
		VALUES ($1, $2, 2, 1, ST_SetSRID(ST_MakePoint(25.87, 40.85), 4326),
		        ST_SetSRID(ST_MakePoint(26.17, 40.90), 4326), 3.20, DATE '2026-09-01')
		RETURNING id`, f.templateID, f.passengerID).Scan(&id)
	if err == nil {
		t.Fatal("a second live arrangement was accepted for the same Tuesday")
	}
}

// The link is a place to look someone up, not a redirect the app will follow
// anywhere. Anything outside the handful of hosts people actually mean is
// refused at the column, so no handler can forget the check.
func TestAProfileLinkOnlyPointsWhereItSaysItDoes(t *testing.T) {
	f := seed(t)
	ctx := context.Background()

	accepted := []string{
		"https://facebook.com/giannis.k",
		"https://www.facebook.com/profile.php?id=1234567890",
		"https://m.facebook.com/groups/123/posts/456",
		"https://instagram.com/giannis.k",
	}
	for _, link := range accepted {
		t.Run("accepts "+link, func(t *testing.T) {
			if _, err := f.pool.Exec(ctx,
				`UPDATE users SET social_url = $2 WHERE id = $1`, f.passengerID, link); err != nil {
				t.Fatalf("a real profile link was refused: %v", err)
			}
		})
	}

	refused := map[string]string{
		"plain http":                             "http://facebook.com/giannis.k",
		"somewhere else":                         "https://faceb00k.com.evil.example/giannis",
		"a host merely ending in the right name": "https://evilfacebook.com/giannis",
		"javascript":                             "javascript:alert(1)",
		"free text":                              "ο Γιάννης στο facebook",
	}
	for name, link := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := f.pool.Exec(ctx,
				`UPDATE users SET social_url = $2 WHERE id = $1`, f.passengerID, link); err == nil {
				t.Fatalf("the database accepted %q as a profile link", link)
			}
		})
	}
}
