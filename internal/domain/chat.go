package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxChatRunes bounds one chat message. It counts characters, not bytes: a
// Greek letter costs two bytes and a byte limit would cut a Greek message in
// half.
const MaxChatRunes = 2000

var (
	// ErrChatEmpty means nothing readable is left once the text is cleaned.
	ErrChatEmpty = errors.New("domain: a message cannot be empty")
	// ErrChatTooLong means the text is over MaxChatRunes.
	ErrChatTooLong = errors.New("domain: that message is too long")
	// ErrChatInvalid means the text holds bytes or control characters a
	// message cannot carry.
	ErrChatInvalid = errors.New("domain: that message holds characters that are not allowed")
)

// NormaliseChat cleans a chat message, or refuses it.
//
// Unlike a name, a message is prose, so line breaks and tabs are kept. What goes is
// what hides or reorders text for a reader: right-to-left overrides and
// isolates and the zero-width characters are stripped, and every other
// control character (NUL included, which the database cannot store) is
// refused. Markup is not touched: it is rendered as plain text.
func NormaliseChat(in string) (string, error) {
	if !utf8.ValidString(in) {
		return "", ErrChatInvalid
	}
	s := norm.NFC.String(strings.ReplaceAll(in, "\r\n", "\n"))

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case isHiddenFormat(r):
			// dropped
		case unicode.Is(unicode.Cc, r):
			return "", ErrChatInvalid
		default:
			b.WriteRune(r)
		}
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", ErrChatEmpty
	}
	if utf8.RuneCountInString(out) > MaxChatRunes {
		return "", ErrChatTooLong
	}
	return out, nil
}

// isHiddenFormat reports the bidi overrides and isolates and the zero-width
// characters.
func isHiddenFormat(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	// Not U+200D: the zero-width joiner is what builds one emoji out of
	// several (a family, a flag), and stripping it splits them.
	case r >= 0x200B && r <= 0x200C:
		return true
	case r == 0xFEFF:
		return true
	}
	return false
}
