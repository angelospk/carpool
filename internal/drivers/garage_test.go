package drivers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/drivers"
	"carpool/internal/vehicles"
)

func aCar(plate string) drivers.Car {
	return drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol",
		EngineCC: 1329, Plate: plate, Colour: "λευκό",
	}
}

// The reason for the whole table. A driver with a van at the weekend and a
// small diesel for the week had to edit one profile before every trip, and
// every trip already published re-priced when they did.
func TestADriverCanKeepMoreThanOneCar(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	first, err := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	if err != nil {
		t.Fatalf("first car: %v", err)
	}
	second, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2287, Plate: "ΑΒΓ-2222",
	})
	if err != nil {
		t.Fatalf("second car: %v", err)
	}

	live, err := f.svc.Vehicles(ctx, f.driver)
	if err != nil {
		t.Fatalf("Vehicles: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("%d cars, want 2", len(live))
	}

	// The first one added is the default, because a driver with one car should
	// never have to choose one.
	if !first.IsDefault {
		t.Error("the first car was not made the default")
	}
	if second.IsDefault {
		t.Error("adding a second car silently changed which one is the default")
	}
}

// Consumption is derived from the car, never typed, and each car gets its own
// figure. That is the whole reason the fuel is on the vehicle rather than the
// person.
func TestEachCarIsAssessedOnItsOwnTerms(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	petrol, err := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	if err != nil {
		t.Fatalf("petrol: %v", err)
	}
	diesel, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2287, Plate: "ΑΒΓ-2222",
	})
	if err != nil {
		t.Fatalf("diesel: %v", err)
	}

	if petrol.LPer100Km == diesel.LPer100Km {
		t.Errorf("a Yaris and a Ducato were both assessed at %v", petrol.LPer100Km)
	}
	if petrol.LPer100Km <= 0 || diesel.LPer100Km <= 0 {
		t.Fatal("a car was stored with no consumption at all")
	}
}

// The one lever left. Consumption cannot be typed, but a thirstier car can be
// claimed, and the fuel share moves with it — nearly threefold between a hybrid
// and a V8. So there is a ceiling on what may be charged for.
func TestWhatMayBeChargedForIsCapped(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	// A CO2 figure high enough to derive a consumption above any plausible
	// private car: 400 g/km of petrol is about 17 l/100 km.
	thirsty, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Nothing", Model: "In The Dataset", Fuel: "petrol",
		EngineCC: 6200, CO2GramsPerKm: 400, Plate: "ΑΒΓ-3333",
	})
	if err != nil {
		t.Fatalf("AddVehicle: %v", err)
	}

	if ceiling := vehicles.PricingCeiling(vehicles.Petrol); thirsty.LPer100Km > ceiling {
		t.Errorf("stored %v l/100km, above the %v ceiling", thirsty.LPer100Km, ceiling)
	}
	if !thirsty.Capped {
		t.Error("a capped assessment did not say it had been capped")
	}
}

// A garage is not a fleet. Five is more cars than anybody sharing a commute
// has, and an unbounded list is an invitation to keep one of everything and
// pick whichever prices best.
func TestAGarageIsBounded(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	for i := range drivers.MaxVehicles {
		if _, err := f.svc.AddVehicle(ctx, f.driver, aCar(fmt.Sprintf("ΑΒΓ-%04d", i))); err != nil {
			t.Fatalf("car %d: %v", i, err)
		}
	}

	_, err := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-9999"))
	if !errors.Is(err, drivers.ErrTooManyVehicles) {
		t.Fatalf("the %dth car gave %v, want ErrTooManyVehicles", drivers.MaxVehicles+1, err)
	}
}

// Retiring, not deleting: a route may still point at it, and somebody asking
// what they rode in last March deserves an answer.
func TestRetiringACarLeavesItReadableAndTakesItOutOfTheList(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	keep, _ := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	sell, _ := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2287, Plate: "ΑΒΓ-2222",
	})

	if err := f.svc.RetireVehicle(ctx, f.driver, sell.ID); err != nil {
		t.Fatalf("RetireVehicle: %v", err)
	}

	live, _ := f.svc.Vehicles(ctx, f.driver)
	if len(live) != 1 || live[0].ID != keep.ID {
		t.Errorf("the list after retiring holds %d cars", len(live))
	}

	var exists bool
	if err := f.pool.QueryRow(ctx,
		`SELECT retired_at IS NOT NULL FROM vehicles WHERE id = $1`, sell.ID).Scan(&exists); err != nil {
		t.Fatalf("the retired car is gone from the table entirely: %v", err)
	}
	if !exists {
		t.Error("the car was not marked retired")
	}
}

// A driver must not be left without a default: publishing would then have
// nothing to fall back on, and the failure would surface at the worst moment.
func TestRetiringTheDefaultPromotesAnother(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	first, _ := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	second, _ := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2287, Plate: "ΑΒΓ-2222",
	})

	if err := f.svc.RetireVehicle(ctx, f.driver, first.ID); err != nil {
		t.Fatalf("RetireVehicle: %v", err)
	}

	live, _ := f.svc.Vehicles(ctx, f.driver)
	if len(live) != 1 {
		t.Fatalf("%d cars left", len(live))
	}
	if !live[0].IsDefault || live[0].ID != second.ID {
		t.Error("retiring the default left the driver without one")
	}
}

// The last car may not be retired while routes still point at it: a template
// with no car cannot be priced, and the driver would find out when somebody
// tried to book.
func TestTheLastCarCannotBeRetiredOutFromUnderALiveRoute(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	only, _ := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	f.publishRoute(only.ID)

	err := f.svc.RetireVehicle(ctx, f.driver, only.ID)
	if !errors.Is(err, drivers.ErrVehicleInUse) {
		t.Fatalf("retiring the only car of a live route gave %v, want ErrVehicleInUse", err)
	}
}

func TestSettingTheDefaultMovesItAndLeavesExactlyOne(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	first, _ := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))
	second, _ := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2287, Plate: "ΑΒΓ-2222",
	})

	if err := f.svc.SetDefaultVehicle(ctx, f.driver, second.ID); err != nil {
		t.Fatalf("SetDefaultVehicle: %v", err)
	}

	live, _ := f.svc.Vehicles(ctx, f.driver)
	var defaults int
	for _, v := range live {
		if v.IsDefault {
			defaults++
			if v.ID != second.ID {
				t.Errorf("the default is %v, want %v", v.ID, second.ID)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("%d defaults, want exactly 1", defaults)
	}
	_ = first
}

// Somebody else's car is not yours to retire, rename or drive.
func TestAStrangersCarIsNotYours(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	mine, _ := f.svc.AddVehicle(ctx, f.driver, aCar("ΑΒΓ-1111"))

	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"retire", func() error { return f.svc.RetireVehicle(ctx, f.stranger, mine.ID) }},
		{"make default", func() error { return f.svc.SetDefaultVehicle(ctx, f.stranger, mine.ID) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, drivers.ErrNoVehicle) {
				t.Errorf("gave %v, want ErrNoVehicle", err)
			}
		})
	}

	live, _ := f.svc.Vehicles(ctx, f.driver)
	if len(live) != 1 || live[0].RetiredAt != nil {
		t.Error("a stranger changed somebody else's garage")
	}
}

func TestACarNeedsAPlateAndSomethingToLookUp(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		car  drivers.Car
	}{
		{"no plate", drivers.Car{Make: "Toyota", Model: "Yaris", Fuel: "petrol", EngineCC: 1329}},
		{"no make", drivers.Car{Model: "Yaris", Fuel: "petrol", EngineCC: 1329, Plate: "ΑΒΓ-1"}},
		{"unpriceable fuel", drivers.Car{Make: "Tesla", Model: "3", Fuel: "electric", Plate: "ΑΒΓ-1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.svc.AddVehicle(ctx, f.driver, tt.car); !errors.Is(err, drivers.ErrInvalid) {
				t.Errorf("gave %v, want ErrInvalid", err)
			}
		})
	}
}

var _ = uuid.Nil
