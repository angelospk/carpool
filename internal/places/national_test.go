package places_test

import (
	"strings"
	"testing"

	"carpool/internal/places"
)

// These run against the embedded dataset itself rather than a fixture. Ranking
// is tested elsewhere with hand-built rows, because tying ranking to live OSM
// data once produced a test that found no matching pair and silently skipped
// while showing green. What is worth asserting here is the opposite: that the
// real file actually contains the country.

func find(t *testing.T, query, want string) places.Place {
	t.Helper()
	results := places.Search(query, 8)
	for _, p := range results {
		if p.Name == want {
			return p
		}
	}
	names := make([]string, 0, len(results))
	for _, p := range results {
		names = append(names, p.Name)
	}
	t.Fatalf("searching %q did not return %q; got %v", query, want, names)
	return places.Place{}
}

// The whole point of the expansion. Every one of these was unfindable when the
// dataset stopped at the Evros boundary, including Komotini, which is the next
// town along the only motorway out of Alexandroupoli.
func TestTheCountryIsFindable(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"θεσσαλονικ", "Θεσσαλονίκη"},
		{"κομοτην", "Κομοτηνή"},
		{"ιωαννιν", "Ιωάννινα"},
		{"ηρακλει", "Ηράκλειο"},
		{"πατρα", "Πάτρα"},
		{"ξανθ", "Ξάνθη"},
		{"αλεξανδρουπολ", "Αλεξανδρούπολη"},
	} {
		find(t, tc.query, tc.want)
	}
}

// Latin and accentless input still work at national scale, which is what the
// transliteration was for and what would break first if the keys were dropped
// to save memory.
func TestLatinAndAccentlessStillReachTheSamePlaces(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"thessaloniki", "Θεσσαλονίκη"},
		{"komotini", "Κομοτηνή"},
		{"irakleio", "Ηράκλειο"},
		{"ΞΑΝΘΗ", "Ξάνθη"},
	} {
		find(t, tc.query, tc.want)
	}
}

// A city beats a hamlet that starts the same way. Without this the search is
// technically complete and practically useless: there are 4,832 hamlets and 54
// cities, so the odds are against the one being looked for.
func TestASearchLeadsWithTheLargestPlace(t *testing.T) {
	got := places.Search("θεσσαλονικη", 5)
	if len(got) == 0 {
		t.Fatal("no results")
	}
	if got[0].Name != "Θεσσαλονίκη" || got[0].Kind != "city" {
		t.Errorf("first result is %q (%s), want the city", got[0].Name, got[0].Kind)
	}
}

// Duplicate names are the reason the municipality is carried at all. Greece has
// dozens of Άγιος Γεώργιος and a list of identical rows is no better than no
// list.
func TestAmbiguousNamesCarryTheirMunicipality(t *testing.T) {
	got := places.Search("αγιος γεωργιος", 5)
	if len(got) < 2 {
		t.Fatalf("got %d results for a name the country has many of", len(got))
	}

	areas := map[string]bool{}
	for _, p := range got {
		if p.Area == "" {
			t.Errorf("%q (%s) has no municipality to tell it apart", p.Name, p.ID)
		}
		areas[p.Area] = true
	}
	if len(areas) < 2 {
		t.Errorf("all results share one municipality (%v); they cannot be told apart", areas)
	}
}

// One letter is not a search, it is a sample of the dataset. Two is where a
// prefix starts meaning something, and it is the floor rather than three
// because the shortest Greek place names are exactly three letters — Κως, Ίος —
// and a floor of three means typing the whole name before seeing it.
func TestASingleCharacterIsNotASearch(t *testing.T) {
	if got := places.Search("α", 8); len(got) != 0 {
		t.Errorf("one character returned %d results, want none", len(got))
	}
	if got := places.Search("κω", 8); len(got) == 0 {
		t.Error("two characters returned nothing; two is meant to be enough")
	}
}

// Every row has to be usable: a coordinate that is really in Greece, a name, and
// an identity. A dataset built by a script is exactly where a silently empty
// column survives for months.
func TestEveryRowIsWellFormed(t *testing.T) {
	all := places.All()
	if len(all) < 10000 {
		t.Fatalf("the dataset holds %d settlements, want the country", len(all))
	}

	seen := map[string]bool{}
	for _, p := range all {
		if p.Name == "" || p.Area == "" {
			t.Fatalf("%s has an empty name or municipality: %+v", p.ID, p)
		}
		if !strings.HasPrefix(p.ID, "node/") || p.ID == "node/" {
			t.Fatalf("%q is not an OSM identity", p.ID)
		}
		if seen[p.ID] {
			t.Fatalf("%s appears twice", p.ID)
		}
		seen[p.ID] = true
		// Greece, generously bounded. A row outside this is a bug in the
		// point-in-polygon step, not a Greek settlement.
		if p.Lon < 19 || p.Lon > 30 || p.Lat < 34 || p.Lat > 42 {
			t.Fatalf("%s (%s) is at %.4f,%.4f, which is not in Greece", p.Name, p.ID, p.Lon, p.Lat)
		}
	}
}

// Two cities whose names begin the same way. The tie used to fall to whichever
// name was shorter, which put Αλεξάνδρεια — a town of thirteen thousand in
// Ημαθία — ahead of Αλεξανδρούπολη, and is not a fact about either of them.
func TestSizeDecidesBetweenPlacesOfTheSameKind(t *testing.T) {
	got := places.Search("alexandr", 8)
	if len(got) == 0 {
		t.Fatal("no results")
	}
	if got[0].Name != "Αλεξανδρούπολη" {
		names := make([]string, 0, len(got))
		for _, p := range got {
			names = append(names, p.Name)
		}
		t.Errorf("first result is %q, want Αλεξανδρούπολη; got %v", got[0].Name, names)
	}
}
