package domain_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"carpool/internal/domain"
)

func estimateInputs() domain.EstimateInputs {
	return domain.EstimateInputs{
		RouteKm:       100,
		RouteMin:      80,
		CostPerKmEUR:  0.0985,
		Divisor:       4,
		Version:       "eea-2026-08",
		FuelPriceAsOf: "2026-08-05T10:00:00Z",
		RegionName:    "Περιφερειακή Ενότητα Έβρου",
	}
}

// 100 km at 9,85 cents a kilometre is 9,85 €; over four — three seats and the
// driver — that is 2,46, floored to the half euro at 2,00.
func TestTheEstimateIsDividedAndFlooredTheSameWayThePriceIs(t *testing.T) {
	got := domain.Estimate(estimateInputs())

	if got.ContributionCents != 200 {
		t.Errorf("estimate = %d cents, want 200", got.ContributionCents)
	}
	if got.RouteKm != 100 || got.RouteMin != 80 {
		t.Errorf("the route did not survive: %v km, %v min", got.RouteKm, got.RouteMin)
	}
}

// Under is tolerable; over is the app quoting somebody a number they will not
// be asked for. Checked across a spread rather than on one example, because a
// rounding that goes up would only show at particular distances.
func TestTheEstimateNeverExceedsWhatTheRealFormulaWouldAsk(t *testing.T) {
	for _, km := range []float64{3, 12, 47, 88, 100, 153, 291, 640} {
		in := estimateInputs()
		in.RouteKm = km
		estimate := domain.Estimate(in)

		// The same trip through the real formula, with a car that happens to
		// drink exactly what the typical figure assumes.
		cents, _ := domain.SuggestedPrice(domain.PricingInputs{
			TripKm:                   km,
			FuelConsumptionLPer100Km: in.CostPerKmEUR * 100, // 1 €/litre, so l/100km == cents/km
			FuelPriceEUR:             1.0,
			WearFactor:               1.0,
			OfferedSeats:             in.Divisor - 1,
			Mode:                     domain.PricingFormula,
		})

		if estimate.ContributionCents > cents {
			t.Errorf("%.0f km: estimated %d but the formula asks %d",
				km, estimate.ContributionCents, cents)
		}
	}
}

// A guess must not be able to become what somebody agreed to pay. It is a
// different type with its own discriminator, so nothing that takes a Basis will
// take it, and nothing reading the JSON can mistake one for the other.
func TestAnEstimateIsNotAReceipt(t *testing.T) {
	got := domain.Estimate(estimateInputs())

	if got.Kind != domain.EstimateKind {
		t.Errorf("kind = %q, want %q", got.Kind, domain.EstimateKind)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var asBasis domain.Basis
	if err := json.Unmarshal(raw, &asBasis); err == nil && asBasis.SeatPriceCents != 0 {
		t.Error("an estimate decoded into a Basis carrying a seat price")
	}
	if asBasis.Version != 0 {
		t.Error("an estimate decoded into a Basis with a formula version")
	}
}

// The figure is worth what its inputs are worth, so it carries them: which
// dataset, what it divided by, and how old the pump price was.
func TestTheEstimateSaysWhatItWasBuiltFrom(t *testing.T) {
	got := domain.Estimate(estimateInputs())

	if got.Assumptions.Version == "" {
		t.Error("no dataset version; the figure cannot be explained later")
	}
	if got.Assumptions.Divisor != 4 {
		t.Errorf("divisor = %d", got.Assumptions.Divisor)
	}
	if got.Assumptions.FuelPriceAsOf == "" {
		t.Error("no fuel price date; a six-month-old figure looks like this week's")
	}
	if got.Assumptions.RegionName == "" {
		t.Error("no region; the price came from somewhere and should say where")
	}
}

// Rather than a fabricated number. A trip that could not be routed, or a
// deployment with no fuel prices, has no estimate — and nothing is worse here
// than a confident figure built on nothing.
func TestNothingToGoOnMeansNoFigure(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   domain.EstimateInputs
	}{
		{"no route", domain.EstimateInputs{CostPerKmEUR: 0.1, Divisor: 4}},
		{"no fuel cost", domain.EstimateInputs{RouteKm: 100, Divisor: 4}},
		{"no divisor", domain.EstimateInputs{RouteKm: 100, CostPerKmEUR: 0.1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.Estimate(tt.in); got.ContributionCents != 0 {
				t.Errorf("estimated %d cents out of nothing", got.ContributionCents)
			}
		})
	}
}

// A hop too short to be worth anything estimates at nothing, not at the
// rounding step. Flooring means rounding down to a step, never imposing a
// minimum.
func TestAShortHopEstimatesAtNothingRatherThanAtTheStep(t *testing.T) {
	in := estimateInputs()
	in.RouteKm = 4 // about 10 cents a seat

	if got := domain.Estimate(in); got.ContributionCents != 0 {
		t.Errorf("a 4 km trip estimated at %d cents", got.ContributionCents)
	}
}

// The honest failure of a single figure is upward. Somebody quoted 12 € and
// then asked for 25 € has been misled, even though both numbers came honestly
// from different cars — so the estimate says how far up it might go.
func TestTheEstimateSaysHowFarUpItCouldGo(t *testing.T) {
	in := estimateInputs()
	in.CeilingCostPerKmEUR = 0.2045 // the thirstiest car the app will price

	got := domain.Estimate(in)

	if got.CeilingCents <= got.ContributionCents {
		t.Errorf("ceiling %d is not above the typical %d", got.CeilingCents, got.ContributionCents)
	}
	// 100 km at 20,45 cents is 20,45 €, over four is 5,11, floored to 5,00.
	if got.CeilingCents != 500 {
		t.Errorf("ceiling = %d cents, want 500", got.CeilingCents)
	}
	// Floored the same way, so the two figures are made alike.
	if got.CeilingCents%50 != 0 {
		t.Errorf("ceiling %d is not a whole half euro", got.CeilingCents)
	}
}

// There is deliberately no matching floor. The thriftiest real car is five
// times cheaper than the ceiling, and a range that wide reads as "nobody knows"
// and gets ignored — while the upper bound is the one that prevents a surprise.
func TestThereIsNoLowerBoundToIgnore(t *testing.T) {
	in := estimateInputs()
	in.CeilingCostPerKmEUR = 0.2045

	raw, err := json.Marshal(domain.Estimate(in))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, unwanted := range []string{"floor_cents", "low_cents", "contribution_low"} {
		if bytes.Contains(raw, []byte(unwanted)) {
			t.Errorf("the estimate carries %q", unwanted)
		}
	}
}

// A ceiling that is not above the typical figure is not a ceiling, and showing
// "about 4 €, up to 4 €" would be noise.
func TestNoCeilingIsShownWhenThereIsNothingAboveTheEstimate(t *testing.T) {
	in := estimateInputs()
	in.CeilingCostPerKmEUR = in.CostPerKmEUR // no headroom at all

	if got := domain.Estimate(in); got.CeilingCents != 0 {
		t.Errorf("ceiling = %d with no headroom above the estimate", got.CeilingCents)
	}
}

// "Περίπου 2 €, έως 2 €" is noise, and the guard against it cannot live on the
// cost per kilometre alone. A car that drinks a little more than typical still
// floors to the same half euro, and the ceiling then repeats the figure it was
// supposed to be an upper bound on.
func TestNoCeilingIsShownWhenItFloorsToTheSameFigure(t *testing.T) {
	in := estimateInputs()
	// 100 km, over four: 9,85 € becomes 2,46 → 2,00, and 9,95 € becomes 2,49 →
	// also 2,00. Dearer per kilometre, identical once floored.
	in.CeilingCostPerKmEUR = 0.0995

	got := domain.Estimate(in)
	if got.CeilingCents != 0 {
		t.Errorf("ceiling = %d cents beside a contribution of %d",
			got.CeilingCents, got.ContributionCents)
	}
}

// QA U F4: the search showed 1,00 € for the fuel while the real ask was 2,38 €,
// because the barriers the passenger rides through were not in the figure.
// The toll is added after the fuel is floored, exactly as SuggestedPrice adds
// it, and is not itself floored: a toll is money handed over, not a step.
func TestTheEstimateIncludesTheTollShare(t *testing.T) {
	in := estimateInputs()
	in.CeilingCostPerKmEUR = 0.2045
	fuelOnly := domain.Estimate(in)

	in.TollShareCents = 138
	got := domain.Estimate(in)

	if got.TollCents != 138 {
		t.Errorf("toll = %d, want 138", got.TollCents)
	}
	if got.FuelCents != fuelOnly.ContributionCents {
		t.Errorf("fuel = %d, want %d", got.FuelCents, fuelOnly.ContributionCents)
	}
	if got.ContributionCents != fuelOnly.ContributionCents+138 {
		t.Errorf("contribution = %d, want fuel %d + 138", got.ContributionCents, fuelOnly.ContributionCents)
	}
	if got.CeilingCents != fuelOnly.CeilingCents+138 {
		t.Errorf("ceiling = %d, want %d: the toll is the same in a thirstier car",
			got.CeilingCents, fuelOnly.CeilingCents+138)
	}
}

func TestWithoutTollsTheEstimateIsFuelOnly(t *testing.T) {
	got := domain.Estimate(estimateInputs())
	if got.TollCents != 0 {
		t.Errorf("toll = %d with no barriers", got.TollCents)
	}
	if got.ContributionCents != got.FuelCents {
		t.Errorf("contribution %d != fuel %d", got.ContributionCents, got.FuelCents)
	}
}

// No ceiling is shown when the fuel has no headroom, tolls or not.
func TestATollAloneDoesNotInventACeiling(t *testing.T) {
	in := estimateInputs()
	in.CeilingCostPerKmEUR = in.CostPerKmEUR
	in.TollShareCents = 138
	if got := domain.Estimate(in); got.CeilingCents != 0 {
		t.Errorf("ceiling = %d with no fuel headroom", got.CeilingCents)
	}
}
