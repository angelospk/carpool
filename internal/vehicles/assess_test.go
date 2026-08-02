package vehicles

import (
	"errors"
	"math"
	"testing"
)

func TestAssessPrefersTheDatasetWhenTheModelIsKnown(t *testing.T) {
	got, err := Assess(Car{Make: "Toyota", Model: "Yaris", Fuel: PetrolHybrid,
		EngineCC: 1490, CO2GramsPerKm: 92})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Source != SourceDataset {
		t.Errorf("Source = %q, want %q even though a CO2 figure was supplied",
			got.Source, SourceDataset)
	}
	if got.Entry == nil {
		t.Error("a dataset assessment did not carry the row it came from")
	}
}

// The pre-2021 case, which is most of the Έβρος fleet. The model is not in the
// data, but the driver's own document has the number.
func TestAssessFallsBackToTheFigureOnTheRegistrationDocument(t *testing.T) {
	got, err := Assess(Car{Make: "Toyota", Model: "Corolla Verso", Fuel: Diesel,
		EngineCC: 2000, CO2GramsPerKm: 160})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Source != SourceCO2 {
		t.Fatalf("Source = %q, want %q", got.Source, SourceCO2)
	}
	// 160 g/km ÷ 26.30 g per litre × 100 = 6.08 l/100km.
	if math.Abs(got.LPer100Km-6.08) > 0.05 {
		t.Errorf("LPer100Km = %v, want about 6.08", got.LPer100Km)
	}
}

func TestAssessFallsBackToTheClassFloorWhenNothingElseIsKnown(t *testing.T) {
	got, err := Assess(Car{Make: "Toyota", Model: "Corolla Verso", Fuel: Petrol,
		EngineCC: 1600})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Source != SourceBand {
		t.Fatalf("Source = %q, want %q", got.Source, SourceBand)
	}
	if got.Verified() {
		t.Error("a class floor reported itself as verified")
	}
}

// The floor exists to be wrong in one direction only. If it ever sat above the
// typical car in its class, an unverified driver would be overcharging
// passengers by default, which is the failure this whole package exists to
// prevent.
func TestTheClassFloorIsBelowWhatThatClassActuallyDrinks(t *testing.T) {
	if err := ready(); err != nil {
		t.Fatalf("loading: %v", err)
	}

	for _, fuel := range Fuels() {
		for _, cc := range []int{1000, 1400, 1800, 2500} {
			floor, err := BandFloor(fuel, cc)
			if errors.Is(err, ErrUnusable) {
				continue // no cars of that fuel in that band
			}
			if err != nil {
				t.Fatalf("BandFloor(%s, %d): %v", fuel, cc, err)
			}

			var above, total int
			for _, e := range entries {
				if e.Fuel != fuel || bandOf(e.EngineCC) != bandOf(cc) {
					continue
				}
				total++
				if e.LPer100Km >= floor {
					above++
				}
			}
			if total == 0 {
				continue
			}
			if share := float64(above) / float64(total); share < 0.85 {
				t.Errorf("%s %s: floor %v is above %.0f%% of the class, so it overcharges them",
					fuel, bandOf(cc), floor, (1-share)*100)
			}
		}
	}
}

// The previous flat default of 7.0 was not neutral: a small petrol car drinks
// about 5.6, so every one of them was quoting a quarter more than it burnt.
func TestTheFloorForASmallPetrolCarIsBelowTheOldFlatDefault(t *testing.T) {
	floor, err := BandFloor(Petrol, 1200)
	if err != nil {
		t.Fatalf("BandFloor: %v", err)
	}
	if floor >= 7.0 {
		t.Fatalf("floor for a small petrol car is %v, no better than the 7.0 it replaces", floor)
	}
}

// Both constants pinned, because each fuel has its own and a test that only
// exercises one leaves the other free to be anything.
//
// The expected values are arithmetic, not fixtures: 100 × CO2 ÷ grams per
// litre. A change here means either the dataset moved or somebody edited a
// constant, and both deserve a person looking.
func TestTheCO2ConversionMatchesTheMeasuredConstantForEachFuel(t *testing.T) {
	tests := []struct {
		fuel Fuel
		co2  int
		want float64 // l/100km
	}{
		{Petrol, 130, 5.73},      // 130 × 100 ÷ 2270
		{PetrolHybrid, 92, 4.05}, // hybrids burn the same petrol
		{Diesel, 160, 6.08},      // 160 × 100 ÷ 2630
		{DieselHybrid, 120, 4.56},
	}
	for _, tt := range tests {
		got, err := FromCO2(tt.co2, tt.fuel)
		if err != nil {
			t.Errorf("FromCO2(%d, %s): %v", tt.co2, tt.fuel, err)
			continue
		}
		if math.Abs(got-tt.want) > 0.02 {
			t.Errorf("FromCO2(%d, %s) = %.2f, want %.2f", tt.co2, tt.fuel, got, tt.want)
		}
	}
}

// Diesel carries more carbon per litre than petrol, so the same CO2 reading
// means less diesel was burnt. If the two constants were ever swapped every
// figure would still look plausible, and this is what would notice.
func TestTheSameCO2MeansLessDieselThanPetrol(t *testing.T) {
	petrol, err := FromCO2(150, Petrol)
	if err != nil {
		t.Fatalf("FromCO2 petrol: %v", err)
	}
	diesel, err := FromCO2(150, Diesel)
	if err != nil {
		t.Fatalf("FromCO2 diesel: %v", err)
	}
	if diesel >= petrol {
		t.Errorf("150 g/km is %v l of diesel and %v l of petrol; diesel has to be the smaller",
			diesel, petrol)
	}
}

func TestACO2FigureThatImpliesNoRealCarIsRefused(t *testing.T) {
	for _, co2 := range []int{0, -5, 12, 9000} {
		if _, err := FromCO2(co2, Petrol); !errors.Is(err, ErrUnusable) {
			t.Errorf("FromCO2(%d) = nil error, want ErrUnusable", co2)
		}
	}
}

// A typo in the CO2 box must not stop the driver publishing, and must not let
// them charge as though they had proved something.
func TestAnImpossibleCO2FigureFallsThroughToTheFloor(t *testing.T) {
	got, err := Assess(Car{Make: "Nothing", Model: "At All", Fuel: Petrol,
		EngineCC: 1600, CO2GramsPerKm: 9000})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Source != SourceBand {
		t.Errorf("Source = %q, want %q", got.Source, SourceBand)
	}
}

func TestACarWithNoStatedDisplacementCannotBeAssessed(t *testing.T) {
	if _, err := Assess(Car{Make: "Toyota", Model: "Corolla", Fuel: Petrol}); !errors.Is(err, ErrUnusable) {
		t.Errorf("err = %v, want ErrUnusable", err)
	}
}

// Every route through Assess has to say which one it took, because the figure
// is frozen into a booking and has to stay explicable afterwards.
func TestEveryAssessmentNamesItsSource(t *testing.T) {
	known := map[Source]bool{SourceDataset: true, SourceCO2: true, SourceBand: true}
	cars := []Car{
		{Make: "Toyota", Model: "Yaris", Fuel: PetrolHybrid, EngineCC: 1490},
		{Make: "Unknown", Model: "Thing", Fuel: Diesel, EngineCC: 1600, CO2GramsPerKm: 140},
		{Make: "Unknown", Model: "Thing", Fuel: Diesel, EngineCC: 1600},
	}
	for _, c := range cars {
		got, err := Assess(c)
		if err != nil {
			t.Errorf("Assess(%v): %v", c, err)
			continue
		}
		if !known[got.Source] {
			t.Errorf("Assess(%v) reported source %q", c, got.Source)
		}
		if got.LPer100Km < minPlausible || got.LPer100Km > maxPlausible {
			t.Errorf("Assess(%v) = %v l/100km", c, got.LPer100Km)
		}
	}
}
