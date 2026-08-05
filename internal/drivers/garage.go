package drivers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/vehicles"
)

// MaxVehicles is how many cars one person may keep.
//
// A garage, not a fleet. Five is more than anybody sharing a commute has, and
// an unbounded list is an invitation to keep one of everything and publish with
// whichever prices best.
const MaxVehicles = 5

var (
	// ErrTooManyVehicles means the garage is full.
	ErrTooManyVehicles = errors.New("drivers: that is as many cars as one account may keep")
	// ErrNoVehicle means there is no such car, or it is not this person's. The
	// two are one error: somebody poking at ids should not learn which.
	ErrNoVehicle = errors.New("drivers: no such car")
	// ErrVehicleInUse means a live route still names it, and retiring it would
	// leave that route unable to be priced.
	ErrVehicleInUse = errors.New("drivers: a route still uses that car")
)

// Vehicle is one car in somebody's garage.
type Vehicle struct {
	ID    uuid.UUID
	Owner uuid.UUID
	Car
	LPer100Km float64
	Source    vehicles.Source
	// Capped says the figure was brought down to the pricing ceiling for its
	// fuel class. The car may well drink what the dataset says; this is the
	// most that may be charged for. See vehicles.PricingCeiling.
	Capped    bool
	IsDefault bool
	RetiredAt *time.Time
}

// Name is what to show in a list: what the driver calls it, or the car.
func (v Vehicle) Name() string {
	if v.Label != "" {
		return v.Label
	}
	return strings.TrimSpace(v.Make + " " + v.Model)
}

// AddVehicle assesses a car and puts it in the garage.
//
// The first car a driver adds becomes their default, so that somebody with one
// car never has to choose one.
func (s *Service) AddVehicle(ctx context.Context, ownerID uuid.UUID, car Car) (Vehicle, error) {
	assessed, fuel, car, err := s.assess(car)
	if err != nil {
		return Vehicle{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Vehicle{}, fmt.Errorf("drivers: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The owner's row is locked so that two requests arriving together cannot
	// both count four cars, and cannot both decide they are the first.
	var locked bool
	if err := tx.QueryRow(ctx,
		`SELECT true FROM users WHERE id = $1 FOR UPDATE`, ownerID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Vehicle{}, ErrNoVehicle
		}
		return Vehicle{}, fmt.Errorf("drivers: locking the owner: %w", err)
	}

	var live int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM vehicles WHERE owner_id = $1 AND retired_at IS NULL`,
		ownerID).Scan(&live); err != nil {
		return Vehicle{}, fmt.Errorf("drivers: counting the garage: %w", err)
	}
	if live >= MaxVehicles {
		return Vehicle{}, ErrTooManyVehicles
	}

	capped, wasCapped := atMost(assessed.LPer100Km, fuel)

	v := Vehicle{Owner: ownerID, Car: car, LPer100Km: capped, Source: assessed.Source,
		Capped: wasCapped, IsDefault: live == 0}
	err = tx.QueryRow(ctx, `
		INSERT INTO vehicles (owner_id, make, model, fuel, engine_cc, co2_g_km,
			consumption_l_100km, consumption_source, assessed_at, label, colour, plate, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id`,
		ownerID, car.Make, car.Model, string(fuel), nullableCC(car.EngineCC), nullableCO2(car.CO2GramsPerKm),
		capped, string(assessed.Source), s.clock.Now(), nullableText(car.Label), car.Colour, car.Plate,
		v.IsDefault).Scan(&v.ID)
	if err != nil {
		return Vehicle{}, fmt.Errorf("drivers: saving the car: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Vehicle{}, fmt.Errorf("drivers: commit: %w", err)
	}
	return v, nil
}

// Vehicles lists the cars somebody still has, default first.
func (s *Service) Vehicles(ctx context.Context, ownerID uuid.UUID) ([]Vehicle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, make, model, fuel, coalesce(engine_cc, 0), coalesce(co2_g_km, 0),
		       consumption_l_100km, consumption_source, coalesce(label, ''),
		       coalesce(colour, ''), plate, is_default, retired_at
		FROM vehicles
		WHERE owner_id = $1 AND retired_at IS NULL
		ORDER BY is_default DESC, created_at`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("drivers: listing the garage: %w", err)
	}
	defer rows.Close()

	var out []Vehicle
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Vehicle loads one car, and only if it is theirs.
func (s *Service) Vehicle(ctx context.Context, ownerID, vehicleID uuid.UUID) (Vehicle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, make, model, fuel, coalesce(engine_cc, 0), coalesce(co2_g_km, 0),
		       consumption_l_100km, consumption_source, coalesce(label, ''),
		       coalesce(colour, ''), plate, is_default, retired_at
		FROM vehicles WHERE id = $1 AND owner_id = $2`, vehicleID, ownerID)
	if err != nil {
		return Vehicle{}, fmt.Errorf("drivers: loading the car: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Vehicle{}, fmt.Errorf("drivers: loading the car: %w", err)
		}
		return Vehicle{}, ErrNoVehicle
	}
	return scanVehicle(rows)
}

// DefaultVehicle is the car a route is published with when the driver does not
// say which.
func (s *Service) DefaultVehicle(ctx context.Context, ownerID uuid.UUID) (Vehicle, error) {
	live, err := s.Vehicles(ctx, ownerID)
	if err != nil {
		return Vehicle{}, err
	}
	if len(live) == 0 {
		return Vehicle{}, ErrNoVehicle
	}
	// Vehicles orders the default first, and there is at most one by index.
	return live[0], nil
}

// SetDefaultVehicle moves the default. Both statements are in one transaction
// because the partial unique index would refuse the second on its own.
func (s *Service) SetDefaultVehicle(ctx context.Context, ownerID, vehicleID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("drivers: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var mine bool
	err = tx.QueryRow(ctx, `
		SELECT true FROM vehicles
		 WHERE id = $1 AND owner_id = $2 AND retired_at IS NULL FOR UPDATE`,
		vehicleID, ownerID).Scan(&mine)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoVehicle
	}
	if err != nil {
		return fmt.Errorf("drivers: loading the car: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vehicles SET is_default = false, updated_at = now()
		 WHERE owner_id = $1 AND is_default AND retired_at IS NULL`, ownerID); err != nil {
		return fmt.Errorf("drivers: clearing the old default: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vehicles SET is_default = true, updated_at = now() WHERE id = $1`, vehicleID); err != nil {
		return fmt.Errorf("drivers: setting the default: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("drivers: commit: %w", err)
	}
	return nil
}

// RetireVehicle takes a car out of the garage without erasing it.
//
// Refused while a route that has not been retired still names it: a template
// with no car cannot be priced, and the driver would find that out when
// somebody tried to book. Retiring the route first is the order.
func (s *Service) RetireVehicle(ctx context.Context, ownerID, vehicleID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("drivers: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var wasDefault bool
	err = tx.QueryRow(ctx, `
		SELECT is_default FROM vehicles
		 WHERE id = $1 AND owner_id = $2 AND retired_at IS NULL FOR UPDATE`,
		vehicleID, ownerID).Scan(&wasDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoVehicle
	}
	if err != nil {
		return fmt.Errorf("drivers: loading the car: %w", err)
	}

	var inUse bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM route_templates
			 WHERE vehicle_id = $1 AND active)`, vehicleID).Scan(&inUse); err != nil {
		return fmt.Errorf("drivers: checking what uses the car: %w", err)
	}
	if inUse {
		return ErrVehicleInUse
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vehicles SET retired_at = $2, is_default = false, updated_at = now()
		 WHERE id = $1`, vehicleID, s.clock.Now()); err != nil {
		return fmt.Errorf("drivers: retiring the car: %w", err)
	}

	// A driver with cars left must have a default, or publishing has nothing to
	// fall back on and fails at the worst moment.
	if wasDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE vehicles SET is_default = true, updated_at = now()
			 WHERE id = (
				SELECT id FROM vehicles
				 WHERE owner_id = $1 AND retired_at IS NULL
				 ORDER BY created_at LIMIT 1)`, ownerID); err != nil {
			return fmt.Errorf("drivers: promoting another car: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("drivers: commit: %w", err)
	}
	return nil
}

// assess validates a submitted car and works out what it drinks.
//
// It returns the car back, trimmed. Without that the caller stores whatever
// whitespace arrived — " Toyota " goes in the database and comes back on a
// passenger's screen — while the lookup that decided the consumption used the
// tidy version.
func (s *Service) assess(car Car) (vehicles.Assessment, vehicles.Fuel, Car, error) {
	car.Make = strings.TrimSpace(car.Make)
	car.Model = strings.TrimSpace(car.Model)
	car.Plate = strings.TrimSpace(car.Plate)

	if car.Make == "" || car.Model == "" {
		return vehicles.Assessment{}, "", Car{}, fmt.Errorf("%w: a make and a model are needed", ErrInvalid)
	}
	if car.Plate == "" {
		return vehicles.Assessment{}, "", Car{}, fmt.Errorf("%w: a plate is needed so a passenger can find the car", ErrInvalid)
	}

	fuel := vehicles.Fuel(car.Fuel)
	known := false
	for _, f := range vehicles.Fuels() {
		known = known || f == fuel
	}
	if !known {
		return vehicles.Assessment{}, "", Car{}, fmt.Errorf("%w: %q is not a fuel this app can price", ErrInvalid, car.Fuel)
	}

	assessed, err := vehicles.Assess(vehicles.Car{
		Make: car.Make, Model: car.Model, Fuel: fuel,
		EngineCC: car.EngineCC, CO2GramsPerKm: car.CO2GramsPerKm,
	})
	if errors.Is(err, vehicles.ErrUnusable) {
		return vehicles.Assessment{}, "", Car{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if err != nil {
		return vehicles.Assessment{}, "", Car{}, fmt.Errorf("drivers: assessing the car: %w", err)
	}
	return assessed, fuel, car, nil
}

// atMost brings a figure down to what may be charged for.
func atMost(l float64, fuel vehicles.Fuel) (float64, bool) {
	ceiling := vehicles.PricingCeiling(fuel)
	if l > ceiling {
		return ceiling, true
	}
	return l, false
}

func scanVehicle(rows pgx.Rows) (Vehicle, error) {
	var v Vehicle
	var fuel, source string
	if err := rows.Scan(&v.ID, &v.Owner, &v.Make, &v.Model, &fuel, &v.EngineCC, &v.CO2GramsPerKm,
		&v.LPer100Km, &source, &v.Label, &v.Colour, &v.Plate, &v.IsDefault, &v.RetiredAt); err != nil {
		return Vehicle{}, fmt.Errorf("drivers: scanning a car: %w", err)
	}
	v.Fuel = fuel
	v.Source = vehicles.Source(source)
	// Reading back cannot distinguish a figure that was brought down to the
	// ceiling from a car that genuinely drinks exactly that much, so it makes
	// the weaker claim: at the ceiling means capped.
	v.Capped = v.LPer100Km >= vehicles.PricingCeiling(vehicles.Fuel(fuel))
	return v, nil
}

func nullableText(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
