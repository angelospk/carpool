package domain

import "math"

// EstimateKind is the discriminator that keeps a guess and a receipt apart.
//
// It is a distinct string rather than a boolean on Basis, deliberately. A
// boolean is one forgotten check away from a guess being stored as what
// somebody agreed to pay; a type that is not Basis cannot be frozen into a
// booking at all, because nothing that takes a Basis will accept it.
const EstimateKind = "planning_estimate"

// EstimateAssumptions records what a figure was built from.
//
// It travels with the estimate so the number can be explained without the
// estimate being kept. Nothing about this is stored: an estimate somebody
// screenshotted in August is explained in November by replaying these, not by
// looking it up.
type EstimateAssumptions struct {
	// Version identifies the consumption dataset and weighting behind it.
	Version string `json:"version"`
	// Divisor is the whole car it was divided by — the seats a route usually
	// offers, plus the driver.
	Divisor int `json:"divisor"`
	// CostPerKmCents is what a typical car was assumed to burn per kilometre at
	// today's pump prices, blended across fuels by their share of the fleet.
	CostPerKmCents float64 `json:"cost_per_km_cents"`
	// FuelPriceAsOf is when the pump prices behind that were last touched. A
	// figure built on a six-month-old price is worth less than one built on
	// last week's, and only this says which it is.
	FuelPriceAsOf string `json:"fuel_price_as_of,omitempty"`
	// RegionName is where the prices were read for.
	RegionName string `json:"region_name,omitempty"`
}

// PlanningEstimate is roughly what a seat on a trip like this tends to cost.
//
// It answers the question every first-time user has and the app could not
// previously answer at all: you see a figure only once a driver has been asked,
// because it depends on their car and how many seats they published, and before
// that there was nothing.
//
// What it is not, and what the wording around it has to keep saying: an offer.
// No driver has seen this trip, nobody has agreed to anything, and the real
// contribution arrives with a specific car. It is closer to a fuel calculator
// than to a price.
type PlanningEstimate struct {
	Kind string `json:"kind"`

	RouteKm  float64 `json:"route_km"`
	RouteMin int     `json:"route_min"`

	// ContributionCents is the headline: one number, floored to the same half
	// euro the real formula floors to, so the estimate and the eventual figure
	// are at least made the same way.
	ContributionCents int `json:"contribution_cents"`
	// CeilingCents is the same trip in the thirstiest car the app will price,
	// at the same number of seats.
	//
	// It is here because the honest failure of a single figure is upward: a
	// passenger quoted 12 € and then asked for 25 € has been misled, even
	// though both numbers were produced honestly from different cars. There is
	// deliberately no matching floor — the thriftiest real car is five times
	// cheaper than the ceiling, and a range that wide reads as "nobody knows"
	// and gets ignored. Nobody is upset by paying less than they expected.
	CeilingCents int `json:"ceiling_cents"`

	Assumptions EstimateAssumptions `json:"assumptions"`
}

// EstimateInputs are what the estimate is computed from.
type EstimateInputs struct {
	RouteKm  float64
	RouteMin int
	// CostPerKmEUR is a typical car's fuel cost per kilometre at the pump
	// prices where the trip starts. See vehicles.Typical.
	CostPerKmEUR float64
	// CeilingCostPerKmEUR is the same, for the thirstiest car the app will
	// price. See vehicles.DearestCostPerKmEUR.
	CeilingCostPerKmEUR float64
	// Divisor is the whole car: the seats a route usually offers, plus the
	// driver. Not a guess about this trip, which has no driver.
	Divisor int

	Version       string
	FuelPriceAsOf string
	RegionName    string
}

// Estimate computes the planning figure.
//
// Divided and floored exactly as SuggestedPrice divides and floors, so that the
// estimate cannot be systematically above what the real formula would produce
// for the same trip. Being under is tolerable; being over is the app quoting
// somebody a number they will not be asked for.
func Estimate(in EstimateInputs) PlanningEstimate {
	out := PlanningEstimate{
		Kind:     EstimateKind,
		RouteKm:  in.RouteKm,
		RouteMin: in.RouteMin,
		Assumptions: EstimateAssumptions{
			Version:        in.Version,
			Divisor:        in.Divisor,
			CostPerKmCents: in.CostPerKmEUR * 100,
			FuelPriceAsOf:  in.FuelPriceAsOf,
			RegionName:     in.RegionName,
		},
	}
	// Not a correctness check on the number — with any of these at zero the
	// arithmetic below already lands on nothing. It is a guard against dividing
	// by a zero divisor, where int(+Inf) is undefined in Go and lands on
	// whatever the platform does with it.
	if in.RouteKm <= 0 || in.CostPerKmEUR <= 0 || in.Divisor <= 0 {
		return out
	}

	perSeat := in.RouteKm * in.CostPerKmEUR / float64(in.Divisor)
	out.ContributionCents = floorToStep(int(math.Round(perSeat * 100)))

	// Compared after flooring rather than before. A car that drinks a little
	// more than typical is dearer per kilometre and still lands on the same
	// half euro, and "περίπου 2 €, έως 2 €" is noise where an upper bound was
	// meant to be.
	if in.CeilingCostPerKmEUR > in.CostPerKmEUR {
		dearest := in.RouteKm * in.CeilingCostPerKmEUR / float64(in.Divisor)
		if ceiling := floorToStep(int(math.Round(dearest * 100))); ceiling > out.ContributionCents {
			out.CeilingCents = ceiling
		}
	}
	return out
}

// floorToStep rounds down to the half euro.
//
// Down, and to a step — not up, and not to a minimum. A trip whose share comes
// to 40 cents estimates at nothing, which is the honest answer: on a hop that
// short a driver is unlikely to ask for anything.
func floorToStep(cents int) int {
	if cents <= 0 {
		return 0
	}
	return cents / priceStepCents * priceStepCents
}
