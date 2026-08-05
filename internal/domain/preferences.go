package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxBioRunes bounds the free text somebody writes about themselves.
//
// Counted in runes rather than bytes, because Greek is two bytes a letter in
// UTF-8 and a byte limit would give a Greek speaker half the sentence an
// English one gets for the same rule.
//
// 500 is a short paragraph: enough for "δουλεύω στην Αλεξανδρούπολη, κατεβαίνω
// Τρίτη και Πέμπτη, δεν με πειράζει η κουβέντα", and not enough to be an essay
// nobody reads or a place to advertise.
const MaxBioRunes = 500

// PreferenceTags is the closed vocabulary a rider may describe their trips
// with, in the order they are worth showing.
//
// Closed rather than free text because a free tag cannot be translated, cannot
// be filtered on later, and becomes a second bio that nobody moderates. A fixed
// list renders in Greek or English from the same row, and a tag that turns out
// to matter can become a matching rule without a data migration.
//
// They describe; they do not enforce. "Δεν καπνίζω" is what to expect, not a
// promise the app polices.
func PreferenceTags() []string {
	return []string{
		"no_smoking", "smoking_ok",
		"no_pets", "pets_ok",
		"quiet_ride", "music_on",
		"chatty", "keep_to_myself",
		"air_conditioning",
		"big_luggage_ok", "cabin_bag_only",
	}
}

// MaxPreferences is how many somebody can end up with, and it is not a rule —
// it is arithmetic. The vocabulary is eleven tags in five contradictory pairs
// plus air conditioning, so the largest set that does not contradict itself is
// six. A separate cap would be unreachable code pretending to be a policy.
//
// It becomes a real decision the day a non-contradictory tag is added.
const MaxPreferences = 6

// contradictions are the pairs that cannot both be true. Stated rather than
// left to the reader: a profile saying both "no smoking" and "smoking is fine"
// is not a preference, it is a bug somebody will read as one or the other.
var contradictions = [][2]string{
	{"no_smoking", "smoking_ok"},
	{"no_pets", "pets_ok"},
	{"quiet_ride", "music_on"},
	{"chatty", "keep_to_myself"},
	{"big_luggage_ok", "cabin_bag_only"},
}

var (
	// ErrUnknownTag means a tag is not in the vocabulary.
	ErrUnknownTag = errors.New("domain: not a preference this app knows")
	// ErrContradictoryTags means two tags cannot both hold.
	ErrContradictoryTags = errors.New("domain: those preferences contradict each other")
)

// NormalisePreferences checks a chosen set and returns it deduplicated, in the
// vocabulary's own order so that two profiles with the same preferences read
// the same way round.
func NormalisePreferences(in []string) ([]string, error) {
	vocabulary := PreferenceTags()

	seen := map[string]bool{}
	for _, tag := range in {
		tag = strings.TrimSpace(tag)
		if !slices.Contains(vocabulary, tag) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTag, tag)
		}
		seen[tag] = true
	}

	for _, pair := range contradictions {
		if seen[pair[0]] && seen[pair[1]] {
			return nil, fmt.Errorf("%w: %s and %s", ErrContradictoryTags, pair[0], pair[1])
		}
	}
	out := make([]string, 0, len(seen))
	for _, tag := range vocabulary {
		if seen[tag] {
			out = append(out, tag)
		}
	}
	return out, nil
}

// ErrBioTooLong means the text is over MaxBioRunes.
var ErrBioTooLong = errors.New("domain: that is longer than a profile takes")

// NormaliseBio trims a bio and checks its length.
//
// Whitespace is collapsed but nothing else is: this is somebody's own words
// about themselves, and rewriting them would be worse than the ragged spacing.
// It is rendered as plain text, never as markup, so there is nothing to strip.
func NormaliseBio(in string) (string, error) {
	out := strings.TrimSpace(in)
	if utf8.RuneCountInString(out) > MaxBioRunes {
		return "", fmt.Errorf("%w: %d of at most %d characters",
			ErrBioTooLong, utf8.RuneCountInString(out), MaxBioRunes)
	}
	return out, nil
}
