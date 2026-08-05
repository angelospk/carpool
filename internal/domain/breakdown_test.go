package domain_test

import (
	"testing"

	"carpool/internal/domain"
)

func inputs() domain.PricingInputs {
	return domain.PricingInputs{
		TripKm:                   100,
		FuelConsumptionLPer100Km: 6.0,
		FuelPriceEUR:             1.800,
		WearFactor:               1.0,
		FreeBelowKm:              5,
		OfferedSeats:             3,
		Mode:                     domain.PricingFormula,
		// A barrier on the way, because the toll is added after the rounding and
		// outside the waiver — with a zero here the test cannot tell the parts
		// apart at all.
		TollShareCents:    140,
		Fuel:              "diesel",
		RegionName:        "Περιφερειακή Ενότητα Έβρου",
		ConsumptionSource: "dataset",
	}
}

// The breakdown is derived from the frozen receipt, never recomputed from
// today's inputs. A client that did its own arithmetic would disagree with the
// price beside it the day a fuel figure moved.
func TestTheBreakdownAddsUpToThePriceThatWasCharged(t *testing.T) {
	cents, basis := domain.SuggestedPrice(inputs())
	b := domain.BreakdownOf(basis)

	if b.TotalCents != cents {
		t.Errorf("breakdown total %d, price charged %d", b.TotalCents, cents)
	}
	if b.RoundedCents+b.TollShareCents != b.TotalCents {
		t.Errorf("the parts (%d + %d) do not make the whole (%d)",
			b.RoundedCents, b.TollShareCents, b.TotalCents)
	}
}

// 6 l/100km × 1.80 € × 100 km = 10.80 €, over four (three seats and the
// driver) = 2.70, floored to the half euro = 2.50.
func TestTheBreakdownShowsEveryStepOfTheArithmetic(t *testing.T) {
	_, basis := domain.SuggestedPrice(inputs())
	b := domain.BreakdownOf(basis)

	if b.FuelCostCents != 1080 {
		t.Errorf("fuel cost = %d cents, want 1080", b.FuelCostCents)
	}
	if b.Divisor != 4 {
		t.Errorf("divisor = %d, want 4 — three seats and the driver", b.Divisor)
	}
	if b.ShareCents != 270 {
		t.Errorf("share before rounding = %d, want 270", b.ShareCents)
	}
	if b.RoundedCents != 250 {
		t.Errorf("rounded = %d, want 250 — floored, never up", b.RoundedCents)
	}
}

// Rounding is always down. Somebody checking the arithmetic must be able to see
// that the app rounded in their favour, not the driver's.
func TestTheRoundingIsAlwaysDownwards(t *testing.T) {
	for _, km := range []float64{40, 55, 73, 99, 137, 288} {
		in := inputs()
		in.TripKm = km
		_, basis := domain.SuggestedPrice(in)
		b := domain.BreakdownOf(basis)

		if b.RoundedCents > b.ShareCents {
			t.Errorf("%.0f km: rounded %d above the share %d", km, b.RoundedCents, b.ShareCents)
		}
		if b.ShareCents-b.RoundedCents >= 50 {
			t.Errorf("%.0f km: rounded %d is more than a step below %d", km, b.RoundedCents, b.ShareCents)
		}
	}
}

// The invariant the whole formula exists for: every seat sold, added up, comes
// to less than the trip cost. A driver who fills the car does not come out
// ahead — which is what άρθρο 18 ν. 1903/1990 is interested in.
func TestEverySeatSoldStillComesToLessThanTheTripCost(t *testing.T) {
	for seats := 1; seats <= 8; seats++ {
		for _, km := range []float64{10, 45, 120, 400} {
			in := inputs()
			in.OfferedSeats = seats
			in.TripKm = km

			cents, basis := domain.SuggestedPrice(in)
			b := domain.BreakdownOf(basis)

			// Every offered seat sold at this price, counting only the fuel
			// part: a barrier is cash the driver handed over, and recovering a
			// share of it from each person aboard is not coming out ahead.
			recovered := (cents - b.TollShareCents) * seats
			if recovered >= b.FuelCostCents && b.FuelCostCents > 0 {
				t.Errorf("%d seats over %.0f km: %d cents recovered against a %d cent trip",
					seats, km, recovered, b.FuelCostCents)
			}
		}
	}
}

// A trip under the driver's own threshold costs nothing, and the breakdown has
// to say why rather than showing a zero somebody reads as a bug.
func TestAWaivedTripSaysItWasWaived(t *testing.T) {
	in := inputs()
	in.TripKm = 3 // under free_below_km
	in.TollShareCents = 0

	cents, basis := domain.SuggestedPrice(in)
	b := domain.BreakdownOf(basis)

	if cents != 0 {
		t.Fatalf("a 3 km trip priced at %d cents", cents)
	}
	if !b.Waived {
		t.Error("a waived trip does not say it was waived")
	}
	if b.TotalCents != 0 {
		t.Errorf("total = %d, want 0", b.TotalCents)
	}
}

// The waiver is over the fuel, not over the barriers. A driver willing not to
// charge for six kilometres of their own petrol has still handed over 1,40 € at
// a booth, and asking for a share of that is not a fare.
func TestAWaivedTripStillSharesTheTollsItPassed(t *testing.T) {
	in := inputs()
	in.TripKm = 3

	cents, basis := domain.SuggestedPrice(in)
	b := domain.BreakdownOf(basis)

	if cents != in.TollShareCents {
		t.Errorf("a waived trip with a barrier priced at %d, want the %d toll share",
			cents, in.TollShareCents)
	}
	if b.RoundedCents != 0 {
		t.Errorf("fuel share = %d on a waived trip, want 0", b.RoundedCents)
	}
}

// A driver asking for nothing is not the same as a trip too short to charge
// for, and the two must not be shown with the same sentence.
func TestADriverAskingForNothingIsNotAWaivedTrip(t *testing.T) {
	in := inputs()
	in.Mode = domain.PricingFree
	// And short enough that the waiver would apply if the mode were not
	// checked, which is the whole distinction.
	in.TripKm = 3

	_, basis := domain.SuggestedPrice(in)
	b := domain.BreakdownOf(basis)

	if b.Waived {
		t.Error("a free ride was described as a trip too short to charge for")
	}
	if b.Mode != string(domain.PricingFree) {
		t.Errorf("mode = %q", b.Mode)
	}

	// And it charges nothing at all, barriers included. The receipt still
	// records the toll share the formula worked out before the mode discarded
	// it; showing that as charged would put a total on screen that nobody is
	// paying.
	if b.TollShareCents != 0 || b.TotalCents != 0 {
		t.Errorf("a free ride shows %d cents of toll and a %d cent total",
			b.TollShareCents, b.TotalCents)
	}
}

// A receipt written by an older version of the formula must not be replayed
// through today's arithmetic and disagree with the price sitting next to it.
func TestAnOlderReceiptIsShownAsWhatItSaysRatherThanRecomputed(t *testing.T) {
	old := domain.Basis{
		Version:              1,
		ConsumptionLPer100Km: 7.0,
		FuelPriceEUR:         1.700,
		TripKm:               100,
		WearFactor:           1.0,
		OfferedSeats:         3,
		CostEUR:              11.90,
		SeatPriceCents:       250,
		Mode:                 string(domain.PricingFormula),
	}

	b := domain.BreakdownOf(old)

	if b.TotalCents != 250 {
		t.Errorf("total = %d, want the 250 the receipt records", b.TotalCents)
	}
	if b.FuelCostCents != 1190 {
		t.Errorf("fuel cost = %d, want the 1190 the receipt records", b.FuelCostCents)
	}
	if b.BasisVersion != 1 {
		t.Errorf("basis version = %d; a reader cannot tell which formula this was", b.BasisVersion)
	}
}

// The share before rounding is divided out of the unrounded cost, the way
// SuggestedPrice divides it — not out of FuelCostCents, which has already been
// rounded to the nearest cent.
//
// 6 l/100km × 1.85 €/l × 90 km is 9.99 €, and one seat plus the driver is a
// divisor of two. Dividing 999 cents by two truncates to 499; halving 9.99 €
// and rounding gives 500. A line that says 4,99 above a total of 5,00 is the
// kind of arithmetic somebody checks once and then stops trusting.
func TestTheShareIsDividedOutOfTheUnroundedCost(t *testing.T) {
	in := inputs()
	in.FuelConsumptionLPer100Km = 6.0
	in.FuelPriceEUR = 1.85
	in.TripKm = 90
	in.OfferedSeats = 1
	in.TollShareCents = 0

	_, basis := domain.SuggestedPrice(in)
	b := domain.BreakdownOf(basis)

	if basis.CostEUR != 9.99 {
		t.Fatalf("cost = %v, want 9.99 — the case this test is about", basis.CostEUR)
	}
	if b.ShareCents != 500 {
		t.Errorf("share = %d, want 500; %d is what dividing the rounded %d cents gives",
			b.ShareCents, b.FuelCostCents/b.Divisor, b.FuelCostCents)
	}
}
