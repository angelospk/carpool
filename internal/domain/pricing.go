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

	// ContributionShare is how much of the computed cost share this driver
	// actually asks for, between zero (exclusive) and one.
	//
	// The app computes the ceiling; the driver decides where under it they sit.
	// That distinction is the point: an app that sets the figure is influencing
	// the price, and one that caps it is preventing a profit. Our own terms
	// already call the contribution a private arrangement between the two
	// people — this is what makes that true.
	//
	// Zero is not "ask nothing", because PricingFree already says that. A zero
	// here is a caller that forgot the field, and it is read as one: see
	// shareOf.
	ContributionShare float64

	// ConsumptionSource names where FuelConsumptionLPer100Km came from — a
	// dataset match, the CO2 on the car's registration document, or a floor for
	// its class. It changes nothing about the arithmetic and everything about
	// how firm the answer is, so it travels with it. See internal/vehicles.
	ConsumptionSource string

	// Fuel is what the car burns, which decides which pump price applies.
	Fuel string
	// RegionName is the prefecture the trip starts in, and PriceIsRegional
	// says whether its figure was looked up there or is the national one
	// standing in. A contribution built on a national average must not read
	// back a year later as though somebody had checked the pump in Ρόδο.
	RegionName      string
	PriceIsRegional bool
	PriceSource     string

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
	cents, _ := SuggestedPrice(in)
	return cents
}

// BasisVersion identifies the formula a recorded price was produced by.
//
// Bump it whenever the arithmetic changes. Without it, a basis from before a
// change replays through the new formula and disagrees with the price sitting
// next to it, and there is no way to tell that from a bug.
// Version 2 records what the price was for and where it came from: the fuel,
// the prefecture the trip started in, and whether the figure was looked up
// there or is the national one standing in. A version 1 receipt was built from
// the driver's home region and one price for every fuel, which is a different
// question answered, not the same question answered less precisely.
const BasisVersion = 2

// Basis is the receipt: every input the formula used, plus what it produced.
//
// It is stored as jsonb next to the price so that a booking stays explicable
// after the driver changes car, the vehicle dataset is refreshed and the
// region's fuel price is edited — all of which will happen, and none of which
// should change what somebody paid in March.
type Basis struct {
	Version int `json:"v"`

	ConsumptionLPer100Km float64 `json:"consumption_l_100km"`
	ConsumptionSource    string  `json:"consumption_source,omitempty"`
	FuelPriceEUR         float64 `json:"fuel_price_eur"`
	// ContributionShare is how much of the computed share the driver asked for.
	// One means all of it, which is what every route published before drivers
	// could choose was implicitly asking.
	ContributionShare float64 `json:"contribution_share,omitempty"`
	// What the price was for and where it came from. Every one of these moves
	// under a booking that has already happened — the driver changes car, a
	// prefecture gets its own figure, the national number is corrected — which
	// is the whole reason the receipt is frozen rather than recomputed.
	Fuel            string  `json:"fuel,omitempty"`
	RegionName      string  `json:"region_name,omitempty"`
	PriceIsRegional bool    `json:"price_is_regional,omitempty"`
	PriceSource     string  `json:"price_source,omitempty"`
	TripKm          float64 `json:"trip_km"`
	WearFactor      float64 `json:"wear_factor"`
	OfferedSeats    int     `json:"offered_seats"`
	FreeBelowKm     float64 `json:"free_below_km"`
	TollShareCents  int     `json:"toll_share_cents"`
	Mode            string  `json:"mode"`

	// CostEUR is the whole trip before it was divided or rounded. It is the
	// number the no-profit argument is about, and reading it back should not
	// require redoing the multiplication.
	CostEUR float64 `json:"cost_eur"`
	// SeatPriceCents is what was actually asked, tolls included.
	SeatPriceCents int `json:"seat_price_cents"`
}

// SuggestedPrice computes the contribution and the receipt for it together, so
// that the two cannot drift: there is one multiplication, and the record is
// made of the same values that produced the answer.
func SuggestedPrice(in PricingInputs) (int, Basis) {
	basis := Basis{
		Version:              BasisVersion,
		ConsumptionLPer100Km: in.FuelConsumptionLPer100Km,
		ConsumptionSource:    in.ConsumptionSource,
		FuelPriceEUR:         in.FuelPriceEUR,
		Fuel:                 in.Fuel,
		RegionName:           in.RegionName,
		PriceIsRegional:      in.PriceIsRegional,
		PriceSource:          in.PriceSource,
		TripKm:               in.TripKm,
		WearFactor:           in.WearFactor,
		OfferedSeats:         in.OfferedSeats,
		FreeBelowKm:          in.FreeBelowKm,
		ContributionShare:    shareOf(in),
		TollShareCents:       in.TollShareCents,
		Mode:                 string(in.Mode),
	}
	cents := suggest(in, &basis)
	basis.SeatPriceCents = cents
	return cents, basis
}

func suggest(in PricingInputs, basis *Basis) int {
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
	basis.CostEUR = cost
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
	// What the driver actually asks for, at or below the share the whole car
	// works out to. It can only ever lower the figure, which is what keeps the
	// no-profit arithmetic true a fortiori.
	perPax *= shareOf(in)

	cents := math.Round(perPax * 100)
	rounded := math.Floor(cents/priceStepCents) * priceStepCents
	if rounded < 0 {
		rounded = 0
	}
	return int(rounded) + in.TollShareCents
}

// shareOf reads the driver's chosen fraction, defaulting a missing one to all
// of it.
//
// All of it, rather than none, because that is what every route published
// before drivers could choose was implicitly asking for — and because the
// alternative default would have a forgotten field silently publish a route
// that asks for nothing, which is the same landmine the detour budget had.
// Anything outside (0, 1] is a caller that got it wrong, and is read the same
// way: the figure the app would have produced on its own.
func shareOf(in PricingInputs) float64 {
	if in.ContributionShare <= 0 || in.ContributionShare > 1 {
		return 1
	}
	return in.ContributionShare
}
