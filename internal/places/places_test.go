package places

import (
	"strconv"
	"strings"
	"testing"
)

func TestTheDatasetLoaded(t *testing.T) {
	all := All()
	if len(all) < 100 {
		t.Fatalf("only %d places loaded", len(all))
	}
	// Pinned so that a harvest which quietly drops the towns fails here rather
	// than in front of a driver looking for one.
	for _, name := range []string{"Αλεξανδρούπολη", "Ορεστιάδα", "Διδυμότειχο", "Φέρες", "Σουφλί"} {
		if !hasName(all, name) {
			t.Errorf("%s is missing from the dataset", name)
		}
	}
}

func first(t *testing.T, q string) Place {
	t.Helper()
	got := Search(q, 10)
	if len(got) == 0 {
		t.Fatalf("Search(%q) found nothing", q)
	}
	return got[0]
}

func TestSearchFindsATownHoweverItIsSpelled(t *testing.T) {
	// The whole point. Each of these is a way somebody will type the same town.
	for _, q := range []string{
		"Αλεξανδρούπολη", "Αλεξανδρουπολη", "ΑΛΕΞΑΝΔΡΟΥΠΟΛΗ",
		"alexandroupoli", "Alexandroupolis", "alexandroupolh", "aleksandroupoli",
	} {
		if got := first(t, q); got.Name != "Αλεξανδρούπολη" {
			t.Errorf("Search(%q) returned %q", q, got.Name)
		}
	}
}

func TestSearchWorksOnAPrefixInEitherScript(t *testing.T) {
	// Autocomplete is the point: nobody finishes typing Διδυμότειχο.
	for _, q := range []string{"διδυ", "didy", "Διδ", "did"} {
		if got := first(t, q); got.Name != "Διδυμότειχο" {
			t.Errorf("Search(%q) returned %q", q, got.Name)
		}
	}
}

// A hand-built set, because these are rules about ranking rather than facts
// about Evros. Tying them to live OSM data means the day a village is
// reclassified, a rule silently stops being tested.
func fixture(t *testing.T, defs ...[2]string) []indexed {
	t.Helper()
	var set []indexed
	for i, d := range defs {
		name, kind := d[0], d[1]
		row := []string{"node", strconv.Itoa(i + 1), "test", kind, name, "", "", ""}
		col := map[string]int{"osm_type": 0, "osm_id": 1, "region": 2, "kind": 3,
			"name": 4, "name_en": 5, "int_name": 6, "alt_name": 7}
		set = append(set, indexed{
			Place: Place{ID: "node/" + row[1], Kind: kind, Name: name},
			keys:  keysFor(row, col),
		})
	}
	return set
}

func TestAnExactMatchBeatsALargerPlaceThatMerelyStartsTheSame(t *testing.T) {
	// The case ranking by settlement size alone gets wrong. Someone who typed
	// the village's whole name means the village, however small it is.
	set := fixture(t, [2]string{"Νέα", "village"}, [2]string{"Νέα Βύσσα", "town"})
	got := searchIn(set, "Νέα", 5)
	if len(got) == 0 || got[0].Name != "Νέα" {
		t.Errorf("typing a village name in full returned %v", names(got))
	}
}

func TestANameOSMCarriesBeatsOneWeInvented(t *testing.T) {
	// "vyssa" is a romanisation this package generates for Βύσσα, and also the
	// actual name of somewhere else. The real name wins.
	set := fixture(t, [2]string{"Βύσσα", "town"}, [2]string{"Vyssa", "village"})
	got := searchIn(set, "vyssa", 5)
	if len(got) == 0 || got[0].Name != "Vyssa" {
		t.Errorf("a generated spelling outranked a real name: %v", names(got))
	}
}

func TestBetweenEquallyGoodMatchesTheBiggerPlaceWins(t *testing.T) {
	set := fixture(t, [2]string{"Ορεινό", "village"}, [2]string{"Ορεστιάδα", "town"})
	got := searchIn(set, "ορ", 5)
	if len(got) == 0 || got[0].Name != "Ορεστιάδα" {
		t.Errorf("the village outranked the town on a shared prefix: %v", names(got))
	}
}

func TestTheBiggerPlaceComesFirstWhenBothOnlyMatchAPrefix(t *testing.T) {
	// This used to assert that "ορ" returns Ορεστιάδα, which held while the
	// dataset was one prefecture and stopped holding the moment it was a
	// country: Ορμύλια is also a town and has a shorter name, so it wins the
	// tie honestly. Two letters cannot single out one settlement among twelve
	// thousand, and a test that pretends otherwise is pinning the dataset
	// rather than the rule.
	//
	// The rule itself — a larger place wins a tie — is tested on a fixture in
	// TestBetweenEquallyGoodMatchesTheBiggerPlaceWins. What is worth asserting
	// against the real data is that a full name is not buried by a prefix.
	if got := first(t, "ορεστιαδα"); got.Name != "Ορεστιάδα" {
		t.Errorf("Search(\"ορεστιαδα\") returned %q, want the town typed in full", got.Name)
	}
}

func TestSearchMatchesTheSecondWordOfAName(t *testing.T) {
	// Νέα Βύσσα, Νέα Χηλή and so on: half the settlements here start with a
	// word that does not distinguish them at all.
	got := Search("βυσσα", 10)
	if len(got) == 0 || !strings.Contains(got[0].Name, "Βύσσα") {
		t.Errorf("searching the second word found %v", names(got))
	}
}

func TestSearchRespectsTheLimitAndRefusesNothing(t *testing.T) {
	if got := Search("α", 3); len(got) > 3 {
		t.Errorf("limit ignored: %d results", len(got))
	}
	for _, q := range []string{"", "   ", "·", "zzzzzzzz"} {
		if got := Search(q, 10); len(got) != 0 {
			t.Errorf("Search(%q) returned %v, want nothing", q, names(got))
		}
	}
}

func TestSearchIsStable(t *testing.T) {
	// Two identical queries must not return two different orders, or the
	// suggestion under the user's finger moves between keystrokes.
	a, b := Search("ν", 10), Search("ν", 10)
	if strings.Join(names(a), "|") != strings.Join(names(b), "|") {
		t.Errorf("unstable order: %v then %v", names(a), names(b))
	}
}

func hasName(all []Place, name string) bool {
	for _, p := range all {
		if p.Name == name {
			return true
		}
	}
	return false
}

func names(ps []Place) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func TestAHamletDoesNotOutrankACityOnASharedPrefix(t *testing.T) {
	// There are 4,832 hamlets and 54 cities. If they ranked equally, a search
	// would be complete and useless in the same breath: the odds are against
	// the place being looked for appearing at all.
	// The hamlet has the shorter name on purpose, so that nothing but the kind
	// can decide it. With an earlier fixture the city won on length and the
	// test passed even when hamlets were ranked alongside cities.
	set := fixture(t, [2]string{"Λαγός", "hamlet"}, [2]string{"Λάρισα", "city"})
	got := searchIn(set, "λα", 5)
	if len(got) == 0 || got[0].Name != "Λάρισα" {
		t.Errorf("a hamlet came before the city: %v", names(got))
	}
}
