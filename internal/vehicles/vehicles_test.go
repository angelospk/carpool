package vehicles

import (
	"errors"
	"testing"
)

func TestLookupFindsACarByItsRegistrationDetails(t *testing.T) {
	// 1496 cm³ is a real displacement off a registration document, not a band
	// edge. Snapping to 1400 is the package's job, not the caller's.
	e, err := Lookup("Toyota", "Yaris", PetrolHybrid, 1496)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.EngineCC != 1400 {
		t.Errorf("EngineCC = %d, want the 1400 band", e.EngineCC)
	}
	if e.LPer100Km < 3 || e.LPer100Km > 6 {
		t.Errorf("a hybrid Yaris drinks %v l/100km, which is not believable", e.LPer100Km)
	}
}

// A driver types their car's name the way it is written on the document, or the
// way they think of it. The dataset stores one folded form. If the two disagree
// the lookup silently finds nothing and the driver is told their car is unknown.
func TestLookupIgnoresSpellingAndPunctuation(t *testing.T) {
	want, err := Lookup("MERCEDES BENZ", "A 180", Diesel, 1500)
	if errors.Is(err, ErrUnknown) {
		t.Skip("no Mercedes A 180 diesel in this dataset revision")
	}
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	for _, spelling := range []string{"Mercedes-Benz", "mercedes benz", "Mercedes  Benz"} {
		got, err := Lookup(spelling, "a 180", Diesel, 1500)
		if err != nil {
			t.Errorf("Lookup(%q): %v", spelling, err)
			continue
		}
		if got != want {
			t.Errorf("Lookup(%q) = %+v, want %+v", spelling, got, want)
		}
	}
}

// The common case in Έβρος, and the reason this package cannot be the whole
// answer: the consumption column starts at 2021 registrations and most of the
// fleet is older. An unknown car has to be an ordinary outcome.
func TestACarThatIsNotInTheDataIsUnknownRatherThanAGuess(t *testing.T) {
	if _, err := Lookup("Toyota", "Corolla Verso", Diesel, 2000); !errors.Is(err, ErrUnknown) {
		t.Errorf("err = %v, want ErrUnknown", err)
	}
	if _, err := Lookup("Delorean", "DMC-12", Petrol, 2800); !errors.Is(err, ErrUnknown) {
		t.Errorf("err = %v, want ErrUnknown", err)
	}
}

// A missing displacement must not fall into the 0 band and match something.
func TestLookupRefusesACarWithNoStatedDisplacement(t *testing.T) {
	for _, cc := range []int{0, -1} {
		if _, err := Lookup("Toyota", "Yaris", Petrol, cc); !errors.Is(err, ErrUnknown) {
			t.Errorf("%d cm³: err = %v, want ErrUnknown", cc, err)
		}
	}
}

func TestMakesAndModelsAreOfferedForAPicker(t *testing.T) {
	makes, err := Makes()
	if err != nil {
		t.Fatalf("Makes: %v", err)
	}
	if len(makes) < 30 {
		t.Fatalf("%d makes, want the dataset's full list", len(makes))
	}
	for i := 1; i < len(makes); i++ {
		if makes[i-1] >= makes[i] {
			t.Fatalf("makes are not sorted: %q before %q", makes[i-1], makes[i])
		}
	}

	models, err := Models("volkswagen")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("no Volkswagens, though the lookup is case-folded")
	}
	for _, m := range models {
		if m.Make != "VOLKSWAGEN" {
			t.Errorf("Models(volkswagen) returned a %s", m.Make)
		}
	}
}

// Every figure the app might quote to a driver, checked in one pass. A dataset
// refresh is a diff a human reads, and this is the floor under that reading:
// the harvester's gates could be edited and the review skipped, but a figure
// outside these bounds is not a car and stops the build.
func TestEveryPublishedFigureIsPhysicallyPossible(t *testing.T) {
	if err := ready(); err != nil {
		t.Fatalf("loading the dataset: %v", err)
	}

	fuels := map[Fuel]bool{}
	for _, f := range Fuels() {
		fuels[f] = true
	}

	for _, e := range entries {
		switch {
		case e.LPer100Km < 2 || e.LPer100Km > 25:
			t.Errorf("%s %s %s %dcc: %v l/100km is not a car",
				e.Make, e.Model, e.Fuel, e.EngineCC, e.LPer100Km)
		case e.P25 > e.LPer100Km || e.P75 < e.LPer100Km:
			t.Errorf("%s %s %s %dcc: median %v sits outside its own quartiles %v-%v",
				e.Make, e.Model, e.Fuel, e.EngineCC, e.LPer100Km, e.P25, e.P75)
		case !fuels[e.Fuel]:
			t.Errorf("%s %s %dcc: fuel %q is not one this package admits",
				e.Make, e.Model, e.EngineCC, e.Fuel)
		case e.EngineCC%100 != 0 || e.EngineCC <= 0:
			t.Errorf("%s %s %s: %d is not a displacement band",
				e.Make, e.Model, e.Fuel, e.EngineCC)
		case e.Registrations < 50:
			t.Errorf("%s %s %s %dcc: %d registrations is below the harvester's own floor",
				e.Make, e.Model, e.Fuel, e.EngineCC, e.Registrations)
		case e.Make != Normalise(e.Make) || e.Model != Normalise(e.Model):
			t.Errorf("%q %q is not stored in the folded form the lookup uses",
				e.Make, e.Model)
		}
	}
}
