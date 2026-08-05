package domain_test

import (
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
