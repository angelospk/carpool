// Package drivers turns "here is my car" into a driver profile.
//
// The whole reason it exists is that the fuel consumption in that profile is
// not something the driver gets to say. They describe the car — the things
// printed on the άδεια κυκλοφορίας — and internal/vehicles works out what it
// drinks. See docs/pricing.md for why that distinction is load-bearing rather
// than fussy.
//
// Before this there was no way to become a driver at all: scripts/e2e.sh
// inserted the row with psql and the API only ever read it.
package drivers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/vehicles"
)

// ErrInvalid means the submitted car cannot be made sense of.
var ErrInvalid = errors.New("drivers: that is not a car we can price")

// Service reads and writes driver profiles.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Car is what a driver submits. Every field is copied off the registration
// document, which is the point: none of it is the driver's judgement, and all
// of it can be checked against the paper afterwards.
type Car struct {
	Make, Model string
	Fuel        string
	EngineCC    int
	// CO2GramsPerKm is field V.7, zero when the document has no such field.
	CO2GramsPerKm int

	Colour, Plate string

	// Label is what the driver calls this one, so that two Yarises are tellable
	// apart in a list. Optional; the make and model are the fallback.
	Label string
}

// Profile is a driver profile as stored.
type Profile struct {
	Car
	LPer100Km float64
	Source    vehicles.Source
	// Verified is false when the figure is a floor for the car's class rather
	// than anything specific to the car. Such a profile under-charges on
	// purpose, and a review that identifies the car raises it.
	Verified bool
}

// Upsert records the driver's car: the profile row, and the car itself.
//
// This is the one-car path — "here is what I drive" — and it stays because it
// is what most drivers do and what the app asks for when somebody first offers
// a ride. A driver who keeps two adds the second through the garage, and this
// then edits whichever is their default.
//
// It is the only writer of fuel_consumption_l_100km, which remains on the
// profile as the legacy column. Pricing reads the vehicle.
func (s *Service) Upsert(ctx context.Context, userID uuid.UUID, car Car) (Profile, error) {
	car.Make = strings.TrimSpace(car.Make)
	car.Model = strings.TrimSpace(car.Model)
	car.Plate = strings.TrimSpace(car.Plate)

	if car.Make == "" || car.Model == "" {
		return Profile{}, fmt.Errorf("%w: a make and a model are needed", ErrInvalid)
	}
	if car.Plate == "" {
		return Profile{}, fmt.Errorf("%w: a plate is needed so a passenger can find the car", ErrInvalid)
	}

	fuel := vehicles.Fuel(car.Fuel)
	known := false
	for _, f := range vehicles.Fuels() {
		known = known || f == fuel
	}
	if !known {
		return Profile{}, fmt.Errorf("%w: %q is not a fuel this app can price", ErrInvalid, car.Fuel)
	}

	assessed, err := vehicles.Assess(vehicles.Car{
		Make: car.Make, Model: car.Model, Fuel: fuel,
		EngineCC: car.EngineCC, CO2GramsPerKm: car.CO2GramsPerKm,
	})
	if errors.Is(err, vehicles.ErrUnusable) {
		return Profile{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("drivers: assessing the car: %w", err)
	}

	// The model and make are stored as the driver wrote them, because that is
	// what a passenger will be looking for at the kerb. The folded forms only
	// exist inside the lookup.
	now := s.clock.Now()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO driver_profiles (
			user_id, car_model, car_color, car_plate,
			vehicle_make, vehicle_model, vehicle_fuel, engine_cc, co2_g_km,
			fuel_consumption_l_100km, consumption_source, assessed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id) DO UPDATE SET
			car_model = EXCLUDED.car_model,
			car_color = EXCLUDED.car_color,
			car_plate = EXCLUDED.car_plate,
			vehicle_make = EXCLUDED.vehicle_make,
			vehicle_model = EXCLUDED.vehicle_model,
			vehicle_fuel = EXCLUDED.vehicle_fuel,
			engine_cc = EXCLUDED.engine_cc,
			co2_g_km = EXCLUDED.co2_g_km,
			fuel_consumption_l_100km = EXCLUDED.fuel_consumption_l_100km,
			consumption_source = EXCLUDED.consumption_source,
			assessed_at = EXCLUDED.assessed_at,
			updated_at = now()`,
		userID,
		strings.TrimSpace(car.Make+" "+car.Model), car.Colour, car.Plate,
		car.Make, car.Model, string(fuel), nullableCC(car.EngineCC), nullableCO2(car.CO2GramsPerKm),
		assessed.LPer100Km, string(assessed.Source), now,
	); err != nil {
		return Profile{}, fmt.Errorf("drivers: saving the profile: %w", err)
	}

	// And the car itself, as their default. Without this a driver could set up
	// a profile and then have nothing to publish a route with, because a route
	// names a vehicle and there would be none.
	//
	// Upserted on the default rather than inserted, so that editing a profile
	// twice does not fill the garage with near-identical Yarises.
	capped, _ := atMost(assessed.LPer100Km, fuel)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO vehicles (owner_id, make, model, fuel, engine_cc, co2_g_km,
			consumption_l_100km, consumption_source, assessed_at, colour, plate, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,true)
		ON CONFLICT (owner_id) WHERE is_default AND retired_at IS NULL DO UPDATE SET
			make = EXCLUDED.make, model = EXCLUDED.model, fuel = EXCLUDED.fuel,
			engine_cc = EXCLUDED.engine_cc, co2_g_km = EXCLUDED.co2_g_km,
			consumption_l_100km = EXCLUDED.consumption_l_100km,
			consumption_source = EXCLUDED.consumption_source,
			assessed_at = EXCLUDED.assessed_at,
			colour = EXCLUDED.colour, plate = EXCLUDED.plate,
			updated_at = now()`,
		userID, car.Make, car.Model, string(fuel), nullableCC(car.EngineCC), nullableCO2(car.CO2GramsPerKm),
		capped, string(assessed.Source), now, car.Colour, car.Plate); err != nil {
		return Profile{}, fmt.Errorf("drivers: saving the car: %w", err)
	}

	return Profile{
		Car:       car,
		LPer100Km: assessed.LPer100Km,
		Source:    assessed.Source,
		Verified:  assessed.Verified(),
	}, nil
}

// The two nullable columns are nullable because "not stated" and "stated as
// zero" are different, and only the first is allowed.
func nullableCC(cc int) *int {
	if cc <= 0 {
		return nil
	}
	return &cc
}

func nullableCO2(g int) *int {
	if g <= 0 {
		return nil
	}
	return &g
}
