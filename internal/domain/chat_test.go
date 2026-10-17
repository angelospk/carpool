package domain_test

import (
	"errors"
	"strings"
	"testing"

	"carpool/internal/domain"
)

func TestNormaliseChat(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		err  error
	}{
		{"plain", "  Καλημέρα!  ", "Καλημέρα!", nil},
		{"newline kept", "a\nb", "a\nb", nil},
		{"crlf becomes newline", "a\r\nb", "a\nb", nil},
		{"nfc", "e\u0301", "é", nil},
		{"markup untouched", "<b>hi</b>", "<b>hi</b>", nil},
		{"bidi override stripped", "abc\u202e gnirts", "abc gnirts", nil},
		{"bidi isolate stripped", "a\u2066b\u2069c", "abc", nil},
		{"zero width stripped", "a\u200bb\u200cc\ufeffd", "abcd", nil},
		// The joiner is what builds 👨‍👩‍👧 out of three emoji; it stays.
		{"emoji joiner kept", "\U0001F468\u200d\U0001F469", "\U0001F468\u200d\U0001F469", nil},
		{"empty", "", "", domain.ErrChatEmpty},
		{"blank", " \n ", "", domain.ErrChatEmpty},
		{"only invisible", "\u200b\u202e", "", domain.ErrChatEmpty},
		{"nul", "a\x00b", "", domain.ErrChatInvalid},
		{"tab kept", "a\tb", "a\tb", nil},
		{"escape", "a\x1bb", "", domain.ErrChatInvalid},
		{"lone cr", "a\rb", "", domain.ErrChatInvalid},
		{"invalid utf8", "a\xffb", "", domain.ErrChatInvalid},
		{"2000 greek runes ok", strings.Repeat("ω", 2000), strings.Repeat("ω", 2000), nil},
		{"2001 runes too long", strings.Repeat("ω", 2001), "", domain.ErrChatTooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.NormaliseChat(tc.in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
