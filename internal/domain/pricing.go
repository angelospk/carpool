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

	// OfferedSeats is how many seats the driver published on this trip, not how
	// many are taken. The cost is divided across the full car precisely because
	// a seat that goes unsold must not raise anybody else's share.
	OfferedSeats int

	Mode PricingMode

	// TollShareCents is this passenger's share of the motorway barriers they
	// will actually be in the car for, already divided among the people aboard
	// at each one — see internal/tolls. Zero when the route passes none.
	TollShareCents int
}

// SuggestedPriceCents computes the suggested per-passenger contribution, in
// cents, per MATCHING.md:
//
//	cost    = fuel_consumption/100 × fuel_price × trip_km × wear_factor
//	per_pax = floor(cost / (offered_seats + 1), 0.50 €) + toll_share
//
// The divisor is the whole car: every seat the driver offered, plus the driver.
// It deliberately does not count who has actually booked.
//
// It used to. The divisor was the passengers already aboard plus the new one
// plus the driver, which meant the first passenger paid cost/2, the second
// cost/3 and the third cost/4. Each figure looks like a share. Added up, on a
// trip costing 12.60 €, they came to 16.00 €. The driver was earning 27% over
// the cost of the trip, one honest-looking price at a time.
//
// That is not only a fairness bug. Άρθρο 18 ν. 1903/1990, as amended by άρθρο 36
// ν. 4663/2020, penalises the driver of a private car carrying passengers με
// κόμιστρο with a 3.000 € administrative fine, and reaches the owner too when
// they are a different person. What separates a fare from a shared cost is not
// defined anywhere, so the driver's best available answer is that they did not
// come out ahead. Dividing by the published seats makes coming out ahead
// arithmetically impossible: n seats at cost/(n+1) is n/(n+1) of the cost,
// always short of it, and flooring the rounding keeps it short.
//
// That is a risk control, not a safe harbour, and it does not decide how any
// authority would characterise a given trip. docs/research/sources.md has the
// provision, its amendment history, and what remains unsettled.
//
// The cost of this is that a driver who fills one seat of four recovers a fifth
// of their fuel rather than half. That is the right direction to be wrong in,
// and a driver who wants a larger share can offer fewer seats.
//
// A trip at or below the driver's free-below threshold, or any non-formula
// pricing mode, suggests nothing.
//
// This is a suggestion shown to both sides, never an enforced fare.
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

	// A trip with no seats on it cannot be priced per seat. Suggest nothing
	// rather than dividing by the driver alone and quoting them half the fuel.
	if in.OfferedSeats <= 0 {
		return in.TollShareCents
	}

	cost := in.FuelConsumptionLPer100Km / 100 * in.FuelPriceEUR * in.TripKm * in.WearFactor
	shares := float64(in.OfferedSeats + 1) // every published seat, + the driver
	perPax := cost / shares

	// Two separate steps, and the order matters.
	//
	// First settle on a whole number of cents, because the division above is
	// binary floating point and a share that is exactly 1.00 € arrives as
	// 0.9999999. Flooring that directly would drop a euro trip to 0.50 €.
	//
	// Then floor to the step. Floor, not nearest: rounding 0.63 € up to 1.00 €
	// is the same arithmetic that produced the overcharge above, in miniature,
	// and it can only ever move money towards the driver.
	cents := math.Round(perPax * 100)
	rounded := math.Floor(cents/priceStepCents) * priceStepCents
	if rounded < 0 {
		rounded = 0
	}
	return int(rounded) + in.TollShareCents
}
