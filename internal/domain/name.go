package domain

import (
	"errors"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ErrBadName is returned when a display name cannot be used.
var ErrBadName = errors.New("that name cannot be used")

// maxNameRunes counts characters rather than bytes, because a Greek name costs
// two bytes a letter and a byte limit would quietly allow half the length of an
// English one. maxNameBytes is the defensive backstop underneath it.
const (
	maxNameRunes = 80
	maxNameBytes = 400
)

// NormaliseName cleans up a display name, or refuses it.
//
// Refusing rather than silently trimming: a name is what other people see next
// to a stranger they are getting into a car with, and one that arrives with a
// line break in it, or is entirely spaces, is worth an answer rather than a
// quiet correction.
//
// Deliberately not restricted to a letter allowlist. The obvious rule to write
// is Latin letters plus spaces, which rejects every Greek name in the pilot;
// the rule after that is Greek plus Latin, which rejects the region's Turkish,
// Bulgarian and Pomak names. So what is blocked is what breaks a screen or
// hides text: control characters, line breaks, and the invisible formatting
// runes that let a name pretend to be somebody else's.
func NormaliseName(in string) (string, error) {
	// NFC first, so that the same name typed on two keyboards is one string and
	// the length limit means the same thing for both.
	n := norm.NFC.String(in)

	// Any run of whitespace becomes one space, which also removes the tabs and
	// newlines a paste brings with it.
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return "", ErrBadName
	}

	for _, r := range n {
		if r == ' ' {
			continue
		}
		// Cc is control characters, Cf is the invisible formatting ones,
		// including the right-to-left overrides that make a name render as
		// something other than what is stored.
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return "", ErrBadName
		}
	}

	if len([]rune(n)) > maxNameRunes || len(n) > maxNameBytes {
		return "", ErrBadName
	}
	return n, nil
}
