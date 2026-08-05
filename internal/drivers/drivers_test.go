package drivers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/drivers"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
	"carpool/internal/vehicles"
)

func seed(t *testing.T) (*pgxpool.Pool, *drivers.Service, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID, userID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&regionID); err != nil {
		t.Fatalf("seeding the region: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		VALUES ('+306900000001', 'Οδηγός', $1) RETURNING id`, regionID).Scan(&userID); err != nil {
		t.Fatalf("seeding the user: %v", err)
	}

	fixed := clock.NewFake(time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	return pool, drivers.NewService(pool, fixed), userID
}

func stored(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) (l float64, source string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT fuel_consumption_l_100km, consumption_source FROM driver_profiles WHERE user_id = $1`,
		userID).Scan(&l, &source); err != nil {
		t.Fatalf("reading the profile: %v", err)
	}
	return l, source
}

func TestAKnownModelIsPricedFromTheDataset(t *testing.T) {
	pool, svc, userID := seed(t)

	got, err := svc.Upsert(context.Background(), userID, drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol_hybrid",
		EngineCC: 1490, Plate: "ΑΒΓ-1234",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Source != vehicles.SourceDataset || !got.Verified {
		t.Errorf("source %q verified %v, want a verified dataset match", got.Source, got.Verified)
	}

	l, source := stored(t, pool, userID)
	if source != "dataset" {
		t.Errorf("consumption_source = %q, want dataset", source)
	}
	if l != got.LPer100Km {
		t.Errorf("stored %v, assessed %v", l, got.LPer100Km)
	}
}

// The pre-2021 car, which is most of the fleet this pilot will meet.
func TestAnOlderCarIsPricedFromTheFigureOnItsDocument(t *testing.T) {
	pool, svc, userID := seed(t)

	if _, err := svc.Upsert(context.Background(), userID, drivers.Car{
		Make: "Toyota", Model: "Corolla Verso", Fuel: "diesel",
		EngineCC: 2000, CO2GramsPerKm: 160, Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	l, source := stored(t, pool, userID)
	if source != "co2" {
		t.Fatalf("consumption_source = %q, want co2", source)
	}
	if l < 6.0 || l > 6.2 {
		t.Errorf("160 g/km of diesel stored as %v l/100km, want about 6.08", l)
	}
}

// A car we know nothing specific about must end up cheaper than the old flat
// default, not more expensive, and must not claim to be verified.
func TestACarWeCannotIdentifyIsPricedLowAndMarkedUnverified(t *testing.T) {
	pool, svc, userID := seed(t)

	got, err := svc.Upsert(context.Background(), userID, drivers.Car{
		Make: "Lada", Model: "Niva", Fuel: "petrol",
		EngineCC: 1700, Plate: "ΑΒΓ-1234",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Verified {
		t.Error("a class floor reported itself as verified")
	}

	l, source := stored(t, pool, userID)
	if source != "band" {
		t.Errorf("consumption_source = %q, want band", source)
	}
	if l >= 7.0 {
		t.Errorf("stored %v l/100km, which is no better than the 7.0 flat default it replaces", l)
	}
}

// The whole point of the package. A driver describes the car; they do not get
// to state what it drinks, and there is no field through which they could.
func TestTheDriverCannotStateTheConsumption(t *testing.T) {
	pool, svc, userID := seed(t)

	thirsty := drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol_hybrid",
		EngineCC: 1490, Plate: "ΑΒΓ-1234",
	}
	if _, err := svc.Upsert(context.Background(), userID, thirsty); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	first, _ := stored(t, pool, userID)

	// Everything a driver can send, sent again with the numbers they would most
	// want changed. The consumption has to land in the same place.
	thirsty.Colour = "μπλε"
	thirsty.CO2GramsPerKm = 400 // would imply 17.6 l/100km if it were believed
	if _, err := svc.Upsert(context.Background(), userID, thirsty); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	second, _ := stored(t, pool, userID)

	if second != first {
		t.Errorf("consumption moved from %v to %v because the driver changed what they told us",
			first, second)
	}
}

func TestChangingCarReassessesRatherThanKeepingTheOldFigure(t *testing.T) {
	pool, svc, userID := seed(t)
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, userID, drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol_hybrid",
		EngineCC: 1490, Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	small, _ := stored(t, pool, userID)

	if _, err := svc.Upsert(ctx, userID, drivers.Car{
		Make: "Volkswagen", Model: "Golf", Fuel: "petrol",
		EngineCC: 1984, Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("Upsert the second car: %v", err)
	}
	big, _ := stored(t, pool, userID)

	if big <= small {
		t.Errorf("swapping a hybrid Yaris for a 2.0 Golf moved consumption %v -> %v", small, big)
	}
}

func TestACarThatCannotBePricedIsRefused(t *testing.T) {
	_, svc, userID := seed(t)
	ctx := context.Background()

	cases := map[string]drivers.Car{
		"no make":         {Model: "Yaris", Fuel: "petrol", EngineCC: 1000, Plate: "Α-1"},
		"no model":        {Make: "Toyota", Fuel: "petrol", EngineCC: 1000, Plate: "Α-1"},
		"no plate":        {Make: "Toyota", Model: "Yaris", Fuel: "petrol", EngineCC: 1000},
		"electric":        {Make: "Tesla", Model: "3", Fuel: "electric", EngineCC: 1, Plate: "Α-1"},
		"lpg":             {Make: "Fiat", Model: "Panda", Fuel: "lpg", EngineCC: 1200, Plate: "Α-1"},
		"no displacement": {Make: "Toyota", Model: "Yaris", Fuel: "petrol", Plate: "Α-1"},
	}
	for name, car := range cases {
		if _, err := svc.Upsert(ctx, userID, car); !errors.Is(err, drivers.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
