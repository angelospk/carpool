package places

// Turning what somebody typed into something that can be compared.
//
// A person looking for Αλεξανδρούπολη may type it with the accent or without,
// in capitals, or in Latin letters — and if in Latin, there is no single right
// spelling to type. Greek romanisation has an ISO standard, a UN standard, the
// spelling on the road signs and the spelling people actually use, and they
// disagree about the same town. So rather than pick one and be wrong for
// everybody else, each place is indexed under several spellings and the query
// is folded the same way before it is compared.
//
// Two rules keep that from getting out of hand. Ambiguity is only branched
// where Greek really is written more than one way in practice, and the total
// number of spellings per name is capped: every branch doubles the count, so an
// uncapped generator turns a long name into thousands of keys.

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxKeys bounds the spellings generated for one name. Reached only by names
// with many ambiguous letters; the dataset's worst case is far below it.
const maxKeys = 64

// dialytika marks a vowel as read separately from the one before it, which is a
// distinction in the word rather than an accent on it: Καϊρο is not Καιρο. So
// it survives folding, while the tonos does not.
const dialytika = '̈'

// fold reduces text to what two spellings of the same name have in common:
// lower case, no accents, one form of sigma, no punctuation, single spaces.
func fold(s string) string {
	// Decomposed first so that an accent is a separate mark whether the input
	// arrived precomposed or not. The same text reaches us both ways depending
	// on the keyboard and the operating system.
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case r == dialytika:
			b.WriteRune(r)
		case unicode.Is(unicode.Mn, r):
			// Any other combining mark is an accent, and Greek drops accents in
			// capitals anyway, so it cannot be required for matching.
		case r == 'ς':
			// Final sigma is the same letter as σ in a different position.
			b.WriteRune('σ')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			// Hyphens, apostrophes, ano teleia and the rest are separators.
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// A romanisation step: what to match at this position, and the Latin spellings
// it is written as. More than one spelling means the branch is real, not
// theoretical — someone types it both ways.
type step struct {
	greek string
	latin []string
}

// Digraphs come first and are matched in order, because Greek spelling and
// Greek sound part company at exactly these pairs. A letter-for-letter table
// romanises μπ as "mp" and ευ as "eu", which is how nobody writes them.
var digraphs = []step{
	{"ου", []string{"ou", "u"}},
	{"αυ", []string{"av", "af", "au"}},
	{"ευ", []string{"ev", "ef", "eu"}},
	{"ηυ", []string{"iv", "if"}},
	{"μπ", []string{"b", "mp"}},
	{"ντ", []string{"d", "nt"}},
	{"γκ", []string{"g", "gk"}},
	{"γγ", []string{"ng", "gg"}},
	{"γχ", []string{"nch"}},
	{"τσ", []string{"ts"}},
	{"τζ", []string{"tz"}},
	{"αι", []string{"ai", "e"}},
	{"ει", []string{"ei", "i"}},
	{"οι", []string{"oi", "i"}},
}

var letters = map[rune][]string{
	'α': {"a"}, 'β': {"v", "b"}, 'γ': {"g"}, 'δ': {"d"}, 'ε': {"e"},
	'ζ': {"z"}, 'η': {"i", "h"}, 'θ': {"th"}, 'ι': {"i"}, 'κ': {"k"},
	'λ': {"l"}, 'μ': {"m"}, 'ν': {"n"}, 'ξ': {"x", "ks"}, 'ο': {"o"},
	'π': {"p"}, 'ρ': {"r"}, 'σ': {"s"}, 'τ': {"t"}, 'υ': {"y", "i"},
	'φ': {"f"}, 'χ': {"ch", "h"}, 'ψ': {"ps"}, 'ω': {"o"},
}

// latinKeys romanises already-folded Greek text into every spelling worth
// indexing. Text with no Greek in it comes back as itself, so a name already in
// Latin passes through rather than being mangled.
func latinKeys(folded string) []string {
	out := []string{""}
	runes := []rune(folded)

	appendAll := func(suffixes []string) {
		next := make([]string, 0, len(out)*len(suffixes))
		for _, prefix := range out {
			for _, s := range suffixes {
				next = append(next, prefix+s)
			}
		}
		// Capped by dropping the tail rather than refusing to romanise: the
		// spellings are generated left to right, so what survives is still a
		// complete set for the front of the name, which is what a prefix search
		// compares against.
		if len(next) > maxKeys {
			next = next[:maxKeys]
		}
		out = next
	}

	for i := 0; i < len(runes); {
		if i+1 < len(runes) {
			pair := string(runes[i : i+2])
			if d, ok := digraphFor(pair); ok {
				appendAll(d)
				i += 2
				continue
			}
		}
		if l, ok := letters[runes[i]]; ok {
			appendAll(l)
		} else if runes[i] != dialytika {
			// Latin letters, digits and spaces pass through. Dialytika does
			// not: it did its job by blocking the digraph above.
			appendAll([]string{string(runes[i])})
		}
		i++
	}

	return dedupe(out)
}

func digraphFor(pair string) ([]string, bool) {
	for _, d := range digraphs {
		if d.greek == pair {
			return d.latin, true
		}
	}
	return nil, false
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
