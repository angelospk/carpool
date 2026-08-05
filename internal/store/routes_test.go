package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/match"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

func driverAndPool(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	pool := testdb.Fresh(t)
	if err := store.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return seedDriver(t, pool, "+306900000001")
}

// seedDriver puts a region, a user and a driver profile in an already-migrated
// database, and returns the pool and the driver.
func seedDriver(t *testing.T, pool *pgxpool.Pool, phone string) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	// Upserted rather than inserted: a test that seeds a second driver in the
	// same database would otherwise trip over the unique name.
	var regionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)))
		ON CONFLICT (name) DO UPDATE SET updated_at = now()
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	var driverID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (date_of_birth, phone, name, home_region_id) VALUES (DATE '1990-01-01', $1, 'Οδηγός', $2) RETURNING id`,
		phone, regionID).Scan(&driverID); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`,
		driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	return pool, driverID
}

func templateParams(t *testing.T, pool *pgxpool.Pool, driverID uuid.UUID, detour match.Budget) store.CreateRouteTemplateParams {
	t.Helper()
	return store.CreateRouteTemplateParams{
		DriverID:         driverID,
		VehicleID:        testdb.DefaultCar(t, pool, driverID),
		Origin:           geo.Point{Lon: 25.80, Lat: 40.85},
		Destination:      geo.Point{Lon: 26.00, Lat: 40.85},
		OriginLabel:      "Αλεξανδρούπολη",
		DestinationLabel: "Φέρες",
		RouteWKT:         "LINESTRING(25.80 40.85, 26.00 40.85)",
		RouteKm:          17,
		RouteMin:         20,
		Seats:            3,
		PricingMode:      domain.PricingFormula,
		OSRMDataVersion:  "test",
		Detour:           detour,
		Schedule: domain.Schedule{
			Kind:           domain.ScheduleRecurring,
			Weekdays:       []time.Weekday{time.Monday},
			DepartEarliest: domain.TimeOfDay{Hour: 7, Minute: 30},
			DepartLatest:   domain.TimeOfDay{Hour: 8, Minute: 0},
			EffectiveFrom:  domain.Date{Year: 2026, Month: time.July, Day: 1},
		},
	}
}

// What the driver agreed to has to survive the trip to the database. Everything
// downstream — which drivers get asked, how far out of their way they are sent —
// reads these three columns and nothing else.
func TestARouteKeepsTheDetourItWasCreatedWith(t *testing.T) {
	ctx := context.Background()
	pool, driverID := driverAndPool(t)

	want := match.Budget{AllowStops: true, Km: 7.5, Min: 12}
	id, err := store.CreateRouteTemplate(ctx, pool, templateParams(t, pool, driverID, want))
	if err != nil {
		t.Fatalf("CreateRouteTemplate: %v", err)
	}

	var got match.Budget
	if err := pool.QueryRow(ctx,
		`SELECT allow_stops, detour_km, detour_min FROM route_templates WHERE id = $1`, id).
		Scan(&got.AllowStops, &got.Km, &got.Min); err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if got != want {
		t.Errorf("stored %+v, want %+v", got, want)
	}
}

// The other answer, because a bool that is only ever tested true is a bool
// nobody has tested. A driver who says no stops must have that stored, not the
// column default.
func TestARouteThatRefusesStopsSaysSo(t *testing.T) {
	ctx := context.Background()
	pool, driverID := driverAndPool(t)

	id, err := store.CreateRouteTemplate(ctx, pool,
		templateParams(t, pool, driverID, match.Budget{AllowStops: false, Km: 0, Min: 0}))
	if err != nil {
		t.Fatalf("CreateRouteTemplate: %v", err)
	}

	var allow bool
	if err := pool.QueryRow(ctx,
		`SELECT allow_stops FROM route_templates WHERE id = $1`, id).Scan(&allow); err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if allow {
		t.Error("a driver who refused intermediate stops was stored as accepting them")
	}
}

// A driver who had set a detour budget of zero had said, in the only way the
// old schema allowed, that they wanted no detours. The column default is true,
// so without a backfill the migration reverses what they had chosen — quietly,
// on somebody else's account.
func TestTheDetourMigrationKeepsWhatDriversHadAlreadyChosen(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)

	// Stop just before the migration under test, so the rows below are the
	// rows a real deployment would be carrying when it runs.
	if err := store.MigrateTo(ctx, pool, 15); err != nil {
		t.Fatalf("MigrateTo(15): %v", err)
	}

	// The price column is still mandatory at this point in the history — 00019
	// is what gives it a default, and this test deliberately stops before it.
	var regionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	seed := func(phone string, budget int) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx,
			// No date_of_birth here: this test stops at migration 15 on purpose,
			// and the column does not arrive until 00025.
			`INSERT INTO users (phone, name, home_region_id) VALUES ($1, 'Ο', $2) RETURNING id`,
			phone, regionID).Scan(&id); err != nil {
			t.Fatalf("seeding user: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO driver_profiles (user_id, car_model, car_plate, detour_budget_min)
			VALUES ($1, 'Yaris', 'ABC-1234', $2)`, id, budget); err != nil {
			t.Fatalf("seeding driver profile: %v", err)
		}
		return id
	}
	refuses := seed("+306900000001", 0)
	accepts := seed("+306900000002", 20)

	// Up to 00016 and no further. That is the migration under test, and 00025
	// deliberately refuses to run over accounts that predate it — an account
	// with no recorded age is exactly what the age gate exists to prevent, and
	// there is no honest value to backfill one with.
	if err := store.MigrateTo(ctx, pool, 16); err != nil {
		t.Fatalf("MigrateTo(16): %v", err)
	}

	for _, tc := range []struct {
		name  string
		id    uuid.UUID
		allow bool
		min   int
	}{
		{"a driver who wanted none", refuses, false, 0},
		{"a driver who allowed twenty minutes", accepts, true, 20},
	} {
		var allow bool
		var min int
		if err := pool.QueryRow(ctx,
			`SELECT default_allow_stops, detour_budget_min FROM driver_profiles WHERE user_id = $1`,
			tc.id).Scan(&allow, &min); err != nil {
			t.Fatalf("%s: reading back: %v", tc.name, err)
		}
		if allow != tc.allow || min != tc.min {
			t.Errorf("%s: default_allow_stops=%v detour_budget_min=%d, want %v and %d",
				tc.name, allow, min, tc.allow, tc.min)
		}
	}
}

// The car a route names is the one it is published with, and it must be the
// driver's own.
//
// The second half is a composite foreign key rather than a check in a handler:
// a route naming somebody else's Yaris would price a trip off a car that is not
// there, and "the handler remembers" is not the same guarantee as "the database
// refuses".
func TestARouteCarriesTheCarItWasPublishedWith(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_, driverID := seedDriver(t, pool, "+306900000401")

	var second uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO vehicles (owner_id, make, model, fuel, consumption_l_100km,
			consumption_source, plate, is_default)
		VALUES ($1, 'Fiat', 'Ducato', 'diesel', 9.0, 'dataset', 'ΖΗΘ-9999', false)
		RETURNING id`, driverID).Scan(&second); err != nil {
		t.Fatalf("adding a second car: %v", err)
	}

	params := templateParams(t, pool, driverID, match.Budget{AllowStops: true, Km: 15, Min: 20})
	params.VehicleID = second
	id, err := store.CreateRouteTemplate(ctx, pool, params)
	if err != nil {
		t.Fatalf("CreateRouteTemplate: %v", err)
	}

	var stored uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT vehicle_id FROM route_templates WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("reading the route: %v", err)
	}
	if stored != second {
		t.Errorf("the route went out in %v, want the van %v", stored, second)
	}
}

func TestARouteCannotBePublishedWithSomebodyElsesCar(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_, driverID := seedDriver(t, pool, "+306900000402")

	var strangerID, theirCar uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		SELECT DATE '1990-01-01', '+306900000403', 'Άλλος', home_region_id FROM users WHERE id = $1
		RETURNING id`, driverID).Scan(&strangerID); err != nil {
		t.Fatalf("seeding a stranger: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vehicles (owner_id, make, model, fuel, consumption_l_100km,
			consumption_source, plate, is_default)
		VALUES ($1, 'Fiat', 'Ducato', 'diesel', 9.0, 'dataset', 'ΖΗΘ-1111', true)
		RETURNING id`, strangerID).Scan(&theirCar); err != nil {
		t.Fatalf("seeding their car: %v", err)
	}

	params := templateParams(t, pool, driverID, match.Budget{AllowStops: true, Km: 15, Min: 20})
	params.VehicleID = theirCar
	if _, err := store.CreateRouteTemplate(ctx, pool, params); err == nil {
		t.Fatal("a route was published with somebody else's car")
	}
}
