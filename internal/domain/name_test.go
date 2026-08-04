package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormaliseNameAcceptsTheNamesThePilotHas(t *testing.T) {
	// Every one of these is a name somebody in this prefecture actually has.
	// A Latin-letters allowlist, which is the obvious rule to write, rejects
	// all of them.
	for in, want := range map[string]string{
		"Γιώργος":            "Γιώργος",
		"Μαρία Παπαδοπούλου": "Μαρία Παπαδοπούλου",
		"  Νίκος  ":          "Νίκος",
		"Νίκος   Κ.":         "Νίκος Κ.",
		"Mehmet":             "Mehmet",
		"Ünal":               "Ünal",
		"O'Brien":            "O'Brien",
		"Anne-Marie":         "Anne-Marie",
		// A paste out of a form arrives with the line break in it. Collapsing
		// beats refusing: the stored name has no break in it either way, and
		// refusing sends somebody back to a field to find an invisible
		// character.
		"Γιώργος\nΜαρία": "Γιώργος Μαρία",
	} {
		got, err := NormaliseName(in)
		if err != nil {
			t.Errorf("NormaliseName(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseNameRefusesWhatBreaksAScreen(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"\n",
		"Γιώργος\u0000", // a control character
		"Γιώργος\u202e", // right-to-left override: renders as another name
		strings.Repeat("α", 81),
	} {
		if _, err := NormaliseName(in); !errors.Is(err, ErrBadName) {
			t.Errorf("NormaliseName(%q) was accepted", in)
		}
	}
}

func TestNormaliseNameCountsCharactersNotBytes(t *testing.T) {
	// A Greek letter is two bytes. A byte limit would let an English name be
	// twice as long as a Greek one, which is a rule nobody would write on
	// purpose.
	greek := strings.Repeat("α", maxNameRunes)
	if _, err := NormaliseName(greek); err != nil {
		t.Errorf("a Greek name of exactly the limit was refused: %v", err)
	}
	latin := strings.Repeat("a", maxNameRunes)
	if _, err := NormaliseName(latin); err != nil {
		t.Errorf("a Latin name of exactly the limit was refused: %v", err)
	}
}

func TestNormaliseNamePutsTheSameNameIntoTheSameString(t *testing.T) {
	// The same name typed on two keyboards arrives as two byte sequences. If
	// they are stored differently, one of them silently fails any comparison
	// and counts a different length.
	precomposed, err1 := NormaliseName("Ünal")
	decomposed, err2 := NormaliseName("Ünal") // U + combining diaeresis
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if precomposed != decomposed {
		t.Errorf("the same name normalised to %q and %q", precomposed, decomposed)
	}
}
