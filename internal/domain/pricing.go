package domain

import "math"

// priceStepCents is the rounding granularity of the suggested contribution:
// 0.50 €. Prices are computed and stored in integer cents so that repeated
// arithmetic cannot accumulate binary-float error.
const priceStepCents = 50

// PricingMode is the driver's cost-sharing preference for a route.
type PricingMode string

const (
	// PricingFormula suggests a contribution from the fuel formula.
	PricingFormula PricingMode = "formula"
	// PricingFreeContribution is pay-what-you-want: no figure is suggested.
	PricingFreeContribution PricingMode = "free_contribution"
	// PricingFree means the driver asks for nothing.
	PricingFree PricingMode = "free"
)

// PricingInputs are the values the suggested-contribution formula needs.
type PricingInputs struct {
	TripKm                   float64
	FuelConsumptionLPer100Km float64
	FuelPriceEUR             float64
	WearFactor               float64
	FreeBelowKm              float64

	// CurrentPassengers counts passengers already booked, excluding the one
	// being priced.
	CurrentPassengers int
	Mode              PricingMode

	// TollShareCents is this passenger's share of the motorway barriers they
	// will actually be in the car for, already divided among the people aboard
	// at each one — see internal/tolls. Zero when the route passes none.
	TollShareCents int
}

// SuggestedPriceCents computes the suggested per-passenger contribution, in
// cents, per MATCHING.md:
//
//	cost    = fuel_consumption/100 × fuel_price × trip_km × wear_factor
//	per_pax = round(cost / (current_passengers + 1 + 1), 0.50 €) + toll_share
//
// The divisor counts the passenger being priced and the driver alongside the
// passengers already aboard. A trip at or below the driver's free-below
// threshold, or any non-formula pricing mode, suggests nothing.
//
// This is a suggestion shown to both sides, never an enforced fare — see
// RISKS.md on staying inside cost-sharing rather than commercial transport.
func SuggestedPriceCents(in PricingInputs) int {
	if in.Mode != PricingFormula {
		return 0
	}

	// Tolls are added outside the fuel rounding, and outside the free-below
	// waiver. The fuel share is an estimate of wear and consumption, and a
	// driver may reasonably waive it on a short hop; a barrier is cash they
	// handed over on that hop, and rounding it to the nearest 50 cents would
	// make an exact number approximate for no reason.
	if in.TripKm <= in.FreeBelowKm || in.TripKm <= 0 {
		return in.TollShareCents
	}

	cost := in.FuelConsumptionLPer100Km / 100 * in.FuelPriceEUR * in.TripKm * in.WearFactor
	shares := float64(in.CurrentPassengers + 2) // + the new passenger, + the driver
	perPax := cost / shares

	cents := perPax * 100
	rounded := math.Round(cents/priceStepCents) * priceStepCents
	if rounded < 0 {
		rounded = 0
	}
	return int(rounded) + in.TollShareCents
}
