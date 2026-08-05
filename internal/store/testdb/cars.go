package testdb

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GiveEveryDriverACar puts one default vehicle in the garage of every driver
// profile in the database.
//
// Routes name the car they go out in, and the column is NOT NULL, so a fixture
// that seeds a driver and then a route has to seed a car in between. Almost
// none of them care which car — they care about detours, cancellations,
// arrangements — so rather than eleven copies of the same INSERT they call this
// and get a plausible small petrol hatchback.
//
// A fixture that does care builds its own vehicles first; this only fills the
// gaps, so calling it afterwards changes nothing.
func GiveEveryDriverACar(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO vehicles (owner_id, make, model, fuel, engine_cc,
			consumption_l_100km, consumption_source, plate, colour, is_default)
		SELECT d.user_id, 'Toyota', 'Yaris', 'petrol', 1329,
		       5.6, 'dataset', coalesce(d.car_plate, 'ΑΒΓ-0000'), d.car_color, true
		  FROM driver_profiles d
		 WHERE NOT EXISTS (
		       SELECT 1 FROM vehicles v WHERE v.owner_id = d.user_id AND v.retired_at IS NULL)`); err != nil {
		t.Fatalf("giving the drivers cars: %v", err)
	}
}

// DefaultCar returns the id of a driver's default vehicle, putting one in the
// garage if they have none.
//
// A route names the car it goes out in, so a fixture that builds a template
// through store.CreateRouteTemplate needs an id to hand it. Most fixtures do
// not care which car; this gives them one and gets out of the way.
func DefaultCar(t *testing.T, pool *pgxpool.Pool, driverID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var id uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO vehicles (owner_id, make, model, fuel, engine_cc,
			consumption_l_100km, consumption_source, plate, is_default)
		SELECT $1, 'Toyota', 'Yaris', 'petrol', 1329, 5.6, 'dataset', 'ΑΒΓ-0000', true
		 WHERE NOT EXISTS (
		       SELECT 1 FROM vehicles WHERE owner_id = $1 AND retired_at IS NULL)
		RETURNING id`, driverID).Scan(&id)
	if err == nil {
		return id
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("giving %s a car: %v", driverID, err)
	}

	// They already had one.
	if err := pool.QueryRow(ctx, `
		SELECT id FROM vehicles
		 WHERE owner_id = $1 AND retired_at IS NULL
		 ORDER BY is_default DESC, created_at LIMIT 1`, driverID).Scan(&id); err != nil {
		t.Fatalf("finding %s's car: %v", driverID, err)
	}
	return id
}
