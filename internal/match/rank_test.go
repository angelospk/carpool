package match_test

import (
	"testing"

	"carpool/internal/match"
)

func TestScorePrefersASmallerDetour(t *testing.T) {
	near := match.Scored{DetourMin: 4}
	far := match.Scored{DetourMin: 12}

	if match.Score(near) >= match.Score(far) {
		t.Fatalf("a 4 min detour (%v) should score better than 12 min (%v)",
			match.Score(near), match.Score(far))
	}
}

func TestScorePenalisesWalkingToThePickup(t *testing.T) {
	doorstep := match.Scored{DetourMin: 5}
	aWalkAway := match.Scored{DetourMin: 5, WalkToPickupKm: 1.5}

	if match.Score(aWalkAway) <= match.Score(doorstep) {
		t.Fatal("a pickup 1.5 km away should score worse than one at the door")
	}
}

func TestScorePenalisesWalkingFromTheDropoff(t *testing.T) {
	base := match.Scored{DetourMin: 5}
	withWalk := match.Scored{DetourMin: 5, WalkFromDropoffKm: 1.5}

	if match.Score(withWalk) <= match.Score(base) {
		t.Fatal("a dropoff 1.5 km short of the destination should score worse")
	}
}

func TestScoreGivesABoundedBonusToBetterRatedDrivers(t *testing.T) {
	unrated := match.Scored{DetourMin: 5}
	excellent := match.Scored{DetourMin: 5, DriverRating: 5}

	better := match.Score(excellent)
	if better >= match.Score(unrated) {
		t.Fatal("a well-rated driver should score at least slightly better")
	}
	// The nudge must stay small enough that rating cannot outweigh a materially
	// worse detour.
	worseButPerfect := match.Scored{DetourMin: 15, DriverRating: 5}
	if match.Score(worseButPerfect) <= match.Score(unrated) {
		t.Fatal("a 10 minute worse detour must not be outweighed by the rating bonus")
	}
}

func TestRankOrdersCandidatesBestFirst(t *testing.T) {
	got := match.Rank([]match.Scored{
		{ID: "far", DetourMin: 20},
		{ID: "near", DetourMin: 2},
		{ID: "middle", DetourMin: 9},
	})

	want := []string{"near", "middle", "far"}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("position %d = %s, want %s", i, got[i].ID, want[i])
		}
	}
}

// Ties must break on a stable key. Without one, two equally good candidates
// could swap places between runs and the sequential ask flow would re-ask a
// different driver after a restart.
func TestRankBreaksTiesDeterministically(t *testing.T) {
	input := []match.Scored{
		{ID: "ccc", DetourMin: 5},
		{ID: "aaa", DetourMin: 5},
		{ID: "bbb", DetourMin: 5},
	}

	first := match.Rank(input)
	second := match.Rank([]match.Scored{input[2], input[0], input[1]})

	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("ranking is not stable: %v vs %v", ids(first), ids(second))
		}
	}
	if first[0].ID != "aaa" {
		t.Errorf("tie broken to %s, want the lowest id aaa", first[0].ID)
	}
}

func ids(s []match.Scored) []string {
	out := make([]string, len(s))
	for i, c := range s {
		out[i] = c.ID
	}
	return out
}
