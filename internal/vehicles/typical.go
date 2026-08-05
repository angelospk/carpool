package vehicles

import (
	"fmt"
	"sort"
	"sync"
)

// TypicalVersion identifies the assumptions a planning estimate was built from.
//
// It travels with every estimate, so that a figure somebody screenshotted in
// August can be explained in November without keeping the estimate itself. It
// changes when the dataset behind it changes — which is what the digest in
// data/eea.csv records — or when the weighting below does.
const TypicalVersion = "eea-2026-08"

// Typical is what a car nobody has named yet is assumed to drink.
//
// It exists for one job: quoting a rough contribution before any driver has
// been found, when there is no car to assess. Every other figure in the app
// comes from a specific car.
type Typical struct {
	// LPer100Km is the registration-weighted median across the dataset, per
	// fuel — the middle car of the ones actually sold, not the middle row of a
	// table where a rare model counts as much as a common one.
	LPer100Km map[Fuel]float64
	// Share is how much of the dataset's registrations each fuel accounts for.
	// It stands in for the fleet mix, which is the honest limit here: these are
	// EU new-car registrations for 2021 and 2022, and the Greek fleet is older
	// and differently composed. It is a documented approximation, not a claim.
	Share   map[Fuel]float64
	Version string
}

var (
	typicalOnce sync.Once
	typical     Typical
	typicalErr  error
)

// TypicalCar returns the representative figures, computed once from the
// embedded dataset.
//
// Deliberately not the class band floors that Assess falls back on. Those
// under-state on purpose, because a car nobody can identify should not be able
// to charge as though it were identified — but an estimate built from a
// known-low input is systematically low, and quoting somebody 3 € for a trip
// that costs 4 € is its own kind of dishonesty.
func TypicalCar() (Typical, error) {
	typicalOnce.Do(func() {
		if err := ready(); err != nil {
			typicalErr = err
			return
		}
		typical = computeTypical(entries)
	})
	if typicalErr != nil {
		return Typical{}, typicalErr
	}
	return typical, nil
}

func computeTypical(rows []Entry) Typical {
	perFuel := map[Fuel][]Entry{}
	total := 0
	for _, e := range rows {
		perFuel[e.Fuel] = append(perFuel[e.Fuel], e)
		total += e.Registrations
	}

	out := Typical{
		LPer100Km: map[Fuel]float64{},
		Share:     map[Fuel]float64{},
		Version:   TypicalVersion,
	}
	for fuel, group := range perFuel {
		out.LPer100Km[fuel] = weightedMedian(group)

		registrations := 0
		for _, e := range group {
			registrations += e.Registrations
		}
		if total > 0 {
			out.Share[fuel] = float64(registrations) / float64(total)
		}
	}
	return out
}

// weightedMedian is the consumption of the middle registered car, not of the
// middle row.
//
// The distinction matters: the dataset holds one row per make/model/fuel/band,
// so a Yaris sold two hundred thousand times counts once in an unweighted
// median alongside a model sold fifty times. Weighting by registrations is what
// makes "typical" mean typical of the cars on the road.
func weightedMedian(group []Entry) float64 {
	if len(group) == 0 {
		return 0
	}
	sorted := make([]Entry, len(group))
	copy(sorted, group)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LPer100Km < sorted[j].LPer100Km })

	total := 0
	for _, e := range sorted {
		total += e.Registrations
	}
	if total == 0 {
		return sorted[len(sorted)/2].LPer100Km
	}

	seen := 0
	for _, e := range sorted {
		seen += e.Registrations
		if seen*2 >= total {
			return e.LPer100Km
		}
	}
	return sorted[len(sorted)-1].LPer100Km
}

// CostPerKmEUR is what a typical car costs in fuel per kilometre, given today's
// pump prices.
//
// The two fuels are blended by their share of the dataset rather than quoted
// separately, because a passenger with no driver yet cannot reason about a car
// that does not exist. Asking them to is asking the wrong person.
func (t Typical) CostPerKmEUR(petrolEUR, dieselEUR float64) (float64, error) {
	if petrolEUR <= 0 || dieselEUR <= 0 {
		return 0, fmt.Errorf("vehicles: both pump prices are needed, got %v and %v",
			petrolEUR, dieselEUR)
	}

	price := map[Fuel]float64{
		Petrol: petrolEUR, Diesel: dieselEUR,
		PetrolHybrid: petrolEUR, DieselHybrid: dieselEUR,
	}

	var cost, weight float64
	for fuel, l := range t.LPer100Km {
		share, priced := t.Share[fuel], price[fuel]
		if share == 0 || priced == 0 {
			continue
		}
		cost += share * priced * l / 100
		weight += share
	}
	if weight == 0 {
		return 0, fmt.Errorf("vehicles: no fuel in the dataset has both a share and a price")
	}
	// Renormalised, so a fuel the dataset carries but the app cannot price does
	// not silently drag the average towards zero.
	return cost / weight, nil
}
