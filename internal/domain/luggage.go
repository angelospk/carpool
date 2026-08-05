package domain

import (
	"errors"
	"fmt"
	"slices"
)

// Luggage is how much somebody is bringing, or how much a driver will take.
//
// Ordered, because the whole point is the comparison: a driver who will take a
// suitcase will take a cabin bag, and one who will take nothing beyond what
// fits at your feet will not take either. An unordered set of tags cannot
// answer "does this fit".
type Luggage string

const (
	// LuggageNone is what fits at your feet. A rucksack, a laptop bag.
	LuggageNone Luggage = "none"
	// LuggageCabin is a wheeled cabin bag: it needs boot space but not much.
	LuggageCabin Luggage = "cabin"
	// LuggageSuitcase is a full suitcase, the case a boot is measured by.
	LuggageSuitcase Luggage = "suitcase"
	// LuggageBulky is a bicycle, skis, a pushchair — the thing that has to be
	// agreed in advance or somebody is standing at the kerb with it.
	LuggageBulky Luggage = "bulky"
)

// LuggageSizes lists them smallest first, which is also their order of
// increasing demand on a driver.
func LuggageSizes() []Luggage {
	return []Luggage{LuggageNone, LuggageCabin, LuggageSuitcase, LuggageBulky}
}

// ErrUnknownLuggage means the value is not one of the four.
var ErrUnknownLuggage = errors.New("domain: not a luggage size this app knows")

// ParseLuggage checks a value and returns it.
func ParseLuggage(in string) (Luggage, error) {
	l := Luggage(in)
	if !slices.Contains(LuggageSizes(), l) {
		return "", fmt.Errorf("%w: %q", ErrUnknownLuggage, in)
	}
	return l, nil
}

// rank is a size's position in the order, and the only thing Fits compares.
func (l Luggage) rank() int { return slices.Index(LuggageSizes(), l) }

// Fits reports whether a driver taking l will take what a passenger is
// bringing.
//
// The comparison is one-directional on purpose. A driver who will take a
// bicycle will obviously take a rucksack; a driver who will take a rucksack
// will not take a bicycle, and finding that out at the kerb is the failure this
// exists to prevent.
func (l Luggage) Fits(bringing Luggage) bool {
	return l.rank() >= bringing.rank()
}

// Valid reports whether l is one of the four.
func (l Luggage) Valid() bool { return l.rank() >= 0 }
