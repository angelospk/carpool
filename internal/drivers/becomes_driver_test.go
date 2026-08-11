package drivers_test

import (
	"context"
	"testing"

	"carpool/internal/drivers"
)

// A person who has told us what they drive can publish a route.
//
// The garage writes to vehicles. Publishing checks driver_profiles. Nothing
// wrote the second, and the app has no screen that could: it calls addVehicle
// and nothing else, so a driver added their car, pressed publish, and was told
// "add your car details before publishing a route" — which is what they had
// just spent a screen doing, with nowhere left to go.
//
// PUT /me/driver would have satisfied it, and is the older single-car path the
// app stopped using when the garage arrived. So the fix belongs where the car
// is declared rather than in a second screen asking for the same details again.
func TestTheFirstCarMakesSomebodyADriver(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	// The fixture seeds a profile for its own purposes; this is about somebody
	// who has never been a driver.
	if _, err := f.pool.Exec(ctx, `DELETE FROM driver_profiles`); err != nil {
		t.Fatalf("clearing the profile: %v", err)
	}

	if _, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol", EngineCC: 1330,
		Colour: "ασημί", Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("AddVehicle: %v", err)
	}

	var profiles int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM driver_profiles WHERE user_id = $1`, f.driver).Scan(&profiles); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if profiles != 1 {
		t.Fatalf("%d driver profiles after declaring a car, want 1: without it they cannot publish", profiles)
	}
}

// The profile follows the first car and then stays put. A second car is a
// second car, not a change of mind about the first, and the trips already
// published name the vehicle they were published with.
func TestASecondCarDoesNotRewriteTheProfile(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx, `DELETE FROM driver_profiles`); err != nil {
		t.Fatalf("clearing the profile: %v", err)
	}
	if _, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol", EngineCC: 1330, Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("first AddVehicle: %v", err)
	}

	if _, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Fiat", Model: "Ducato", Fuel: "diesel", EngineCC: 2300, Plate: "ΑΒΓ-9999",
	}); err != nil {
		t.Fatalf("second AddVehicle: %v", err)
	}

	var plate string
	if err := f.pool.QueryRow(ctx,
		`SELECT car_plate FROM driver_profiles WHERE user_id = $1`, f.driver).Scan(&plate); err != nil {
		t.Fatalf("reading the profile: %v", err)
	}
	if plate != "ΑΒΓ-1234" {
		t.Fatalf("the profile now reads %q, want the first car: a second car is not a correction to the first", plate)
	}
}

// Somebody who already had a profile keeps it exactly as it was, including the
// detour budget and notify radius they chose.
func TestDeclaringACarLeavesAnExistingProfileAlone(t *testing.T) {
	f := newGarageFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx,
		`UPDATE driver_profiles SET detour_budget_min = 25 WHERE user_id = $1`, f.driver); err != nil {
		t.Fatalf("setting a budget: %v", err)
	}

	if _, err := f.svc.AddVehicle(ctx, f.driver, drivers.Car{
		Make: "Toyota", Model: "Yaris", Fuel: "petrol", EngineCC: 1330, Plate: "ΑΒΓ-1234",
	}); err != nil {
		t.Fatalf("AddVehicle: %v", err)
	}

	var budget int
	if err := f.pool.QueryRow(ctx,
		`SELECT detour_budget_min FROM driver_profiles WHERE user_id = $1`, f.driver).Scan(&budget); err != nil {
		t.Fatalf("reading the budget: %v", err)
	}
	if budget != 25 {
		t.Fatalf("detour budget is %d, want the 25 they chose", budget)
	}
}
