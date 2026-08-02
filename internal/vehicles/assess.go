package vehicles

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Source records where a consumption figure came from, because the three
// answers are not equally good and the difference has to survive into the
// booking that was priced with it.
type Source string

const (
	// SourceDataset is a measured median for this exact make, model, fuel and
	// displacement band. The best answer available.
	SourceDataset Source = "dataset"
	// SourceCO2 is derived from the CO2 figure printed at field V.7 of the
	// car's own άδεια κυκλοφορίας. Specific to that car rather than to a model,
	// but only as honest as the number the driver typed in.
	SourceCO2 Source = "co2"
	// SourceBand is a conservative floor for the fuel and displacement class,
	// used when nothing better is known. It under-states on purpose.
	SourceBand Source = "band"
)

// Assessment is what the app decided a car drinks, and on what basis.
type Assessment struct {
	LPer100Km float64
	Source    Source
	// Entry is the dataset row this came from, when it came from one.
	Entry *Entry
}

// Verified reports whether the figure is specific to this car rather than a
// floor for its class. An unverified car should not be able to charge as if it
// were verified.
func (a Assessment) Verified() bool { return a.Source != SourceBand }

// Car is what the driver tells the app about their vehicle. Everything here is
// printed on the registration document, which is the point: none of it is a
// judgement the driver makes, and all of it can be checked against the paper.
type Car struct {
	Make, Model string
	Fuel        Fuel
	// EngineCC is the real displacement, not a band.
	EngineCC int
	// CO2GramsPerKm is field V.7. Zero when the document does not carry it,
	// which is the case for cars first registered before 1 November 2010.
	CO2GramsPerKm int
}

// ErrUnusable means the app cannot price this car at all: not enough was given
// to place it even in a class.
var ErrUnusable = errors.New("vehicles: not enough about this car to estimate anything")

// Grams of CO2 per litre burnt, measured from the dataset rather than taken
// from a textbook, as the median of Ewltp ÷ Fc over 9.0 million petrol and 4.3
// million diesel registrations.
//
// The quartiles are 22.6–22.9 and 26.1–26.5, which is to say the relationship
// is very nearly deterministic. It should be: burning a litre of a given fuel
// releases a fixed amount of CO2, and the test cycle scales both numbers
// together.
//
// Diesel lands 0.4% off the textbook 2640. Petrol lands 5% below the textbook
// 2392, and the likely reason is that European pump petrol is E10: ethanol
// carries less carbon per litre, and 90% petrol with 10% ethanol works out at
// about 2300. Measuring beats assuming, so the measured figures are used.
//
// The consequence for old cars: a pre-2021 document carries an NEDC figure, and
// NEDC understates real consumption by roughly a fifth. Dividing it by these
// constants gives the NEDC consumption, so an old car is under-estimated. That
// is the safe direction, and it is not corrected for, because a correction
// factor would be a number nobody measured.
var gramsCO2PerLitre = map[Fuel]float64{
	Petrol:       2270,
	PetrolHybrid: 2270,
	Diesel:       2630,
	DieselHybrid: 2630,
}

// FromCO2 turns the figure at field V.7 into a consumption.
//
// The bounds are on the answer rather than the input, because what makes a CO2
// value wrong here is that it implies a car nobody builds. A mistyped digit
// lands outside them; a plausible typo does not, which is why a person still
// checks afterwards.
func FromCO2(co2GramsPerKm int, fuel Fuel) (float64, error) {
	perLitre, ok := gramsCO2PerLitre[fuel]
	if !ok {
		return 0, fmt.Errorf("%w: no CO2 conversion for %q", ErrUnusable, fuel)
	}
	if co2GramsPerKm <= 0 {
		return 0, ErrUnusable
	}
	l := float64(co2GramsPerKm) * 100 / perLitre
	if l < minPlausible || l > maxPlausible {
		return 0, fmt.Errorf("%w: %d g/km implies %.1f l/100km", ErrUnusable, co2GramsPerKm, l)
	}
	return l, nil
}

// The same bounds the harvester applies to the dataset. A figure outside them
// is a data error rather than a car.
const (
	minPlausible = 2.0
	maxPlausible = 25.0
)

// bands are the displacement classes the conservative floor is grouped by.
// Coarser than the dataset's 100 cm³ bands on purpose: the floor is a fallback
// for cars we know least about, and slicing it thin would put it back at the
// mercy of a handful of models.
var bands = []struct {
	upTo  int
	label string
}{
	{1200, "≤1200"},
	{1600, "1300-1600"},
	{2000, "1700-2000"},
	{1 << 30, ">2000"},
}

func bandOf(engineCC int) string {
	for _, b := range bands {
		if engineCC <= b.upTo {
			return b.label
		}
	}
	return bands[len(bands)-1].label
}

var (
	floorOnce sync.Once
	floors    map[string]float64
)

// buildFloors takes the 10th percentile of every model in each fuel and
// displacement class.
//
// Tenth, not median, and the reason is the direction of the error. A car that
// is not in the dataset is almost certainly older than the dataset, and older
// cars drink more, so any figure taken from modern cars under-states it. Taking
// a low percentile of modern cars under-states it further, deliberately: the
// driver recovers less than they spent, which is the failure this app can
// afford. The opposite failure is a driver being paid more than the trip cost.
//
// It is computed from the shipped dataset rather than written down, so a
// dataset refresh moves the floor with it instead of leaving a stale constant
// behind.
func buildFloors() {
	floors = map[string]float64{}
	grouped := map[string][]float64{}
	for _, e := range entries {
		k := string(e.Fuel) + " " + bandOf(e.EngineCC)
		grouped[k] = append(grouped[k], e.LPer100Km)
	}
	for k, vs := range grouped {
		sort.Float64s(vs)
		floors[k] = vs[len(vs)/10]
	}
}

// BandFloor is the conservative figure for a car we know only the fuel and
// displacement of.
func BandFloor(fuel Fuel, engineCC int) (float64, error) {
	if err := ready(); err != nil {
		return 0, err
	}
	if engineCC <= 0 {
		return 0, ErrUnusable
	}
	floorOnce.Do(buildFloors)

	v, ok := floors[string(fuel)+" "+bandOf(engineCC)]
	if !ok {
		return 0, fmt.Errorf("%w: no floor for %q at %d cm³", ErrUnusable, fuel, engineCC)
	}
	return v, nil
}

// Assess decides what a car drinks, trying the three sources in descending
// order of how much they know about this particular car.
//
// The ordering is the whole design. The dataset knows the model but stops at
// 2021 registrations. The document knows this exact car but only back to
// November 2010. The floor knows almost nothing and says so, and is set low
// enough that saying nothing costs the driver rather than the passenger.
//
// Nothing here is a guess dressed as a measurement: every branch records where
// its number came from, and a caller that treats SourceBand like SourceDataset
// is making a choice, not an oversight.
func Assess(c Car) (Assessment, error) {
	if e, err := Lookup(c.Make, c.Model, c.Fuel, c.EngineCC); err == nil {
		return Assessment{LPer100Km: e.LPer100Km, Source: SourceDataset, Entry: &e}, nil
	} else if !errors.Is(err, ErrUnknown) {
		return Assessment{}, err
	}

	if c.CO2GramsPerKm > 0 {
		if l, err := FromCO2(c.CO2GramsPerKm, c.Fuel); err == nil {
			return Assessment{LPer100Km: l, Source: SourceCO2}, nil
		}
		// A CO2 figure that implies an impossible car is a typo, and falling
		// through to the floor is better than refusing to let the driver
		// publish. The floor under-charges, so the typo costs them, not the
		// passenger, and the review catches it.
	}

	l, err := BandFloor(c.Fuel, c.EngineCC)
	if err != nil {
		return Assessment{}, err
	}
	return Assessment{LPer100Km: l, Source: SourceBand}, nil
}
