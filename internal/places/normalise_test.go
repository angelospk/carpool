package places

import (
	"slices"
	"strings"
	"testing"
)

func TestFoldTreatsTheSameNameWrittenAnyWayAsTheSameName(t *testing.T) {
	// A person types a town name the way it comes out of their keyboard, and
	// there are several. Accents get dropped, caps lock happens, and a final
	// sigma is a different code point from a medial one even though it is the
	// same letter in the same word.
	want := fold("Αλεξανδρούπολη")
	for _, written := range []string{
		"Αλεξανδρουπολη",   // no accent
		"ΑΛΕΞΑΝΔΡΟΥΠΟΛΗ",   // all caps, which in Greek carries no accent
		"αλεξανδρούπολη",   // lower case
		" Αλεξανδρούπολη ", // pasted with whitespace
		"Αλεξανδρούπολη",  // tonos as a combining acute rather than precomposed
	} {
		if got := fold(written); got != want {
			t.Errorf("fold(%q) = %q, want %q", written, got, want)
		}
	}
}

func TestFoldUnifiesTheTwoSigmas(t *testing.T) {
	// Φέρες ends in a final sigma. Someone typing the town in the middle of a
	// phrase, or a data source using the medial form, must still match.
	if fold("Φέρες") != fold("Φερεσ") {
		t.Errorf("final and medial sigma folded differently: %q vs %q", fold("Φέρες"), fold("Φερεσ"))
	}
}

func TestFoldKeepsDialytikaBecauseItChangesTheWord(t *testing.T) {
	// Dialytika says two vowels are read apart rather than as one sound, so it
	// is not decoration. Losing it here would merge words the language keeps
	// separate, and would transliterate them wrongly further down.
	if fold("Κάιρο") == fold("Καϊρο") {
		t.Error("dialytika was folded away, so ai and a-i became the same word")
	}
}

func TestFoldEmptiesOutOnPunctuationAlone(t *testing.T) {
	for _, junk := range []string{"", "   ", " ", "·", "-", "'"} {
		if got := fold(junk); got != "" {
			t.Errorf("fold(%q) = %q, want empty", junk, got)
		}
	}
}

func TestLatinKeysCoverHowPeopleActuallyTypeGreekNames(t *testing.T) {
	// The point of generating more than one romanisation: there is no single
	// right answer that people agree on. Someone types the eta as an i because
	// that is how it sounds, or as an h because that is what it looks like.
	keys := latinKeys(fold("Αλεξανδρούπολη"))
	for _, want := range []string{"alexandroupoli", "alexandroupolh", "aleksandroupoli"} {
		if !slices.Contains(keys, want) {
			t.Errorf("latinKeys missing %q; got %v", want, keys)
		}
	}
}

func TestLatinKeysHandleTheDigraphsThatAreNotLetterByLetter(t *testing.T) {
	// These are pairs where Greek spelling and Greek sound part company, and a
	// letter-for-letter table gets every one of them wrong.
	cases := []struct {
		greek string
		want  string
	}{
		{"Ευρώπη", "evropi"},    // ευ is ev, not eu
		{"Μπάρα", "bara"},       // μπ is b
		{"Ντουλάπα", "doulapa"}, // ντ is d
		{"Γκάζι", "gazi"},       // γκ is g
		{"Τσάι", "tsai"},        // τσ is ts
		{"Θέρμα", "therma"},     // θ is th
		{"Ψαρά", "psara"},       // ψ is ps
		{"Χώρα", "chora"},       // χ is ch
	}
	for _, c := range cases {
		keys := latinKeys(fold(c.greek))
		if !slices.Contains(keys, c.want) {
			t.Errorf("latinKeys(%q) missing %q; got %v", c.greek, c.want, keys)
		}
	}
}

func TestDialytikaBlocksTheDigraph(t *testing.T) {
	// Καϊρο is read as two vowels, Καιρο as one sound. If the digraph table
	// runs anyway, the first is romanised with the spellings of the second and
	// the two words become findable as each other.
	keys := latinKeys(fold("Καϊρο"))
	if slices.Contains(keys, "kero") {
		t.Errorf("dialytika ignored: αϊ romanised as the αι digraph; got %v", keys)
	}
	if !slices.Contains(keys, "kairo") {
		t.Errorf("the separate vowels were not romanised at all; got %v", keys)
	}
}

func TestLatinKeysStayBounded(t *testing.T) {
	// Every ambiguous letter doubles the number of spellings, so a long name
	// with many of them would generate thousands of keys and turn a 153-row
	// dataset into a memory problem. The cap is what stops that; this is the
	// name in the dataset with the most ambiguity in it.
	worst := ""
	for _, p := range All() {
		if n := len(latinKeys(fold(p.Name))); n > len(latinKeys(fold(worst))) {
			worst = p.Name
		}
	}
	if got := len(latinKeys(fold(worst))); got > maxKeys {
		t.Errorf("%q generated %d keys, above the cap of %d", worst, got, maxKeys)
	}
}

func TestLatinKeysAreNotDuplicated(t *testing.T) {
	// A name whose ambiguous letters happen to collapse to the same spelling
	// would otherwise return that spelling several times, which quietly weights
	// ranking towards it.
	for _, p := range All() {
		keys := latinKeys(fold(p.Name))
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				t.Fatalf("%q produced %q twice", p.Name, k)
			}
			seen[k] = true
		}
	}
}

func TestFoldLeavesLatinInputAlone(t *testing.T) {
	// Someone typing the name in Latin already gets compared against the Latin
	// keys, so folding must not mangle it.
	if got := fold("Alexandroupoli"); got != "alexandroupoli" {
		t.Errorf("fold of Latin text = %q", got)
	}
	if got := fold("NÉA VYSSA"); !strings.Contains(got, "nea") {
		t.Errorf("Latin diacritics survived folding: %q", got)
	}
}
