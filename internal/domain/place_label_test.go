package domain_test

import (
	"errors"
	"strings"
	"testing"

	"carpool/internal/domain"
)

// A place label is whatever the client said the pin was called, and until now
// nothing checked it. It is stored, and it is read back by people who were not
// there: on a trip card, and — since the demand endpoint — aggregated into a
// line shown to every driver in the area. That makes it the same kind of string
// as a display name, and it was being treated as free text with a 64 KiB body
// limit for a ceiling.

func TestAnOrdinaryPlaceLabelSurvivesUnchanged(t *testing.T) {
	for _, in := range []string{
		"Αλεξανδρούπολη",
		"Φέρες, ΚΤΕΛ Έβρου",
		"Néa Chilí",
		"Κομοτηνή",
	} {
		got, err := domain.NormalisePlaceLabel(in)
		if err != nil {
			t.Fatalf("NormalisePlaceLabel(%q): %v", in, err)
		}
		if got != in {
			t.Errorf("NormalisePlaceLabel(%q) = %q", in, got)
		}
	}
}

// A paste brings its whitespace with it, and the same name typed on two
// keyboards has to end up one string — otherwise one corridor becomes two in a
// count that is grouped by the label.
func TestAPlaceLabelIsTidiedRatherThanRefusedForWhitespace(t *testing.T) {
	got, err := domain.NormalisePlaceLabel("  Αλεξανδρούπολη \t Λιμάνι\n")
	if err != nil {
		t.Fatalf("NormalisePlaceLabel: %v", err)
	}
	if got != "Αλεξανδρούπολη Λιμάνι" {
		t.Errorf("got %q", got)
	}
}

// The reason this is not merely cosmetic. A label is republished to strangers,
// so a label that hides or rewrites what is stored is a way to put text on
// somebody else's screen.
func TestAPlaceLabelMayNotHideOrRewriteItself(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"a right-to-left override", "Αλεξ\u202eανδρούπολη"},
		{"a zero-width joiner", "Αλεξ\u200dανδρούπολη"},
		{"a control character", "Αλεξ\aανδρούπολη"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := domain.NormalisePlaceLabel(tc.in); !errors.Is(err, domain.ErrBadPlaceLabel) {
				t.Fatalf("error = %v, want ErrBadPlaceLabel", err)
			}
		})
	}
}

// Nothing at all is not a place. The column is nullable and the demand query
// already drops empty labels, but a request that arrives with a blank one is
// worth an answer rather than a row nobody can read.
func TestAnEmptyPlaceLabelIsRefused(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		if _, err := domain.NormalisePlaceLabel(in); !errors.Is(err, domain.ErrBadPlaceLabel) {
			t.Errorf("NormalisePlaceLabel(%q) error = %v, want ErrBadPlaceLabel", in, err)
		}
	}
}

// The ceiling was the request body: 64 KiB, so a label could be tens of
// kilobytes and the demand response could be made almost entirely of one
// person's typing.
func TestAPlaceLabelIsBounded(t *testing.T) {
	if _, err := domain.NormalisePlaceLabel(strings.Repeat("α", 500)); !errors.Is(err, domain.ErrBadPlaceLabel) {
		t.Fatal("a 500-character label was accepted")
	}

	// A settlement plus a landmark is the longest real case, and it has to fit.
	long := "Ρήγιο Διδυμοτείχου, στο παλιό βενζινάδικο στην έξοδο προς Ορεστιάδα"
	if _, err := domain.NormalisePlaceLabel(long); err != nil {
		t.Fatalf("a real label of %d characters was refused: %v", len([]rune(long)), err)
	}
}
