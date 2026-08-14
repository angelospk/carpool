package domain

import "math"

// BreakdownVersion is the shape of the explanation, not of the formula.
//
// Bump it when a field is added or its meaning changes, so a client written
// against an older shape can tell rather than guess.
//
// Version 2 says what the distance is a distance of, because from basis
// version 3 it is the road along the driver's route and before that it was the
// straight line between the passenger's stops.
const BreakdownVersion = 2

// Breakdown is a contribution explained: the inputs, each step of the
// arithmetic, and what came out.
//
// It is derived from the frozen Basis, never recomputed from today's figures. A
// client doing its own arithmetic would disagree with the price beside it the
// day a fuel price moved or a driver changed cars — and would disagree
// differently on every phone. So every number here is the server's, and the app
// renders them.
//
// It exists because "3.50 €" answers nothing. The question a passenger has is
// whether that is a fare, and the answer is the division: the whole car's fuel
// over every seat plus the driver, floored, plus the barriers they will be in
// the car for. Showing the division is showing that nobody profits.
type Breakdown struct {
	Version int `json:"v"`
	// BasisVersion is which formula produced the receipt this explains. A
	// version 1 receipt was built from the driver's home region and one price
	// for every fuel; version 2 from where the trip starts and what the car
	// burns.
	BasisVersion int `json:"basis_v"`

	// What the car is, and what it drinks.
	ConsumptionLPer100Km float64 `json:"consumption_l_100km"`
	ConsumptionSource    string  `json:"consumption_source,omitempty"`
	Fuel                 string  `json:"fuel,omitempty"`

	// What the fuel costs and where that figure is from.
	FuelPriceEUR    float64 `json:"fuel_price_eur"`
	RegionName      string  `json:"region_name,omitempty"`
	PriceIsRegional bool    `json:"price_is_regional"`
	// PriceSource is where the figure came from, in the words whoever set it
	// used. A price nobody can trace is a price nobody can argue with.
	PriceSource string `json:"price_source,omitempty"`

	TripKm float64 `json:"trip_km"`
	// TripKmSource is what that distance is a distance of. A receipt from
	// before version 3 carries none, and none means the straight line between
	// the passenger's own stops — never the road, and never to be shown as if
	// it were.
	TripKmSource TripKmSource `json:"trip_km_source"`

	// The arithmetic, in the order it happens.
	//
	// FuelCostCents is the whole trip, before it is divided: the number the
	// no-profit argument is about. Divisor is every seat the driver offered
	// plus the driver — not who has actually booked, so that an empty seat
	// never raises anybody else's share.
	FuelCostCents int `json:"fuel_cost_cents"`
	Divisor       int `json:"divisor"`
	ShareCents    int `json:"share_cents"`
	// ContributionShare is what the driver asked for out of that, between zero
	// and one. Below one it is the reason the rounded figure is lower than the
	// division above it, and without it the sum on screen does not follow.
	ContributionShare float64 `json:"contribution_share"`
	RoundedCents      int     `json:"rounded_cents"`
	// TollShareCents is added after the rounding and outside the waiver: a
	// barrier is cash the driver handed over, not an estimate, and rounding an
	// exact number would make it approximate for no reason.
	TollShareCents int `json:"toll_share_cents"`
	TotalCents     int `json:"total_cents"`

	// Waived means the trip was at or below the driver's free-below threshold,
	// which is a different thing from a driver who asks for nothing at all —
	// and the two must not be shown with the same sentence.
	Waived bool   `json:"waived"`
	Mode   string `json:"mode"`
}

// BreakdownOf explains a stored receipt.
//
// Every figure is read out of the Basis or derived from figures in it, so an
// old receipt is shown as what it says rather than as what today's formula
// would have made of the same trip.
func BreakdownOf(b Basis) Breakdown {
	out := Breakdown{
		Version:              BreakdownVersion,
		BasisVersion:         b.Version,
		ConsumptionLPer100Km: b.ConsumptionLPer100Km,
		ConsumptionSource:    b.ConsumptionSource,
		Fuel:                 b.Fuel,
		FuelPriceEUR:         b.FuelPriceEUR,
		RegionName:           b.RegionName,
		PriceIsRegional:      b.PriceIsRegional,
		PriceSource:          b.PriceSource,
		TripKm:               b.TripKm,
		TripKmSource:         b.TripKmSource,
		ContributionShare:    b.ContributionShare,
		FuelCostCents:        int(math.Round(b.CostEUR * 100)),
		Divisor:              b.OfferedSeats + 1,
		TollShareCents:       b.TollShareCents,
		TotalCents:           b.SeatPriceCents,
		Mode:                 b.Mode,
	}

	// A receipt written before the distance had a name was made of the straight
	// line. Reading its silence as today's measure would show an old figure as
	// something it is not.
	if out.TripKmSource == "" {
		out.TripKmSource = TripKmStraightLine
	}

	// A driver who asks for nothing charges nothing, barriers included. The
	// receipt still records the toll share that would have applied, because the
	// formula computed it before the mode discarded it — reporting it here as
	// though it were charged would show a total nobody is paying.
	if b.Mode != string(PricingFormula) {
		out.TollShareCents = 0
		out.TotalCents = 0
		return out
	}

	// What the fuel share came to before and after rounding. Both, because the
	// gap between them is the app rounding in the passenger's favour, and that
	// is worth being able to see.
	out.RoundedCents = b.SeatPriceCents - b.TollShareCents
	if out.Divisor > 0 {
		// From the unrounded cost, the way SuggestedPrice divides it. Dividing
		// the rounded FuelCostCents instead loses up to a cent, which is enough
		// to make the line above the total not add up to it.
		out.ShareCents = int(math.Round(b.CostEUR * 100 / float64(out.Divisor)))
	}

	// A trip that produced no fuel share, over a distance short enough to be
	// under the driver's own threshold, was waived. The mode needs no checking
	// here: anything but the formula returned above.
	out.Waived = out.RoundedCents == 0 && b.TripKm > 0 && b.TripKm <= b.FreeBelowKm

	return out
}
