package domain_test

import (
	"testing"

	"carpool/internal/domain"
)

// What a driver asks for, at the two ends and in the shape money has to take.
//
// The no-profit invariant is tested next door across seats and distances. These
// are the parts that invariant cannot see: a share of nothing, a share of
// everything, and the fact that whatever comes out is a figure two people can
// settle at a kerb without either of them needing change.

// The fuel part of a suggestion is always a whole number of fifty-cent steps,
// and the toll is added outside it, exact to the cent.
//
// Not decoration: the step is the reason the arithmetic floors twice instead of
// rounding once, and the toll sits outside it because a barrier is cash the
// driver actually handed over. A change that folded the toll inside the
// rounding would move real money and break nothing else.
func TestASuggestionIsAlwaysSomethingTwoPeopleCanSettleAtAKerb(t *testing.T) {
	const step = 50
	for _, share := range []float64{0.25, 0.5, 0.75, 1} {
		for _, km := range []float64{6, 23.4, 87.9, 412.5} {
			for _, seats := range []int{1, 2, 3, 4} {
				in := inputs()
				in.ContributionShare = share
				in.TripKm = km
				in.OfferedSeats = seats

				cents := domain.SuggestedPriceCents(in)

				fuelPart := cents - in.TollShareCents
				if fuelPart < 0 {
					t.Fatalf("share %.2f over %.1f km with %d seats: %d cents is less than the toll alone",
						share, km, seats, cents)
				}
				if fuelPart%step != 0 {
					t.Fatalf("share %.2f over %.1f km with %d seats: the fuel part is %d cents, not a whole %d-cent step",
						share, km, seats, fuelPart, step)
				}
			}
		}
	}
}

// Zero is not a share, it is an absent one.
//
// The schema forbids it outright — contribution_share is NOT NULL with a CHECK
// that it is greater than zero — so no driver can be in that state. What a zero
// means in Go is a field nobody set, which is every route published before
// drivers could choose, and those asked for the whole share. Reading it as "none
// of the fuel" would silently reprice all of them down to the tolls.
func TestAShareNobodySetIsReadAsTheWholeThing(t *testing.T) {
	unset := inputs()
	unset.ContributionShare = 0 // the zero value of a field that predates the choice

	full := inputs()
	full.ContributionShare = 1

	got, want := domain.SuggestedPriceCents(unset), domain.SuggestedPriceCents(full)
	if got != want {
		t.Fatalf("a route from before the choice existed suggests %d cents, a full share %d", got, want)
	}
	if want <= unset.TollShareCents {
		t.Fatalf("a full share suggests %d cents, which is no more than the toll", want)
	}
}

// What a driver actually reaches for to say "just the tolls" is the smallest
// share the schema allows, and it has to come out at the toll alone rather than
// keeping a fifty-cent step they did not ask for.
func TestTheSmallestShareOnAShortHopIsTheTollAlone(t *testing.T) {
	in := inputs()
	in.ContributionShare = 0.01
	in.TripKm = 6 // a few kilometres, so a hundredth of the fuel is under a step

	if got := domain.SuggestedPriceCents(in); got != in.TollShareCents {
		t.Fatalf("the smallest share over %.0f km suggests %d cents, want the %d cent toll alone",
			in.TripKm, got, in.TollShareCents)
	}
}

// A share below zero or above one is not a smaller or larger price, it is a
// broken input. Reading it as "all of it" is the conservative direction: it can
// only ever quote the passenger the whole share the car works out to, never
// more, and never something a driver did not ask for.
func TestANonsenseShareIsReadAsAllOfIt(t *testing.T) {
	full := inputs()
	full.ContributionShare = 1
	want := domain.SuggestedPriceCents(full)

	for _, share := range []float64{-1, -0.01, 1.5, 42} {
		in := inputs()
		in.ContributionShare = share
		if got := domain.SuggestedPriceCents(in); got != want {
			t.Errorf("a share of %v suggests %d cents, want the full %d", share, got, want)
		}
	}
}

// An old receipt says what it was, not what it would be now.
//
// Up to basis version 2 the distance in a receipt was the straight line between
// the passenger's two stops; from version 3 it is the road they are carried
// along, which is a different measurement rather than a correction of the same
// one. A stored receipt is never recomputed, so both live in the table at once,
// and showing a version 2 figure as though somebody had measured a road would
// be the app claiming something nobody measured.
func TestAReceiptFromBeforeTheRoadWasMeasuredSaysSo(t *testing.T) {
	old := domain.Basis{Version: 2, TripKm: 18.8, OfferedSeats: 3, Mode: "formula"}
	if got := domain.BreakdownOf(old).TripKmSource; got != domain.TripKmStraightLine {
		t.Errorf("a version 2 receipt explains its distance as %q", got)
	}

	fresh := domain.Basis{
		Version: domain.BasisVersion, TripKm: 28.6,
		TripKmSource: domain.TripKmDriverRoute, OfferedSeats: 3, Mode: "formula",
	}
	if got := domain.BreakdownOf(fresh).TripKmSource; got != domain.TripKmDriverRoute {
		t.Errorf("a current receipt explains its distance as %q", got)
	}
	// And neither is silently repriced by being read.
	if domain.BreakdownOf(old).TripKm != 18.8 {
		t.Error("reading an old receipt changed the distance in it")
	}
}
