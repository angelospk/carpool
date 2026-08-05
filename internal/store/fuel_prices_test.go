package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/store"
	"carpool/internal/vehicles"
)

func seededPrices(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := seededGreece(t)
	if err := store.SeedFuelPrices(ctx, pool, store.FuelPrices{
		PetrolEUR: 1.859,
		DieselEUR: 1.729,
		Source:    "test",
	}); err != nil {
		t.Fatalf("SeedFuelPrices: %v", err)
	}
	return ctx, pool
}

// The bug. One price per region meant a diesel was quoted as though it drank
// petrol, on top of a consumption figure that is already fuel-specific.
func TestTheFuelPriceDependsOnWhatTheCarBurns(t *testing.T) {
	ctx, pool := seededPrices(t)

	// Φέρες.
	petrol, err := store.FuelPriceAt(ctx, pool, 26.1740, 40.8940, vehicles.Petrol)
	if err != nil {
		t.Fatalf("petrol: %v", err)
	}
	diesel, err := store.FuelPriceAt(ctx, pool, 26.1740, 40.8940, vehicles.Diesel)
	if err != nil {
		t.Fatalf("diesel: %v", err)
	}

	if petrol.PriceEUR != 1.859 {
		t.Errorf("petrol = %v, want 1.859", petrol.PriceEUR)
	}
	if diesel.PriceEUR != 1.729 {
		t.Errorf("diesel = %v, want 1.729", diesel.PriceEUR)
	}
	if petrol.RegionName != "Περιφερειακή Ενότητα Έβρου" {
		t.Errorf("region = %q", petrol.RegionName)
	}
}

// A hybrid burns petrol at the price everybody else pays for petrol. That it
// burns less of it is what the consumption figure says; charging it a different
// rate per litre would be saying the same thing twice.
func TestAHybridPaysThePumpPriceOfWhatItBurns(t *testing.T) {
	ctx, pool := seededPrices(t)

	for _, tt := range []struct {
		fuel vehicles.Fuel
		want float64
	}{
		{vehicles.PetrolHybrid, 1.859},
		{vehicles.DieselHybrid, 1.729},
	} {
		got, err := store.FuelPriceAt(ctx, pool, 26.1740, 40.8940, tt.fuel)
		if err != nil {
			t.Fatalf("%s: %v", tt.fuel, err)
		}
		if got.PriceEUR != tt.want {
			t.Errorf("%s = %v, want %v", tt.fuel, got.PriceEUR, tt.want)
		}
	}
}

// Nothing fetches the Παρατηρητήριο's per-νομό bulletin, so there is one honest
// national figure rather than seventy-five copies of it pretending to be local.
// A region only differs when somebody has gone and looked one up.
func TestARegionalPriceOverridesTheNationalOneOnlyWhereItExists(t *testing.T) {
	ctx, pool := seededPrices(t)

	var rhodes string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM regions WHERE name = 'Περιφερειακή Ενότητα Ρόδου'`).Scan(&rhodes); err != nil {
		t.Fatalf("finding Ρόδος: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES ($1, 'petrol', 2.019, 'checked at the pump')`, rhodes); err != nil {
		t.Fatalf("setting the island price: %v", err)
	}

	// Ρόδος town.
	island, err := store.FuelPriceAt(ctx, pool, 28.2278, 36.4341, vehicles.Petrol)
	if err != nil {
		t.Fatalf("Ρόδος: %v", err)
	}
	if island.PriceEUR != 2.019 {
		t.Errorf("Ρόδος petrol = %v, want the island override 2.019", island.PriceEUR)
	}
	if !island.Regional {
		t.Error("the island price did not report itself as a regional figure")
	}

	// Diesel there has no override, so it falls back — and says so.
	fallback, err := store.FuelPriceAt(ctx, pool, 28.2278, 36.4341, vehicles.Diesel)
	if err != nil {
		t.Fatalf("Ρόδος diesel: %v", err)
	}
	if fallback.PriceEUR != 1.729 {
		t.Errorf("Ρόδος diesel = %v, want the national 1.729", fallback.PriceEUR)
	}
	if fallback.Regional {
		t.Error("a national figure reported itself as regional, which would make a guess look local")
	}
}

func TestThereIsNoPriceOutsideTheCountry(t *testing.T) {
	ctx, pool := seededPrices(t)

	if _, err := store.FuelPriceAt(ctx, pool, 23.3219, 42.6977, vehicles.Petrol); !errors.Is(err, store.ErrOutsideGreece) {
		t.Errorf("Σόφια gave %v, want ErrOutsideGreece", err)
	}
}

// Reseeding is how a price is corrected, and the timestamp is the only thing
// that says whether a figure is this week's or last winter's. Restating the
// same number has not made it any fresher.
func TestSeedingFuelPricesMovesTheStampOnlyWhenThePriceMoves(t *testing.T) {
	ctx, pool := seededPrices(t)

	stamp := func() string {
		t.Helper()
		var at string
		if err := pool.QueryRow(ctx,
			`SELECT updated_at::text FROM fuel_prices WHERE region_id IS NULL AND fuel = 'petrol'`).Scan(&at); err != nil {
			t.Fatalf("reading the stamp: %v", err)
		}
		return at
	}
	first := stamp()

	if err := store.SeedFuelPrices(ctx, pool, store.FuelPrices{
		PetrolEUR: 1.859, DieselEUR: 1.729, Source: "test",
	}); err != nil {
		t.Fatalf("reseeding the same prices: %v", err)
	}
	if again := stamp(); again != first {
		t.Errorf("restating the same price moved the stamp, so a stale figure now looks current")
	}

	if err := store.SeedFuelPrices(ctx, pool, store.FuelPrices{
		PetrolEUR: 1.899, DieselEUR: 1.729, Source: "test",
	}); err != nil {
		t.Fatalf("reseeding a new price: %v", err)
	}
	if moved := stamp(); moved == first {
		t.Errorf("the petrol price changed but the stamp stayed at %v", moved)
	}
}
