package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/vehicles"
)

// ErrNoFuelPrice means nothing is on record for that fuel, nationally or
// locally. It is a misconfiguration rather than a user error: a deployment
// without fuel prices cannot suggest a contribution for anybody.
var ErrNoFuelPrice = errors.New("store: no fuel price on record")

// FuelPrice is what a litre costs, and how much that answer is worth.
type FuelPrice struct {
	PriceEUR   float64
	RegionID   uuid.UUID
	RegionName string
	// Regional distinguishes a figure somebody looked up for this prefecture
	// from the national one standing in for it. The difference travels into the
	// booking's receipt: a price built on a national average should not read
	// back a year later as though somebody had checked the pump in Ρόδο.
	Regional bool
	Source   string
	// UpdatedAt is when this figure was last moved. A contribution built on a
	// six-month-old price is worth less than one built on last week's, and only
	// this says which it is.
	UpdatedAt time.Time
}

// FuelPrices is a national figure per liquid, which is how the app is seeded.
type FuelPrices struct {
	PetrolEUR float64
	DieselEUR float64
	// Source says where the numbers came from, in words. A price nobody can
	// trace is a price nobody can correct.
	Source string
}

// liquid maps a car's fuel class onto the thing that has a price at the pump.
//
// A hybrid burns petrol or diesel and pays what everyone else pays for it. That
// it burns less is what its consumption figure says; giving it its own €/litre
// would be saying the same thing twice, in the same direction.
func liquid(f vehicles.Fuel) (string, error) {
	switch f {
	case vehicles.Petrol, vehicles.PetrolHybrid:
		return "petrol", nil
	case vehicles.Diesel, vehicles.DieselHybrid:
		return "diesel", nil
	default:
		return "", fmt.Errorf("store: no pump price for fuel %q", f)
	}
}

// FuelPriceAt is what a litre costs where a trip starts.
//
// Where the trip starts, not where the driver lives. Those were the same thing
// while the app ran in one prefecture; nationally they are not, and pricing a
// Αθήνα → Πάτρα run off the driver's address in Έβρος was answering a question
// nobody asked. The origin is the choice because it is stable, because it is
// the one endpoint of a trip that is settled before anybody books, and because
// it is roughly where the tank was filled.
func FuelPriceAt(ctx context.Context, pool *pgxpool.Pool, lon, lat float64, fuel vehicles.Fuel) (FuelPrice, error) {
	kind, err := liquid(fuel)
	if err != nil {
		return FuelPrice{}, err
	}

	region, err := RegionAt(ctx, pool, lon, lat)
	if err != nil {
		return FuelPrice{}, err
	}

	out := FuelPrice{RegionID: region.ID, RegionName: region.Name}
	err = pool.QueryRow(ctx, `
		SELECT price_eur, region_id IS NOT NULL, source, updated_at
		FROM fuel_prices
		WHERE fuel = $1 AND (region_id = $2 OR region_id IS NULL)
		-- The specific figure wins; the national one stands in when nobody has
		-- looked this prefecture up.
		ORDER BY region_id IS NULL
		LIMIT 1`, kind, region.ID).Scan(&out.PriceEUR, &out.Regional, &out.Source, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FuelPrice{}, ErrNoFuelPrice
	}
	if err != nil {
		return FuelPrice{}, fmt.Errorf("store: reading the fuel price: %w", err)
	}
	return out, nil
}

// SeedFuelPrices writes the national figures, and is how they are corrected.
//
// Idempotent, and the timestamp moves only when a number does — restating the
// same price has not made it any fresher, and stamping it anyway would let a
// figure from last winter read as this week's.
func SeedFuelPrices(ctx context.Context, pool *pgxpool.Pool, p FuelPrices) error {
	if p.PetrolEUR <= 0 || p.DieselEUR <= 0 {
		return fmt.Errorf("store: fuel prices must be positive, got petrol %v diesel %v",
			p.PetrolEUR, p.DieselEUR)
	}
	if p.Source == "" {
		return errors.New("store: a fuel price needs a source")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for kind, price := range map[string]float64{"petrol": p.PetrolEUR, "diesel": p.DieselEUR} {
		_, err := tx.Exec(ctx, `
			INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
			VALUES (NULL, $1, $2, $3)
			ON CONFLICT (fuel) WHERE region_id IS NULL DO UPDATE
			SET price_eur  = EXCLUDED.price_eur,
			    source     = EXCLUDED.source,
			    updated_at = CASE
			        WHEN fuel_prices.price_eur IS DISTINCT FROM EXCLUDED.price_eur
			        THEN now() ELSE fuel_prices.updated_at END`,
			kind, price, p.Source)
		if err != nil {
			return fmt.Errorf("store: seeding the %s price: %w", kind, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
