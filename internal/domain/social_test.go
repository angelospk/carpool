package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormaliseHandleAcceptsHowPeopleWriteIt(t *testing.T) {
	for in, want := range map[string]string{
		"giorgos":      "giorgos",
		"@giorgos":     "giorgos",
		"  @giorgos  ": "giorgos",
		"giorgos.k":    "giorgos.k",
		"giorgos_k":    "giorgos_k",
		"giorgos-k":    "giorgos-k",
		"Giorgos123":   "Giorgos123",
		"":             "",
		"   ":          "",
		"@":            "",
	} {
		got, err := NormaliseHandle(in)
		if err != nil {
			t.Errorf("NormaliseHandle(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseHandleRefusesAnythingThatIsNotAUsername(t *testing.T) {
	// A pasted URL is the case that matters. Taking its last segment looks
	// helpful and is how facebook.com/sharer/sharer.php becomes the handle
	// "sharer.php", which is somebody else's page.
	for _, in := range []string{
		"https://facebook.com/giorgos",
		"facebook.com/giorgos",
		"giorgos/photos",
		"giorgos?ref=1",
		"giorgos#top",
		"giorgos k",
		"γιώργος",
		strings.Repeat("a", 61),
	} {
		if _, err := NormaliseHandle(in); !errors.Is(err, ErrBadHandle) {
			t.Errorf("NormaliseHandle(%q) was accepted", in)
		}
	}
}

func TestTheLinkIsBuiltFromAFixedOrigin(t *testing.T) {
	// The handle is a path segment and nothing more, so nothing typed can move
	// where the link points.
	if got := FacebookURL("giorgos"); got != "https://facebook.com/giorgos" {
		t.Errorf("FacebookURL = %q", got)
	}
	if got := InstagramURL("giorgos"); got != "https://instagram.com/giorgos" {
		t.Errorf("InstagramURL = %q", got)
	}
	for _, f := range []func(string) string{FacebookURL, InstagramURL} {
		if got := f(""); got != "" {
			t.Errorf("an empty handle produced %q rather than no link", got)
		}
	}
}
