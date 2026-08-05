package places_test

import (
	"regexp"
	"strings"
	"testing"

	"carpool/internal/places"
)

// Alexandroupoli, the pilot's own city. If the biggest place in the region has
// nowhere to wait, the feature does not exist.
const alexandroupoli = "node/139260064"

func TestACityHasSomewhereToWait(t *testing.T) {
	got := places.PointsFor(alexandroupoli)
	if len(got) == 0 {
		t.Fatal("no meeting points for Αλεξανδρούπολη")
	}
	for _, p := range got {
		if p.Name == "" || p.Kind == "" {
			t.Errorf("a meeting point with no name or kind: %+v", p)
		}
		if p.Lon == 0 || p.Lat == 0 {
			t.Errorf("%q has no coordinate", p.Name)
		}
	}
}

// The decision this dataset is built around. A village with nothing tagged gets
// nothing: an earlier plan put a synthetic "Κέντρο <village>" at the settlement
// node, which is a coordinate wearing a confident name, and the one thing a
// meeting point promises is that it can be found.
func TestASettlementWithNoLandmarkGetsNothingInvented(t *testing.T) {
	got := places.PointsFor("node/does-not-exist")
	if got != nil {
		t.Errorf("invented %d meeting points for a place that has none", len(got))
	}
}

// Bigger places get more, because "the petrol station" is an answer in a
// village and a question in a city.
func TestACityIsAllowedMorePointsThanAVillage(t *testing.T) {
	var city, village int
	for _, p := range places.All() {
		n := len(places.PointsFor(p.ID))
		switch p.Kind {
		case "city":
			if n > city {
				city = n
			}
		case "village":
			if n > village {
				village = n
			}
		}
	}
	if city <= village {
		t.Errorf("the most a city gets is %d and a village %d; a city should be allowed more", city, village)
	}
}

// Every point has to belong to a settlement that exists. A dataset joined by a
// string is exactly where a rename leaves orphans nobody notices.
func TestEveryMeetingPointBelongsToARealSettlement(t *testing.T) {
	known := map[string]bool{}
	for _, p := range places.All() {
		known[p.ID] = true
	}

	seen := map[string]bool{}
	for _, mp := range places.AllPoints() {
		if !known[mp.PlaceID] {
			t.Fatalf("%q belongs to %q, which is not a settlement", mp.Name, mp.PlaceID)
		}
		if seen[mp.ID] {
			t.Fatalf("%s appears twice", mp.ID)
		}
		seen[mp.ID] = true
	}
	if len(seen) < 1000 {
		t.Fatalf("only %d meeting points; the dataset is not built", len(seen))
	}
}

// The harvester's job is the systematic junk; a moderator's is the judgement
// calls. These are the systematic kinds, and they must not come back.
func TestTheListHoldsNoBusStopsWearingAStationsTag(t *testing.T) {
	timetable := regexp.MustCompile(`(Νο\s?\d|ΓΡΑΜΜΗ\s?\d|->|→)`)
	// (^|non-letter) rather than \b: Go's word boundary is ASCII-only, so \bΤ
	// never matches after a Greek letter and the rule would quietly cover less
	// than it reads as covering.
	urban := regexp.MustCompile(`(Αφετηρ|Αμαξοστάσ|(^|[^\p{L}\p{N}_])Τ\.\s?Σ\.)`)

	for _, p := range places.AllPoints() {
		if timetable.MatchString(p.Name) {
			t.Errorf("%s (%s) is a route number, not a place", p.Name, p.ID)
		}
		if urban.MatchString(p.Name) {
			t.Errorf("%s (%s) is a piece of the city bus network", p.Name, p.ID)
		}
		if p.Kind == "port" && strings.Contains(p.Name, "&") {
			t.Errorf("%s (%s) is a berth named after the boats at it", p.Name, p.ID)
		}
	}
}

// Two identical names under one settlement is a choice between two identical
// answers, which is not a choice.
func TestNoSettlementOffersTheSameNameTwice(t *testing.T) {
	seen := map[string]map[string]bool{}
	for _, p := range places.AllPoints() {
		names, ok := seen[p.PlaceID]
		if !ok {
			names = map[string]bool{}
			seen[p.PlaceID] = names
		}
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if names[key] {
			t.Errorf("%s offers %q twice", p.PlaceID, p.Name)
		}
		names[key] = true
	}
}
