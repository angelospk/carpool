// Package tolls works out which motorway toll stations a route passes and what
// they cost, from a dataset shipped with the binary rather than a live lookup.
//
// Why a local dataset. Toll prices change a few times a year, so a third party
// in the request path would buy very little and cost a dependency, a rate limit
// and a failure mode. More decisively, the question this package has to answer
// is not "what do the tolls on this route cost" but "which of them did *this
// passenger* travel through" — and no route-level service can answer that.
//
// Refresh the dataset with scripts/harvest-tolls.py.
package tolls

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed data/greece.json
var datasetJSON []byte

// Class is a vehicle class. Only the two the app can carry are exposed; the
// dataset also holds lorry prices.
type Class string

const (
	Car        Class = "car"
	Motorcycle Class = "motorcycle"
)

// Station is one toll station, in one direction of travel.
//
// Twenty-five of the eighty-six Greek stations are co-located pairs — the same
// plaza, one record per carriageway — which is why Bearings exists and why
// Group ties the pair together. Charging by proximity alone would bill every
// mainline barrier twice.
type Station struct {
	ID      int     `json:"id"`
	Group   int     `json:"group"`
	NameEL  string  `json:"name_el"`
	NameEN  string  `json:"name_en"`
	ShortEL string  `json:"short_el"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	// Bearings are the directions of travel this record serves, in degrees.
	// Five stations legitimately serve both directions; the rest serve one.
	Bearings   []float64     `json:"bearings"`
	Operator   string        `json:"operator"`
	OperatorEL string        `json:"operator_el"`
	Prices     map[Class]int `json:"prices_cents"`
}

// Price returns the cash list price in cents, and whether it is known.
//
// The source uses -1 for a station whose price it cannot state; such a station
// must be skipped rather than treated as free, or a route through it would be
// quoted too low.
func (s Station) Price(c Class) (int, bool) {
	cents, ok := s.Prices[c]
	if !ok || cents < 0 {
		return 0, false
	}
	return cents, true
}

// Dataset is the shipped toll network.
type Dataset struct {
	Schema      int       `json:"schema"`
	HarvestedAt string    `json:"harvested_at"`
	Source      string    `json:"source"`
	Note        string    `json:"note"`
	Stations    []Station `json:"stations"`
}

// Load parses the embedded dataset.
func Load() (*Dataset, error) {
	var d Dataset
	if err := json.Unmarshal(datasetJSON, &d); err != nil {
		return nil, fmt.Errorf("tolls: parsing dataset: %w", err)
	}
	if len(d.Stations) == 0 {
		return nil, fmt.Errorf("tolls: dataset has no stations")
	}
	for _, s := range d.Stations {
		if len(s.Bearings) == 0 {
			return nil, fmt.Errorf("tolls: station %d has no direction", s.ID)
		}
	}
	return &d, nil
}

// Crossing is one passage through one station, at a point along the route.
type Crossing struct {
	Station Station
	// MetresAlong is the distance from the start of the route to the barrier.
	// Metres, not a fraction: it is the measure the split is computed in, and
	// it stays meaningful when the route changes.
	MetresAlong float64
	// HeadingOff is how far the route's heading was from the station's, in
	// degrees. Kept for diagnosis: a large value that still matched is the
	// signature of a dataset problem.
	HeadingOff float64
}

// Ambiguity is a station the route came close to crossing at an angle that is
// neither clearly along it nor clearly across it. It is deliberately *not* a
// charge: quoting money for a barrier we are unsure about is worse than saying
// we do not know.
type Ambiguity struct {
	Station    Station
	HeadingOff float64
}

// Result is what the matcher found.
type Result struct {
	Crossings   []Crossing
	Ambiguous   []Ambiguity
	DatasetDate string
}

// Cents totals the crossings for a vehicle class.
//
// The second return is false when any crossed station has no price for the
// class, because a total that silently omits one is worse than no total.
func (r Result) Cents(c Class) (int, bool) {
	total := 0
	for _, x := range r.Crossings {
		cents, ok := x.Station.Price(c)
		if !ok {
			return 0, false
		}
		total += cents
	}
	return total, true
}
