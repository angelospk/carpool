package store_test

import (
	"context"
	"testing"

	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

// The suggested contribution multiplies the fuel cost by wear_factor, and the
// column used to allow anything up to 1.5. Nothing in the API ever wrote it, so
// the exposure was latent rather than live: the day somebody adds the handler,
// a driver can ask for 50% above what the trip burned and the pricing code will
// hand them the number.
//
// The constraint is what stops that handler from ever being written by
// accident. See migration 00008 for why the multiplier is the wrong shape for
// wear in the first place.
func TestTheWearMultiplierCannotBeRaisedAboveCost(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID, driverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding the region: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', '+306900000001', 'Οδηγός', $1)
		RETURNING id`, regionID).Scan(&driverID); err != nil {
		t.Fatalf("seeding the driver: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Yaris', 'ABC-1234')`, driverID); err != nil {
		t.Fatalf("seeding the profile: %v", err)
	}

	var wear float64
	if err := pool.QueryRow(ctx,
		`SELECT wear_factor FROM driver_profiles WHERE user_id = $1`, driverID).Scan(&wear); err != nil {
		t.Fatalf("reading the default: %v", err)
	}
	if wear != 1.0 {
		t.Errorf("a new driver starts at wear_factor %v, want 1.0", wear)
	}

	// 1.5 was the old ceiling, so it is the value a reinstated handler would
	// most plausibly accept. 1.01 is here because a constraint written as a
	// range rather than an equality would let it through.
	for _, raised := range []float64{1.5, 1.01} {
		if _, err := pool.Exec(ctx,
			`UPDATE driver_profiles SET wear_factor = $2 WHERE user_id = $1`,
			driverID, raised); err == nil {
			t.Errorf("wear_factor accepted %v: a driver can charge %.0f%% above the fuel",
				raised, (raised-1)*100)
		}
	}
}
