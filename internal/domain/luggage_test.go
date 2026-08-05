package domain_test

import (
	"errors"
	"testing"

	"carpool/internal/domain"
)

// The comparison is the whole feature. A driver who will take a suitcase will
// take a cabin bag; one who will take a cabin bag will not take a bicycle, and
// finding that out at the kerb is what this prevents.
func TestWhatFitsIsOneDirectional(t *testing.T) {
	tests := []struct {
		driverTakes domain.Luggage
		bringing    domain.Luggage
		fits        bool
	}{
		{domain.LuggageBulky, domain.LuggageBulky, true},
		{domain.LuggageBulky, domain.LuggageNone, true},
		{domain.LuggageSuitcase, domain.LuggageCabin, true},
		{domain.LuggageSuitcase, domain.LuggageSuitcase, true},
		{domain.LuggageCabin, domain.LuggageSuitcase, false},
		{domain.LuggageNone, domain.LuggageCabin, false},
		{domain.LuggageNone, domain.LuggageNone, true},
		{domain.LuggageCabin, domain.LuggageBulky, false},
	}

	for _, tt := range tests {
		if got := tt.driverTakes.Fits(tt.bringing); got != tt.fits {
			t.Errorf("a driver taking %s carrying %s: fits = %v, want %v",
				tt.driverTakes, tt.bringing, got, tt.fits)
		}
	}
}

// Every size takes itself, and every size takes everything below it. Stated as
// a property rather than as eight examples, because the failure would be a new
// size inserted in the wrong place in the list.
func TestTheOrderIsConsistentWithItself(t *testing.T) {
	sizes := domain.LuggageSizes()
	for i, bigger := range sizes {
		for j, smaller := range sizes {
			want := i >= j
			if got := bigger.Fits(smaller); got != want {
				t.Errorf("%s fits %s = %v, want %v (positions %d and %d)",
					bigger, smaller, got, want, i, j)
			}
		}
	}
}

func TestASizeFromNowhereIsRefused(t *testing.T) {
	for _, in := range []string{"", "huge", "NONE", "μεγάλη", "cabin "} {
		if _, err := domain.ParseLuggage(in); !errors.Is(err, domain.ErrUnknownLuggage) {
			t.Errorf("ParseLuggage(%q) gave %v, want ErrUnknownLuggage", in, err)
		}
	}
	for _, in := range domain.LuggageSizes() {
		if got, err := domain.ParseLuggage(string(in)); err != nil || got != in {
			t.Errorf("ParseLuggage(%q) = %v, %v", in, got, err)
		}
	}
}

// A zero Luggage is not "no luggage" — it is a field somebody forgot to set,
// and it must not silently behave like the smallest size.
func TestAnUnsetSizeIsNotTheSmallestOne(t *testing.T) {
	var unset domain.Luggage

	if unset.Valid() {
		t.Error("the zero value passes as a real size")
	}
	if unset.Fits(domain.LuggageNone) {
		t.Error("an unset driver allowance quietly accepted a passenger's bag")
	}
}
