package tolls_test

import (
	"testing"

	"carpool/internal/tolls"
)

// gates builds a route with barriers at the given metres, each costing cents.
func gates(cents ...int) []tolls.Gate {
	out := make([]tolls.Gate, len(cents))
	for i, c := range cents {
		out[i] = tolls.Gate{MetresAlong: float64((i + 1) * 1000), Cents: c}
	}
	return out
}

func TestTheDriverAlonePaysEverything(t *testing.T) {
	got, err := tolls.Split(gates(100, 250), nil)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if got.Driver != 350 {
		t.Fatalf("driver pays %d, want 350", got.Driver)
	}
}

func TestAPassengerAboardThroughoutHalvesEveryGate(t *testing.T) {
	got, err := tolls.Split(gates(100, 250), []tolls.Rider{
		{ID: "a", FromMetres: 0, ToMetres: 10000},
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if got.Driver != 175 || got.Riders["a"] != 175 {
		t.Fatalf("driver %d, rider %d; want 175 each", got.Driver, got.Riders["a"])
	}
}

// The point of the whole exercise: someone carried for a shorter stretch pays
// for the barriers they were actually in the car for, and nothing else.
func TestAPassengerPaysOnlyForTheGatesTheyRodeThrough(t *testing.T) {
	// Gates at 1000, 2000, 3000 m.
	got, err := tolls.Split(gates(100, 100, 100), []tolls.Rider{
		{ID: "early", FromMetres: 0, ToMetres: 1500},   // gate 1 only
		{ID: "late", FromMetres: 1800, ToMetres: 5000}, // gates 2 and 3
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if got.Riders["early"] != 50 {
		t.Fatalf("early rider pays %d, want 50 (half of one gate)", got.Riders["early"])
	}
	if got.Riders["late"] != 100 {
		t.Fatalf("late rider pays %d, want 100 (half of two gates)", got.Riders["late"])
	}
	if got.Driver != 150 {
		t.Fatalf("driver pays %d, want 150", got.Driver)
	}
	if total := got.Driver + got.Riders["early"] + got.Riders["late"]; total != 300 {
		t.Fatalf("shares total %d, want 300", total)
	}
}

func TestARiderOnATollFreeStretchPaysNothing(t *testing.T) {
	got, err := tolls.Split(gates(240), []tolls.Rider{
		{ID: "a", FromMetres: 2000, ToMetres: 5000}, // the only gate is at 1000
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if got.Riders["a"] != 0 {
		t.Fatalf("rider pays %d, want 0", got.Riders["a"])
	}
	if got.Driver != 240 {
		t.Fatalf("driver pays %d, want the whole 240", got.Driver)
	}
}

// Cents do not always divide. Whatever the remainder, the shares must add up to
// the money actually handed over at the barrier — never one cent more or less.
func TestSharesAlwaysSumToTheAmountPaid(t *testing.T) {
	cases := [][]int{{100}, {1}, {333}, {100, 100, 100}, {1, 1, 1, 1}, {1075, 235}}
	for _, prices := range cases {
		for riders := 0; riders <= 4; riders++ {
			var rs []tolls.Rider
			for i := 0; i < riders; i++ {
				rs = append(rs, tolls.Rider{ID: string(rune('a' + i)), FromMetres: 0, ToMetres: 1e9})
			}

			got, err := tolls.Split(gates(prices...), rs)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}

			want := 0
			for _, p := range prices {
				want += p
			}
			total := got.Driver
			for _, share := range got.Riders {
				total += share
			}
			if total != want {
				t.Fatalf("prices %v with %d riders: shares total %d, want %d",
					prices, riders, total, want)
			}
		}
	}
}

// The remainder must not always land on the same person: over a season of
// trips that is a real transfer of money from whoever sorts first.
func TestTheRemainingCentDoesNotAlwaysFallOnTheSamePerson(t *testing.T) {
	seen := map[string]bool{}
	// One gate of 1 cent, three people. Someone must pay it; who, must vary
	// with the trip rather than with the alphabet.
	for i := 0; i < 12; i++ {
		g := []tolls.Gate{{MetresAlong: float64(1000 + i), Cents: 1}}
		got, err := tolls.Split(g, []tolls.Rider{
			{ID: "a", FromMetres: 0, ToMetres: 1e9},
			{ID: "b", FromMetres: 0, ToMetres: 1e9},
		})
		if err != nil {
			t.Fatalf("Split: %v", err)
		}
		switch {
		case got.Driver == 1:
			seen["driver"] = true
		case got.Riders["a"] == 1:
			seen["a"] = true
		case got.Riders["b"] == 1:
			seen["b"] = true
		}
	}

	if len(seen) < 2 {
		t.Fatalf("the odd cent always went to %v", seen)
	}
}

// Boundaries are half-open [from, to): boarding at a barrier means you were in
// the car for it; alighting at one means you were not. There is no physically
// correct answer when the projections coincide, so it has to be a rule.
func TestBoardingAtAGateCountsAndAlightingAtOneDoesNot(t *testing.T) {
	g := []tolls.Gate{{MetresAlong: 1000, Cents: 100}}

	boarding, err := tolls.Split(g, []tolls.Rider{{ID: "a", FromMetres: 1000, ToMetres: 5000}})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if boarding.Riders["a"] != 50 {
		t.Fatalf("boarding at the gate paid %d, want 50", boarding.Riders["a"])
	}

	alighting, err := tolls.Split(g, []tolls.Rider{{ID: "a", FromMetres: 0, ToMetres: 1000}})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if alighting.Riders["a"] != 0 {
		t.Fatalf("alighting at the gate paid %d, want 0", alighting.Riders["a"])
	}
}

func TestRejectsARiderWhoAlightsBeforeBoarding(t *testing.T) {
	_, err := tolls.Split(gates(100), []tolls.Rider{{ID: "a", FromMetres: 900, ToMetres: 100}})
	if err == nil {
		t.Fatal("a backwards ride was accepted")
	}
}

func TestRejectsTwoRidersWithTheSameID(t *testing.T) {
	_, err := tolls.Split(gates(100), []tolls.Rider{
		{ID: "a", FromMetres: 0, ToMetres: 5000},
		{ID: "a", FromMetres: 0, ToMetres: 5000},
	})
	if err == nil {
		t.Fatal("duplicate rider ids were accepted")
	}
}

// Order of the rider list is an implementation detail of whoever calls this. It
// must not move money.
func TestRiderOrderDoesNotChangeAnyShare(t *testing.T) {
	a := tolls.Rider{ID: "a", FromMetres: 0, ToMetres: 2500}
	b := tolls.Rider{ID: "b", FromMetres: 500, ToMetres: 5000}
	c := tolls.Rider{ID: "c", FromMetres: 1200, ToMetres: 1900}

	one, err := tolls.Split(gates(100, 100, 100), []tolls.Rider{a, b, c})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	two, err := tolls.Split(gates(100, 100, 100), []tolls.Rider{c, b, a})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if one.Driver != two.Driver {
		t.Fatalf("driver share moved with the order: %d vs %d", one.Driver, two.Driver)
	}
	for id, share := range one.Riders {
		if two.Riders[id] != share {
			t.Fatalf("rider %s share moved with the order: %d vs %d", id, share, two.Riders[id])
		}
	}
}

// Adding someone to the car can only ever reduce what everyone else pays.
func TestAnExtraPassengerNeverIncreasesAnyoneElsesShare(t *testing.T) {
	g := gates(300, 300)
	before, err := tolls.Split(g, []tolls.Rider{{ID: "a", FromMetres: 0, ToMetres: 5000}})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	after, err := tolls.Split(g, []tolls.Rider{
		{ID: "a", FromMetres: 0, ToMetres: 5000},
		{ID: "b", FromMetres: 0, ToMetres: 5000},
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	if after.Riders["a"] > before.Riders["a"] {
		t.Fatalf("rider a paid %d alone and %d with company", before.Riders["a"], after.Riders["a"])
	}
	if after.Driver > before.Driver {
		t.Fatalf("driver paid %d then %d", before.Driver, after.Driver)
	}
}
