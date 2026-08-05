package drivers_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/drivers"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

type garageFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	svc      *drivers.Service
	driver   uuid.UUID
	stranger uuid.UUID
}

func newGarageFixture(t *testing.T) *garageFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	f := &garageFixture{t: t, pool: pool,
		svc: drivers.NewService(pool, clock.NewFake(time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)))}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding %q: %v", sql, err)
		}
	}

	var regionID uuid.UUID
	scan(&regionID, `INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`)
	scan(&f.driver, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001','Οδηγός',$1) RETURNING id`, regionID)
	scan(&f.stranger, `INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000002','Άγνωστος',$1) RETURNING id`, regionID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1,'Yaris','ΑΒΓ-0000')`,
		f.driver); err != nil {
		t.Fatalf("seeding the profile: %v", err)
	}
	return f
}

// publishRoute puts a live route behind a car, so retiring it has consequences.
func (f *garageFixture) publishRoute(vehicleID uuid.UUID) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination,
			origin_label, destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, $2, ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, f.driver, vehicleID).Scan(&id); err != nil {
		f.t.Fatalf("publishing a route: %v", err)
	}
	return id
}
