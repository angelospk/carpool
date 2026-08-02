// Package vehicles answers "how much does this car drink" from a checked-in
// dataset, so that the driver does not answer it themselves.
//
// The suggested contribution is consumption × fuel price × distance, and
// consumption used to be a number the driver typed. Doubling it doubles their
// takings and nothing in the app disagrees, which matters beyond fairness:
// άρθρο 18 ν. 1903/1990, as amended by άρθρο 36 ν. 4663/2020, fines a private
// car carrying passengers με κόμιστρο, and what a driver has to be able to say
// is that they did not come out ahead. See docs/research/sources.md.
//
// The data is EU registration monitoring under Regulation 2019/631, published
// by the EEA under CC-BY 4.0. scripts/harvest-vehicles.py builds it and
// explains every choice made on the way; this package only reads it.
//
// What it does not cover is the point to keep in mind. The consumption column
// exists only for 2021 and 2022 registrations, so a car older than that is
// absent, and the Greek fleet is mostly older than that. Those cars go through
// the CO2 figure on their άδεια κυκλοφορίας instead. A caller must handle
// ErrUnknown as a normal outcome rather than an edge case.
package vehicles

import (
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/eea.csv
var dataset string

// ErrUnknown means the dataset has nothing for this car. It is expected, not
// exceptional: most of the cars in a Greek prefecture predate the data.
var ErrUnknown = errors.New("vehicles: no consumption on record for this car")

// Fuel is the fuel class a consumption figure is measured for. Hybrids are
// separated from conventional cars because they are not the same car: a hybrid
// Yaris and a petrol Yaris differ by nearly a litre per 100 km, and merging
// them produces a number that is wrong for both.
type Fuel string

const (
	Petrol       Fuel = "petrol"
	Diesel       Fuel = "diesel"
	PetrolHybrid Fuel = "petrol_hybrid"
	DieselHybrid Fuel = "diesel_hybrid"
)

// Fuels lists the fuel classes the dataset carries, in a stable order.
//
// LPG, CNG, electric and plug-in hybrids are all absent on purpose. Regulation
// 2019/631 lets consumption be reported in kg or m³ as well as litres, and the
// LPG figures do not match the physical CO2-per-litre constant, so their unit
// is genuinely in doubt. A plug-in's consumption is not a property of the model
// at all — it depends how much of the trip ran on the battery.
func Fuels() []Fuel { return []Fuel{Petrol, Diesel, PetrolHybrid, DieselHybrid} }

// Entry is one row: a car and what it was measured to drink.
type Entry struct {
	Make, Model string
	Fuel        Fuel
	// EngineCC is the lower edge of a 100 cm³ band, so 1598 cm³ sits in 1500.
	EngineCC int
	// LPer100Km is the median across the registrations in the band.
	LPer100Km float64
	// P25 and P75 bound the middle half. A wide gap means the group disagrees
	// with itself; the harvester drops the worst, but a caller showing the
	// figure to a person may want to say how firm it is.
	P25, P75 float64
	// Registrations is how many cars the median was taken over.
	Registrations int
}

type key struct {
	make_, model string
	fuel         Fuel
	cc           int
}

var (
	once    sync.Once
	byKey   map[key]Entry
	entries []Entry
	loadErr error
)

// Normalise folds a make or model to the form the dataset is keyed by:
// upper case, with every run of non-alphanumerics collapsed to one space.
//
// It has to match scripts/harvest-vehicles.py exactly, because a driver typing
// "Mercedes-Benz" and a dataset row saying "MERCEDES BENZ" are the same car and
// a lookup that disagrees about that finds nothing.
func Normalise(s string) string {
	return strings.TrimSpace(nonAlnum.ReplaceAllString(strings.ToUpper(s), " "))
}

var nonAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

func load() {
	byKey = map[key]Entry{}

	r := csv.NewReader(strings.NewReader(dataset))
	r.Comment = '#'
	r.FieldsPerRecord = 9

	header, err := r.Read()
	if err != nil {
		loadErr = fmt.Errorf("vehicles: reading the dataset header: %w", err)
		return
	}
	if got, want := header[0], "make"; got != want {
		loadErr = fmt.Errorf("vehicles: dataset starts with %q, want %q", got, want)
		return
	}

	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			loadErr = fmt.Errorf("vehicles: dataset line %d: %w", line, err)
			return
		}

		num := func(s string) float64 {
			v, e := strconv.ParseFloat(s, 64)
			if e != nil && loadErr == nil {
				loadErr = fmt.Errorf("vehicles: dataset line %d: %q is not a number", line, s)
			}
			return v
		}
		e := Entry{
			Make: rec[0], Model: rec[1], Fuel: Fuel(rec[2]),
			EngineCC:      int(num(rec[3])),
			LPer100Km:     num(rec[4]),
			P25:           num(rec[5]),
			P75:           num(rec[6]),
			Registrations: int(num(rec[7])),
		}
		if loadErr != nil {
			return
		}

		k := key{e.Make, e.Model, e.Fuel, e.EngineCC}
		if _, clash := byKey[k]; clash {
			loadErr = fmt.Errorf("vehicles: dataset line %d repeats %v", line, k)
			return
		}
		byKey[k] = e
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		loadErr = errors.New("vehicles: the dataset is empty")
	}
}

func ready() error {
	once.Do(load)
	return loadErr
}

// Lookup finds the consumption for one car. engineCC is the real displacement
// off the registration document; it is snapped to the band here so that callers
// do not have to know the bands exist.
func Lookup(make_, model string, fuel Fuel, engineCC int) (Entry, error) {
	if err := ready(); err != nil {
		return Entry{}, err
	}
	if engineCC <= 0 {
		return Entry{}, ErrUnknown
	}
	e, ok := byKey[key{Normalise(make_), Normalise(model), fuel, engineCC / 100 * 100}]
	if !ok {
		return Entry{}, ErrUnknown
	}
	return e, nil
}

// Makes lists every manufacturer in the dataset, sorted.
func Makes() ([]string, error) {
	if err := ready(); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, e := range entries {
		if _, dup := seen[e.Make]; dup {
			continue
		}
		seen[e.Make] = struct{}{}
		out = append(out, e.Make)
	}
	sort.Strings(out)
	return out, nil
}

// Models lists every entry for one make, sorted by model then displacement, so
// a picker can show "which of these is your car" rather than asking for a
// number the driver would have to look up.
func Models(make_ string) ([]Entry, error) {
	if err := ready(); err != nil {
		return nil, err
	}
	wanted := Normalise(make_)
	out := []Entry{}
	for _, e := range entries {
		if e.Make == wanted {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		if out[i].Fuel != out[j].Fuel {
			return out[i].Fuel < out[j].Fuel
		}
		return out[i].EngineCC < out[j].EngineCC
	})
	return out, nil
}
