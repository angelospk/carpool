package domain_test

import (
	"errors"
	"strings"
	"testing"

	"carpool/internal/domain"
)

// Two profiles with the same preferences must read the same way round, or a
// list of chips becomes a memory game.
func TestPreferencesComeBackInOneOrderWhicheverWayTheyWereChosen(t *testing.T) {
	first, err := domain.NormalisePreferences([]string{"pets_ok", "no_smoking", "chatty"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := domain.NormalisePreferences([]string{"chatty", "pets_ok", "no_smoking"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Errorf("%v and %v are the same preferences in different orders", first, second)
	}
	if len(first) != 3 {
		t.Errorf("%d tags, want 3", len(first))
	}
}

// A free tag cannot be translated and cannot be filtered on. The vocabulary is
// closed, and something outside it is refused rather than stored and shown to a
// stranger in whatever language it was typed.
func TestATagOutsideTheVocabularyIsRefused(t *testing.T) {
	// Stray whitespace is trimmed rather than refused: these are identifiers a
	// client sends from a fixed list, and a rogue space is its bug to survive,
	// not the person's to be told about.
	for _, tag := range []string{"", "καπνιστής", "NO_SMOKING", "<script>", "no smoking"} {
		if _, err := domain.NormalisePreferences([]string{tag}); !errors.Is(err, domain.ErrUnknownTag) {
			t.Errorf("%q gave %v, want ErrUnknownTag", tag, err)
		}
	}
}

// A profile saying both "I do not smoke" and "smoking is fine" is not a
// preference, it is something a passenger will read as one or the other.
func TestContradictoryPreferencesAreRefused(t *testing.T) {
	for _, pair := range [][]string{
		{"no_smoking", "smoking_ok"},
		{"pets_ok", "no_pets"},
		{"quiet_ride", "music_on"},
		{"chatty", "keep_to_myself"},
		{"big_luggage_ok", "cabin_bag_only"},
	} {
		if _, err := domain.NormalisePreferences(pair); !errors.Is(err, domain.ErrContradictoryTags) {
			t.Errorf("%v gave %v, want ErrContradictoryTags", pair, err)
		}
	}
}

// The point of a tag is the ones that are missing. A profile carrying every tag
// says nothing at all.
func TestPickingEverythingIsRefused(t *testing.T) {
	all := domain.PreferenceTags()
	if _, err := domain.NormalisePreferences(all); !errors.Is(err, domain.ErrContradictoryTags) {
		// The full vocabulary contradicts itself before it is too long, which is
		// the stricter of the two rules and the one that should fire.
		t.Errorf("the whole vocabulary gave %v", err)
	}

	// And six is the largest set that does not contradict itself: one from each
	// pair, plus air conditioning. Which is why there is no separate cap — a
	// number above six could never be reached.
	most := []string{
		"no_smoking", "no_pets", "quiet_ride", "chatty",
		"air_conditioning", "big_luggage_ok",
	}
	if _, err := domain.NormalisePreferences(most); err != nil {
		t.Fatalf("the largest compatible set was refused: %v", err)
	}
	if len(most) != domain.MaxPreferences {
		t.Errorf("the largest compatible set is %d tags, but MaxPreferences says %d",
			len(most), domain.MaxPreferences)
	}
}

func TestTheSameTagTwiceIsOneTag(t *testing.T) {
	got, err := domain.NormalisePreferences([]string{"chatty", "chatty", "chatty"})
	if err != nil {
		t.Fatalf("NormalisePreferences: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("%v, want one tag", got)
	}
}

// Counted in runes, not bytes. Greek is two bytes a letter in UTF-8, so a byte
// limit would give a Greek speaker half the sentence an English one gets.
func TestTheBioLimitIsTheSameLengthInGreekAsInEnglish(t *testing.T) {
	greek := strings.Repeat("α", domain.MaxBioRunes)
	if _, err := domain.NormaliseBio(greek); err != nil {
		t.Errorf("%d Greek letters were refused: %v", domain.MaxBioRunes, err)
	}

	if _, err := domain.NormaliseBio(greek + "α"); !errors.Is(err, domain.ErrBioTooLong) {
		t.Errorf("one letter over gave %v, want ErrBioTooLong", err)
	}
}

// It is somebody's own words about themselves. Trimmed at the edges, and
// otherwise left alone — the ragged spacing is better than rewriting them.
func TestABioKeepsItsOwnWords(t *testing.T) {
	got, err := domain.NormaliseBio("  Δουλεύω στην Αλεξανδρούπολη.\nΚατεβαίνω Τρίτη και Πέμπτη.  ")
	if err != nil {
		t.Fatalf("NormaliseBio: %v", err)
	}
	want := "Δουλεύω στην Αλεξανδρούπολη.\nΚατεβαίνω Τρίτη και Πέμπτη."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
