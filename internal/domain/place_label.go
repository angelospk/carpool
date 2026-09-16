package domain

import (
	"errors"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ErrBadPlaceLabel is returned when a place label cannot be stored.
var ErrBadPlaceLabel = errors.New("that place name cannot be used")

// A label is a settlement and sometimes a landmark — "Φέρες, ΚΤΕΛ Έβρου" — so
// the limit has to hold the longest honest version of that and nothing like a
// paragraph. Runes rather than bytes, because Greek costs two bytes a letter
// and a byte limit would quietly give Greek half the room.
const (
	maxPlaceLabelRunes = 120
	maxPlaceLabelBytes = 600
)

// NormalisePlaceLabel cleans up the name a client gave a pin, or refuses it.
//
// It exists because nothing checked these strings. They arrive from the app,
// they are stored on requests and route templates, and they are read back by
// people who were not there: on a trip card, and — since the demand endpoint —
// aggregated into a line put in front of every driver in the area. The only
// ceiling they had was the 64 KiB request body, which is not a place name.
//
// The rules are the display name's rules, for the same reasons written out in
// NormaliseName: no letter allowlist, because the region's names are Greek,
// Turkish, Bulgarian and Pomak as well as Latin, and what is blocked instead is
// what breaks a screen or hides text — control characters and the invisible
// formatting runes that let a string render as something other than what is
// stored.
//
// Whitespace is tidied rather than refused, which the display name does too,
// and here it earns its keep twice: the demand count groups by the label, so
// "Κομοτηνή " and "Κομοτηνή" being two strings would be two corridors and two
// halves of one number.
func NormalisePlaceLabel(in string) (string, error) {
	// NFC first, so the same name typed on two keyboards is one string and the
	// length limit means the same thing for both.
	n := norm.NFC.String(in)
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return "", ErrBadPlaceLabel
	}

	for _, r := range n {
		if r == ' ' {
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return "", ErrBadPlaceLabel
		}
	}

	if len([]rune(n)) > maxPlaceLabelRunes || len(n) > maxPlaceLabelBytes {
		return "", ErrBadPlaceLabel
	}
	return n, nil
}
