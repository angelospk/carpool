package vehicles_test

import (
	"testing"

	"carpool/internal/vehicles"
)

// The figure the whole planning estimate rests on. If it is wrong, every
// estimate is wrong in the same direction and nothing else would show it.
func TestTheTypicalCarIsAPlausibleCar(t *testing.T) {
	got, err := vehicles.TypicalCar()
	if err != nil {
		t.Fatalf("TypicalCar: %v", err)
	}

	for _, fuel := range vehicles.Fuels() {
		l, ok := got.LPer100Km[fuel]
		if !ok {
			t.Errorf("%s has no typical figure", fuel)
			continue
		}
		// A modern car sold in Europe drinks between about three and a half and
		// about twelve. Anything outside that is a units error or a broken
		// median, not a car.
		if l < 3.5 || l > 12 {
			t.Errorf("%s typical = %v l/100km, which is not a car", fuel, l)
		}
	}

	if got.Version == "" {
		t.Error("no version; an estimate could not say what it was built from")
	}
}

// Weighted by registrations, not by rows. The dataset holds one row per
// make/model/fuel/band, so an unweighted median counts a model sold fifty times
// the same as a Yaris sold two hundred thousand times — and "typical" then
// means typical of the catalogue rather than of the road.
func TestTheMedianFollowsTheCarsThatWereActuallySold(t *testing.T) {
	got, err := vehicles.TypicalCar()
	if err != nil {
		t.Fatalf("TypicalCar: %v", err)
	}

	// Every fuel's share is a real fraction, and together they are the whole
	// dataset. A share of zero would mean a fuel that is quoted but weightless.
	var total float64
	for _, fuel := range vehicles.Fuels() {
		share := got.Share[fuel]
		if share <= 0 || share > 1 {
			t.Errorf("%s share = %v", fuel, share)
		}
		total += share
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("the shares add up to %v, want 1", total)
	}

	// Petrol is the commonest fuel in the data, so it must carry the most
	// weight. A median computed per row rather than per registration would not
	// reliably put it first.
	for _, fuel := range []vehicles.Fuel{vehicles.Diesel, vehicles.PetrolHybrid, vehicles.DieselHybrid} {
		if got.Share[vehicles.Petrol] <= got.Share[fuel] {
			t.Errorf("petrol (%v) does not outweigh %s (%v)",
				got.Share[vehicles.Petrol], fuel, got.Share[fuel])
		}
	}
}

// The two fuels are blended rather than quoted apart, because a passenger with
// no driver yet cannot reason about a car that does not exist.
func TestTheCostPerKilometreSitsBetweenTheTwoFuels(t *testing.T) {
	got, err := vehicles.TypicalCar()
	if err != nil {
		t.Fatalf("TypicalCar: %v", err)
	}

	blended, err := got.CostPerKmEUR(1.859, 1.729)
	if err != nil {
		t.Fatalf("CostPerKmEUR: %v", err)
	}

	// Bracketed by the cheapest and dearest single-fuel costs it is made of.
	var cheapest, dearest float64
	for _, fuel := range vehicles.Fuels() {
		price := 1.859
		if fuel == vehicles.Diesel || fuel == vehicles.DieselHybrid {
			price = 1.729
		}
		cost := got.LPer100Km[fuel] * price / 100
		if cheapest == 0 || cost < cheapest {
			cheapest = cost
		}
		if cost > dearest {
			dearest = cost
		}
	}
	if blended < cheapest || blended > dearest {
		t.Errorf("blended %v is outside the range %v..%v of the fuels it blends",
			blended, cheapest, dearest)
	}

	// And it is a sane number: roughly ten cents a kilometre at these prices.
	if blended < 0.05 || blended > 0.20 {
		t.Errorf("%v €/km is not what a car costs to run", blended)
	}
}

// A missing pump price is not a reason to quote a number built on the other
// one. Both, or nothing.
func TestNoPumpPriceMeansNoCostPerKilometre(t *testing.T) {
	got, err := vehicles.TypicalCar()
	if err != nil {
		t.Fatalf("TypicalCar: %v", err)
	}

	for _, tt := range []struct{ petrol, diesel float64 }{
		{0, 1.729}, {1.859, 0}, {0, 0}, {-1, 1.729},
	} {
		if _, err := got.CostPerKmEUR(tt.petrol, tt.diesel); err == nil {
			t.Errorf("petrol %v diesel %v produced a cost anyway", tt.petrol, tt.diesel)
		}
	}
}

// The estimate must not be built on the class floors, which under-state on
// purpose so that an unidentified car cannot charge as if it were identified.
// A known-low input makes a systematically low estimate.
func TestTheTypicalFigureIsNotTheDeliberatelyLowClassFloor(t *testing.T) {
	got, err := vehicles.TypicalCar()
	if err != nil {
		t.Fatalf("TypicalCar: %v", err)
	}

	for _, fuel := range vehicles.Fuels() {
		floor, err := vehicles.Assess(vehicles.Car{
			Make: "nothing", Model: "in the dataset", Fuel: fuel, EngineCC: 1600,
		})
		if err != nil {
			continue
		}
		if got.LPer100Km[fuel] == floor.LPer100Km {
			t.Errorf("%s typical (%v) is the class floor, which under-states on purpose",
				fuel, floor.LPer100Km)
		}
	}
}
