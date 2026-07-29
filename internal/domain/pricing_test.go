package domain

import "testing"

// baseline is a 10 km trip in a 7 l/100km car at 1.80 €/l with no wear markup
// and no other passengers:
//
//	cost    = 7/100 × 1.80 × 10 × 1.0 = 1.26 €
//	per_pax = 1.26 / (0 current + 1 new + 1 driver) = 0.63 €
//	rounded to the nearest 0.50 € = 0.50 €
func baseline() PricingInputs {
	return PricingInputs{
		TripKm:                   10,
		FuelConsumptionLPer100Km: 7.0,
		FuelPriceEUR:             1.80,
		WearFactor:               1.0,
		FreeBelowKm:              5.0,
		CurrentPassengers:        0,
		Mode:                     PricingFormula,
	}
}

func TestSuggestedPriceAppliesTheFuelFormula(t *testing.T) {
	if got := SuggestedPriceCents(baseline()); got != 50 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 50", got)
	}
}

func TestSuggestedPriceSplitsAcrossExistingPassengersAndTheDriver(t *testing.T) {
	in := baseline()
	in.TripKm = 100 // cost = 7/100 × 1.80 × 100 = 12.60 €
	in.CurrentPassengers = 1

	// 12.60 / (1 existing + 1 new + 1 driver) = 4.20 € -> nearest 0.50 = 4.00 €
	if got := SuggestedPriceCents(in); got != 400 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 400", got)
	}
}

func TestSuggestedPriceAppliesTheWearFactor(t *testing.T) {
	in := baseline()
	in.TripKm = 100
	in.WearFactor = 1.5 // cost = 12.60 × 1.5 = 18.90; /2 = 9.45 -> 9.50

	if got := SuggestedPriceCents(in); got != 950 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 950", got)
	}
}

func TestSuggestedPriceIsZeroAtTheFreeBelowThreshold(t *testing.T) {
	in := baseline()
	in.TripKm = 5.0 // the spec's rule is "≤ free_below_km", so the boundary is free

	if got := SuggestedPriceCents(in); got != 0 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 0 at the free-below boundary", got)
	}
}

func TestSuggestedPriceIsChargedJustAboveTheFreeBelowThreshold(t *testing.T) {
	in := baseline()
	in.TripKm = 5.01

	if got := SuggestedPriceCents(in); got == 0 {
		t.Fatal("a trip just above the free-below threshold should not be free")
	}
}

func TestSuggestedPriceIsZeroForNonFormulaPricingModes(t *testing.T) {
	for _, mode := range []PricingMode{PricingFree, PricingFreeContribution} {
		in := baseline()
		in.TripKm = 100
		in.Mode = mode

		if got := SuggestedPriceCents(in); got != 0 {
			t.Errorf("mode %q: SuggestedPriceCents = %d cents, want 0", mode, got)
		}
	}
}

func TestSuggestedPriceRoundsToTheNearestHalfEuro(t *testing.T) {
	tests := []struct {
		name   string
		tripKm float64
		want   int
	}{
		// cost = 0.126 €/km; per_pax = cost/2 = 0.063 €/km.
		// Exact half-step inputs are deliberately not asserted: they land on a
		// binary-float midpoint, so which way they tip is not a business rule.
		{"rounds up to the nearest step", 20, 150},     // 1.260 -> 1.50
		{"rounds down to the nearest step", 16, 100},   // 1.008 -> 1.00
		{"lands exactly on a step", 15.873015873, 100}, // 1.000 -> 1.00
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseline()
			in.FreeBelowKm = 0 // exercise rounding, not the free-below rule
			in.TripKm = tt.tripKm

			got := SuggestedPriceCents(in)

			if got%50 != 0 {
				t.Fatalf("SuggestedPriceCents = %d cents, which is not a multiple of 50", got)
			}
			if got != tt.want {
				t.Fatalf("SuggestedPriceCents = %d cents, want %d", got, tt.want)
			}
		})
	}
}

func TestSuggestedPriceIsNeverNegative(t *testing.T) {
	in := baseline()
	in.TripKm = 0

	if got := SuggestedPriceCents(in); got < 0 {
		t.Fatalf("SuggestedPriceCents = %d cents, want a non-negative value", got)
	}
}
