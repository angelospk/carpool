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

	var driverID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, home_region_id) VALUES ('+306900000001', 'Οδηγός', $1) RETURNING id`,
		regionID).Scan(&driverID); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris', 'ABC-1234')`,
		driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	return pool, driverID
}

func templateParams(driverID uuid.UUID, detour match.Budget) store.CreateRouteTemplateParams {
	return store.CreateRouteTemplateParams{
		DriverID:         driverID,
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
	id, err := store.CreateRouteTemplate(ctx, pool, templateParams(driverID, want))
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
		templateParams(driverID, match.Budget{AllowStops: false, Km: 0, Min: 0}))
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
