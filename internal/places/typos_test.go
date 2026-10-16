package places

import (
	"strings"
	"testing"
)

// The spellings the QA passenger typed that found nothing (QA 2026-10-16,
// U12). A letter missing, doubled or swapped is still the town.
func TestSearchForgivesATypo(t *testing.T) {
	for q, want := range map[string]string{
		"Αλεξανδρούπλη":  "Αλεξανδρούπολη",
		"Kommotini":      "Κομοτηνή",
		"Alexandroypoli": "Αλεξανδρούπολη",
		"Didymoteixo":    "Διδυμότειχο",
		"Διδημότειχο":    "Διδυμότειχο",
		"Xanthe":         "Ξάνθη",
	} {
		if got := first(t, q); got.Name != want {
			t.Errorf("Search(%q) returned %q, want %q", q, got.Name, want)
		}
	}
}

// What is after a comma is the area, typed the way an address is written.
func TestSearchReadsTheNameBeforeAComma(t *testing.T) {
	if got := first(t, "Ορεστιάδα, Έβρος"); got.Name != "Ορεστιάδα" {
		t.Errorf("returned %q", got.Name)
	}
}

// A correct spelling still beats a near one: "Φέρες" is Φέρες, not a place
// one letter away from it.
func TestAnExactSpellingBeatsATypoOfAnother(t *testing.T) {
	set := fixture(t, [2]string{"Φέρες", "town"}, [2]string{"Βέρες", "city"})
	got := searchIn(set, "Φέρες", 5)
	if len(got) == 0 || got[0].Name != "Φέρες" {
		t.Fatalf("got %+v", got)
	}
}

// Two letters are a prefix, not a typo: correcting them would match half the
// country.
func TestAShortQueryIsNotCorrected(t *testing.T) {
	set := fixture(t, [2]string{"Φέρες", "town"})
	if got := searchIn(set, "Βέρ", 5); len(got) != 0 {
		t.Fatalf("a three-letter query was corrected: %+v", got)
	}
}

// Among equally good matches, a place near the other end of the journey comes
// first. "Νέα…" typed in Evros led with Νέα Μουδανιά.
func TestANearbyPlaceComesFirstAmongEquals(t *testing.T) {
	set := fixture(t, [2]string{"Νέα Μουδανιά", "town"}, [2]string{"Νέα Βύσσα", "village"})
	set[0].Lon, set[0].Lat = 23.28, 40.24 // Χαλκιδική
	set[1].Lon, set[1].Lat = 26.54, 41.59 // Έβρος
	near := &Near{Lon: 26.52, Lat: 41.50} // Ορεστιάδα

	if got := searchNear(set, "Νέα", 5, near); got[0].Name != "Νέα Βύσσα" {
		t.Errorf("near Ορεστιάδα: %q first", got[0].Name)
	}
	if got := searchNear(set, "Νέα", 5, nil); got[0].Name != "Νέα Μουδανιά" {
		t.Errorf("with nowhere to be near: %q first", got[0].Name)
	}
}

// The endpoint is public and answers on every keystroke. A query longer than
// any place name is not one, and correcting it against every key would cost a
// thousand times a normal search.
func TestAQueryLongerThanAnyNameFindsNothing(t *testing.T) {
	long := strings.Repeat("αλεξανδρούπολη", 20)
	if got := Search(long, 8); len(got) != 0 {
		t.Fatalf("a %d-letter query found %d places", len([]rune(long)), len(got))
	}
}
