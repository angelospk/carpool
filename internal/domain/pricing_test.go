package domain

import "testing"

// baseline is a 10 km trip in a 7 l/100km car at 1.80 €/l with no wear markup
// and a single seat on offer:
//
//	cost    = 7/100 × 1.80 × 10 × 1.0 = 1.26 €
//	per_pax = 1.26 / (1 seat + 1 driver) = 0.63 €
//	floored to the 0.50 € below = 0.50 €
func baseline() PricingInputs {
	return PricingInputs{
		TripKm:                   10,
		FuelConsumptionLPer100Km: 7.0,
		FuelPriceEUR:             1.80,
		WearFactor:               1.0,
		FreeBelowKm:              5.0,
		OfferedSeats:             1,
		Mode:                     PricingFormula,
	}
}

func TestSuggestedPriceAppliesTheFuelFormula(t *testing.T) {
	if got := SuggestedPriceCents(baseline()); got != 50 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 50", got)
	}
}

func TestSuggestedPriceSplitsAcrossEveryOfferedSeatAndTheDriver(t *testing.T) {
	in := baseline()
	in.TripKm = 100 // cost = 7/100 × 1.80 × 100 = 12.60 €
	in.OfferedSeats = 2

	// 12.60 / (2 seats + 1 driver) = 4.20 € -> floored to 4.00 €
	if got := SuggestedPriceCents(in); got != 400 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 400", got)
	}
}

// The seat price is announced when the trip is published and does not move as
// the car fills. A passenger who books last must not subsidise the empty seats,
// and one who books first must not be punished for it.
func TestSuggestedPriceDoesNotDependOnHowManySeatsAreAlreadyTaken(t *testing.T) {
	in := baseline()
	in.TripKm = 100
	in.OfferedSeats = 3

	want := SuggestedPriceCents(in)
	if want == 0 {
		t.Fatal("the baseline suggests nothing, so this proves nothing")
	}
	// Nothing about who is aboard is an input any more. If a field for it ever
	// comes back, this is where it shows up.
	if got := SuggestedPriceCents(in); got != want {
		t.Fatalf("SuggestedPriceCents = %d cents, want %d", got, want)
	}
}

// The invariant this app leans on against άρθρο 18 ν. 1903/1990: what the
// passengers hand over, added up, is never more than the trip cost. Once it is
// more, the driver is earning, and an earning driver in a private car is a lot
// closer to the κόμιστρο that provision fines — see docs/research/sources.md.
//
// Prices freeze at accept, so the sum is of the figures quoted at each accept,
// not of one figure recomputed at the end.
func TestPassengersTogetherNeverPayMoreThanTheTripCosts(t *testing.T) {
	for _, seats := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		in := baseline()
		in.TripKm = 100 // cost = 7/100 × 1.80 × 100 = 12.60 €
		in.OfferedSeats = seats

		total := 0
		for taken := 0; taken < seats; taken++ {
			total += SuggestedPriceCents(in)
		}

		// Not merely "no profit". The driver is one of the people in the car and
		// pays a share like everybody else, so a full car can only ever cover
		// seats/(seats+1) of the cost. A divisor that quietly drops the driver
		// would leave the total exactly at cost, which passes a no-profit check
		// while meaning the passengers bought the whole trip.
		const costCents = 1260
		ceiling := costCents * seats / (seats + 1)
		if total > ceiling {
			t.Errorf("%d seats sold one at a time collected %d cents for a trip costing %d; the passengers' share tops out at %d, so the driver is %d ahead",
				seats, total, costCents, ceiling, total-ceiling)
		}
	}
}

func TestSuggestedPriceAppliesTheWearFactor(t *testing.T) {
	in := baseline()
	in.TripKm = 100
	in.WearFactor = 1.5 // cost = 12.60 × 1.5 = 18.90; /2 = 9.45 -> floored to 9.00

	if got := SuggestedPriceCents(in); got != 900 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 900", got)
	}
}

func TestSuggestedPriceIsZeroAtTheFreeBelowThreshold(t *testing.T) {
	in := baseline()
	in.TripKm = 5.0 // the spec's rule is "≤ free_below_km", so the boundary is free

	if got := SuggestedPriceCents(in); got != 0 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 0 at the free-below boundary", got)
	}
}

// Above the free-below threshold but below one rounding step, the honest
// suggestion is still nothing. A 5 km hop costs about 32 cents a head; the
// smallest figure the app can express is 50, and quoting that would be asking
// for half again as much as the fuel. Free is the accurate answer, not a
// shortfall to be papered over with a minimum charge.
func TestATripTooCheapToRoundToAStepIsSuggestedFree(t *testing.T) {
	in := baseline()
	in.TripKm = 5.01 // cost 0.63 €; one seat plus the driver is 0.32 € each

	if got := SuggestedPriceCents(in); got != 0 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 0", got)
	}
}

func TestSuggestedPriceIsChargedOnceItReachesAStep(t *testing.T) {
	in := baseline()
	in.TripKm = 40 // cost 5.04 €; 2.52 € each -> 2.50 €

	if got := SuggestedPriceCents(in); got != 250 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 250", got)
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

func TestSuggestedPriceFloorsToTheHalfEuroBelow(t *testing.T) {
	tests := []struct {
		name   string
		tripKm float64
		want   int
	}{
		// cost = 0.126 €/km; per_pax = cost/2 = 0.063 €/km.
		{"floors rather than rounding up", 20, 100},               // 1.260 -> 1.00
		{"floors to the step below", 16, 100},                     // 1.008 -> 1.00
		{"keeps a share that lands on a step", 15.873015873, 100}, // 1.000 -> 1.00
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

// Tolls are money the driver actually hands over, so they are added on top of
// the fuel suggestion rather than folded into it — and they are not rounded to
// the nearest 50 cents, because unlike the fuel estimate they are exact.
func TestTollShareIsAddedOnTopOfTheFuelSuggestion(t *testing.T) {
	in := PricingInputs{
		TripKm:                   60,
		FuelConsumptionLPer100Km: 7,
		FuelPriceEUR:             1.8,
		WearFactor:               1.0,
		OfferedSeats:             1,
		Mode:                     PricingFormula,
	}

	without := SuggestedPriceCents(in)
	in.TollShareCents = 275
	withTolls := SuggestedPriceCents(in)

	if withTolls != without+275 {
		t.Fatalf("with tolls = %d, want %d + 275", withTolls, without)
	}
}

// A driver who asks for nothing asks for nothing, tolls included. The figure is
// a suggestion, and suppressing it must suppress all of it.
func TestATollShareIsNotSuggestedWhenTheDriverAsksForNothing(t *testing.T) {
	in := PricingInputs{
		TripKm:                   60,
		FuelConsumptionLPer100Km: 7,
		FuelPriceEUR:             1.8,
		WearFactor:               1.0,
		TollShareCents:           275,
		Mode:                     PricingFree,
	}

	if got := SuggestedPriceCents(in); got != 0 {
		t.Fatalf("suggested %d, want 0", got)
	}
}

// A short hop under the driver's free-below threshold still costs whatever
// barrier it passed. Waiving the fuel share is a kindness; waiving a toll the
// driver paid in cash is a loss.
func TestATollShareSurvivesTheFreeBelowThreshold(t *testing.T) {
	in := PricingInputs{
		TripKm:                   4,
		FreeBelowKm:              5,
		FuelConsumptionLPer100Km: 7,
		FuelPriceEUR:             1.8,
		WearFactor:               1.0,
		TollShareCents:           140,
		Mode:                     PricingFormula,
	}

	if got := SuggestedPriceCents(in); got != 140 {
		t.Fatalf("suggested %d, want the 140 of toll alone", got)
	}
}

// A trip with no seats on it has nobody to divide the fuel across. Dividing by
// the driver alone would quote half the tank to a passenger who cannot board.
func TestATripOfferingNoSeatsSuggestsNoFuelShare(t *testing.T) {
	for _, seats := range []int{0, -1} {
		in := baseline()
		in.TripKm = 100
		in.OfferedSeats = seats

		if got := SuggestedPriceCents(in); got != 0 {
			t.Errorf("%d seats offered: SuggestedPriceCents = %d cents, want 0", seats, got)
		}
	}
}

// The toll still stands on a seatless trip, for the same reason it survives the
// free-below threshold: it is money that changed hands, not an estimate.
func TestATollSurvivesATripOfferingNoSeats(t *testing.T) {
	in := baseline()
	in.TripKm = 100
	in.OfferedSeats = 0
	in.TollShareCents = 275

	if got := SuggestedPriceCents(in); got != 275 {
		t.Fatalf("SuggestedPriceCents = %d cents, want 275", got)
	}
}

// The receipt has to contain enough to reproduce the price without consulting
// anything that can change. Every input to the formula, plus the answer.
func TestThePricingBasisRecordsEveryInputToTheFormula(t *testing.T) {
	in := baseline()
	in.TripKm = 100
	in.OfferedSeats = 3
	in.FuelConsumptionLPer100Km = 4.1
	in.ConsumptionSource = "dataset"
	in.TollShareCents = 275

	cents, basis := SuggestedPrice(in)

	if basis.SeatPriceCents != cents {
		t.Errorf("basis says %d cents, the function returned %d", basis.SeatPriceCents, cents)
	}
	for what, got := range map[string]float64{
		"trip_km":     basis.TripKm,
		"consumption": basis.ConsumptionLPer100Km,
		"fuel_price":  basis.FuelPriceEUR,
		"wear_factor": basis.WearFactor,
	} {
		if got == 0 {
			t.Errorf("the basis left %s empty, so the price cannot be reproduced from it", what)
		}
	}
	if basis.OfferedSeats != 3 {
		t.Errorf("basis offered_seats = %d, want 3", basis.OfferedSeats)
	}
	if basis.ConsumptionSource != "dataset" {
		t.Errorf("basis consumption_source = %q, want dataset", basis.ConsumptionSource)
	}
	if basis.TollShareCents != 275 {
		t.Errorf("basis toll_share_cents = %d, want 275", basis.TollShareCents)
	}
	if basis.Version == 0 {
		t.Error("the basis carries no formula version, so a later change to the formula is invisible")
	}
}

// The point of a receipt is that it still adds up later. Recomputing from the
// recorded numbers alone must land on the recorded price.
func TestAPriceCanBeRecomputedFromItsBasisAlone(t *testing.T) {
	for _, seats := range []int{1, 2, 4} {
		in := baseline()
		in.TripKm = 137.5
		in.OfferedSeats = seats
		in.FuelConsumptionLPer100Km = 6.3
		in.FuelPriceEUR = 1.734
		in.TollShareCents = 120

		want, basis := SuggestedPrice(in)

		// Nothing here reads `in`: this is what a person with only the stored
		// row would be able to do.
		replay := PricingInputs{
			TripKm:                   basis.TripKm,
			FuelConsumptionLPer100Km: basis.ConsumptionLPer100Km,
			FuelPriceEUR:             basis.FuelPriceEUR,
			WearFactor:               basis.WearFactor,
			OfferedSeats:             basis.OfferedSeats,
			TollShareCents:           basis.TollShareCents,
			Mode:                     PricingFormula,
		}
		if got := SuggestedPriceCents(replay); got != want {
			t.Errorf("%d seats: replaying the basis gave %d cents, the booking says %d",
				seats, got, want)
		}
	}
}

// The app computes a ceiling; the driver decides where under it they sit.
//
// That distinction is the whole point of the field. An app that sets the figure
// is influencing the price; one that caps it is preventing a profit — and our
// own terms already call the contribution a private arrangement between the two
// people, which this is what makes true.
func TestTheDriverAsksForAShareOfWhatTheAppWorksOut(t *testing.T) {
	full := PricingInputs{
		TripKm: 100, FuelConsumptionLPer100Km: 6.0, FuelPriceEUR: 1.80,
		WearFactor: 1.0, OfferedSeats: 3, Mode: PricingFormula,
		ContributionShare: 1.0,
	}
	half := full
	half.ContributionShare = 0.5

	atMost, _ := SuggestedPrice(full)
	asked, _ := SuggestedPrice(half)

	if atMost != 250 {
		t.Fatalf("the ceiling is %d cents, expected 250", atMost)
	}
	if asked != 100 {
		// 10,80 € over four is 2,70; half of that is 1,35, floored to 1,00.
		t.Errorf("half the share came to %d cents, want 100", asked)
	}
	if asked >= atMost {
		t.Errorf("asking for half (%d) is not below the ceiling (%d)", asked, atMost)
	}
}

// A share can only ever lower the figure, which is what keeps the no-profit
// arithmetic true a fortiori: if every seat at the full share comes to less
// than the trip cost, every seat at a fraction of it does too.
func TestAShareCanOnlyLowerWhatIsAsked(t *testing.T) {
	base := PricingInputs{
		TripKm: 240, FuelConsumptionLPer100Km: 7.0, FuelPriceEUR: 1.85,
		WearFactor: 1.0, OfferedSeats: 2, Mode: PricingFormula,
	}
	ceiling, _ := SuggestedPrice(base)

	for _, share := range []float64{0.1, 0.25, 0.5, 0.75, 0.99, 1.0} {
		in := base
		in.ContributionShare = share
		got, _ := SuggestedPrice(in)

		if got > ceiling {
			t.Errorf("a share of %v asked %d, above the %d ceiling", share, got, ceiling)
		}
	}
}

// A forgotten field asks for the whole share, not for nothing.
//
// All of it is what every route published before drivers could choose was
// implicitly asking for. The other default would have one missing line quietly
// publish a route that charges nothing — the same landmine the detour budget
// had, and not one worth building twice.
func TestAMissingShareAsksForAllOfIt(t *testing.T) {
	stated := PricingInputs{
		TripKm: 100, FuelConsumptionLPer100Km: 6.0, FuelPriceEUR: 1.80,
		WearFactor: 1.0, OfferedSeats: 3, Mode: PricingFormula,
		ContributionShare: 1.0,
	}
	forgotten := stated
	forgotten.ContributionShare = 0

	want, _ := SuggestedPrice(stated)
	got, basis := SuggestedPrice(forgotten)

	if got != want {
		t.Errorf("a forgotten share asked %d, want the full %d", got, want)
	}
	if basis.ContributionShare != 1 {
		t.Errorf("the receipt records a share of %v", basis.ContributionShare)
	}
}

// And the receipt says what the driver chose, because a price read back a year
// later is otherwise inexplicable: the arithmetic would not reproduce it.
func TestTheReceiptRecordsTheShareTheDriverChose(t *testing.T) {
	_, basis := SuggestedPrice(PricingInputs{
		TripKm: 100, FuelConsumptionLPer100Km: 6.0, FuelPriceEUR: 1.80,
		WearFactor: 1.0, OfferedSeats: 3, Mode: PricingFormula,
		ContributionShare: 0.5,
	})

	if basis.ContributionShare != 0.5 {
		t.Errorf("the receipt records %v, want 0.5", basis.ContributionShare)
	}
}
