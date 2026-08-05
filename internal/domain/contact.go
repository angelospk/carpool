package domain

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Contact details in text that strangers read.
//
// The rule GoRide states is about listings: "η χρήση υπερσυνδέσμου, email και
// ονόματα που παραπέμπουν σε προφίλ πέρα από το GoRide... απαγορεύονται". Ours
// is about safety first. A bio is shown to people who have not met the person
// who wrote it, and a phone number in one is a public phone number — handed not
// to the passenger they agreed to travel with but to everybody who scrolled
// past.
//
// There is somewhere legitimate for this: facebook_handle and instagram_handle
// are separate, structured, and deliberately shown. The difference is not
// "off-platform contact bad" — it is between an identity somebody chose to
// publish and a number buried in prose where it will not be noticed until it is
// too late.
//
// This will never be evasion-proof and is not trying to be. Somebody determined
// to publish their number can spell it out in words, and the person reading it
// still had to work to find it. What this stops is the ordinary case: somebody
// who does not think of a bio as public typing their number into it because it
// seemed helpful.

// ContactKind names what was found, so the app can say which thing to remove.
type ContactKind string

const (
	ContactNone   ContactKind = ""
	ContactEmail  ContactKind = "email"
	ContactPhone  ContactKind = "phone"
	ContactLink   ContactKind = "link"
	ContactHandle ContactKind = "handle"
)

// maxContactScanBytes bounds the work. Bios are limited to MaxBioRunes long
// before this is reached; the bound is here so that a caller who forgets cannot
// hand a megabyte to a regexp engine.
const maxContactScanBytes = 8 << 10

// HasContactDetails reports whether the text hands out a way to reach somebody
// off the app, and what kind.
//
// The text itself is never modified. Detection runs over a separate normalised
// copy, and what gets stored and shown is what the person actually wrote.
func HasContactDetails(text string) (bool, ContactKind) {
	if len(text) > maxContactScanBytes {
		text = text[:maxContactScanBytes]
	}
	folded := foldForDetection(text)
	if folded == "" {
		return false, ContactNone
	}

	// Order matters only for which name comes back. "giannis at gmail dot com"
	// is an email before it is anything else, so emails are asked first.
	for _, c := range detectors {
		if c.re.MatchString(folded) {
			return true, c.kind
		}
	}
	return false, ContactNone
}

// foldForDetection makes one canonical string to match against.
//
// NFKC first, which turns full-width ６９４ into 694 and a good deal else.
// Then format characters go — zero-width joiners and non-joiners are invisible
// in a bio and split a phone number into pieces no pattern matches. Then case,
// and then the several characters that stand in for a full stop.
//
// Greek accents are deliberately NOT stripped, and Greek letters are
// deliberately NOT mapped onto lookalike digits. Mapping ο→0 and ι→1 would find
// a handful of obfuscated numbers and would flag a great deal of ordinary Greek,
// which is the trade that gets a filter switched off.
func foldForDetection(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	for _, r := range norm.NFKC.String(text) {
		switch {
		case unicode.Is(unicode.Cf, r):
			// Zero-width and other format characters: dropped, not replaced, so
			// that what they were splitting joins back up.
			continue
		case r == '․' || r == '。' || r == '．':
			b.WriteRune('.')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

var detectors = []struct {
	kind ContactKind
	re   *regexp.Regexp
}{
	{ContactEmail, regexp.MustCompile(
		// name@host.tld, and the ways people write the two symbols out.
		`[\p{L}\p{N}._%+-]+\s*(?:@|\(at\)|\[at\]|\sat\s|παπάκι)\s*[\p{L}\p{N}.-]+\s*(?:\.|\(dot\)|\[dot\]|\sdot\s|τελεία)\s*\p{L}{2,}`)},

	{ContactPhone, regexp.MustCompile(
		// A Greek mobile: 69 and eight more, however it is spaced or punctuated,
		// with or without a country prefix. Anchored on the 69 rather than on
		// "ten digits", so years, prices and distances are not swept up.
		`(?:\+?30|0030)?[\s.\-()]*6\s*9[\s.\-()]*(?:\d[\s.\-()]*){8}`)},

	{ContactLink, regexp.MustCompile(
		// A scheme, a www, or a bare host with a plausible top level domain.
		// The tld list is closed on purpose: an open \w{2,} matches "στο.τέλος".
		// .ελ is Greece's own script top level domain and belongs beside .gr.
		// The delimiters are written out rather than using \b, which in Go is
		// ASCII-only: it would put a boundary in the middle of "γιάννης" and
		// none at the edge of a Greek host.
		`(?:https?://|www\.)\S+|(?:^|[^\p{L}\p{N}.-])[\p{L}\p{N}-]+\.(?:gr|ελ|com|net|org|io|me|eu|info|co)(?:$|[^\p{L}\p{N}-])`)},

	{ContactHandle, regexp.MustCompile(
		// A platform named next to something that could be an account, or an
		// @handle. "Χρησιμοποιώ Gmail" is not a handle, so the platform names
		// here are the ones people use to be *reached* at.
		`@[\p{L}\p{N}._]{3,}|\b(?:instagram|insta|facebook|fb|telegram|viber|whatsapp|tiktok|snapchat|signal)\b`)},
}
