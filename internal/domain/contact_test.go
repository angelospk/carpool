package domain

import "testing"

// The refusals. Every one of these is somebody's phone number, address or
// account handed to strangers in a field that is read by strangers.
func TestContactDetailsAreFound(t *testing.T) {
	for _, tt := range []struct {
		in   string
		kind ContactKind
	}{
		// Plainly written.
		{"γράψε μου στο giannis@gmail.com", ContactEmail},
		{"GIANNIS@GMAIL.COM", ContactEmail},
		{"δες https://example.gr/me", ContactLink},
		{"www.giannis.gr", ContactLink},
		{"το site μου είναι giannis.gr", ContactLink},
		{"πάρε με 6941234567", ContactPhone},
		{"+30 694 123 4567", ContactPhone},
		{"0030-694-123-4567", ContactPhone},
		{"694.123.4567", ContactPhone},
		{"τηλ (694) 1234567", ContactPhone},

		// Written around the filter.
		{"giannis at gmail dot com", ContactEmail},
		{"giannis (at) gmail (dot) com", ContactEmail},
		{"giannis παπάκι gmail τελεία com", ContactEmail},
		{"giannis [at] gmail [dot] gr", ContactEmail},
		{"instagram: giannis_1990", ContactHandle},
		{"insta giannis1990", ContactHandle},
		{"βρες με στο viber", ContactHandle},
		{"whatsapp μου", ContactHandle},
		{"@giannis1990", ContactHandle},

		// Greek-script domains. .ελ is a real top level domain and the word
		// boundaries around a host have to work for Greek letters too, which
		// Go's \b does not: it is ASCII-only.
		{"γιάννης@ταχυδρομείο.ελ", ContactEmail},
		{"δες το γιάννης.ελ", ContactLink},

		// Digits that are not ASCII digits.
		{"６９４１２３４５６７", ContactPhone},

		// Zero-width characters wedged into the middle. Written as escapes
		// rather than as the characters themselves: pasted literally they are
		// invisible in the source, so the two lines below looked like ordinary
		// duplicates of cases above and the point of the test was unreadable.
		// This is also what ST1018 asks for, which is how the file stopped
		// being the reason `make check` failed.
		{"giannis@gm\u200bail.com", ContactEmail},
		{"694\u200d1234567", ContactPhone},
	} {
		got, kind := HasContactDetails(tt.in)
		if !got {
			t.Errorf("HasContactDetails(%q) = false, want true (%s)", tt.in, tt.kind)
			continue
		}
		if kind != tt.kind {
			t.Errorf("HasContactDetails(%q) said %s, want %s", tt.in, kind, tt.kind)
		}
	}
}

// The other half, and the harder half. A filter that eats ordinary Greek prose
// gets switched off, and then it protects nobody.
func TestOrdinaryProseIsLeftAlone(t *testing.T) {
	for _, in := range []string{
		"",
		"Οδηγώ από το 2010, μου αρέσει η κουβέντα και η μουσική.",
		"Γεννήθηκα το 1990 στην Αλεξανδρούπολη.",
		"Φεύγω κάθε Δευτέρα στις 6 και επιστρέφω Παρασκευή στις 4.",
		"Έχω 2 παιδιά και 1 σκύλο.",
		"Το ταξίδι κοστίζει περίπου 12 ευρώ.",
		"Κάνω τη διαδρομή 6 φορές τον μήνα, 694 χιλιόμετρα συνολικά.",
		"Δουλεύω στο Πάτρα - Αθήνα, 210 χλμ.",
		"Χρησιμοποιώ Gmail για τη δουλειά μου.",
		"Με λένε Ματθαίο και είμαι από τη Δράμα.",
		"Είμαι φοιτητής, σπουδάζω στο ΤΕΙ.",
		"Ήμουν στο εξωτερικό 6 χρόνια, γύρισα το 2019.",
		// A name that happens to contain the letters this filter looks for.
		"Ο Δημήτρης Ατματζίδης, φίλος μου, ταξιδεύει μαζί μου.",
		"Κάθε Σάββατο στις 09:00 ακριβώς.",
	} {
		if got, kind := HasContactDetails(in); got {
			t.Errorf("HasContactDetails(%q) = true (%s), want false", in, kind)
		}
	}
}

// A bio is not stored in whatever mangled form the detector needed to read it.
// The person wrote what they wrote.
func TestTheTextIsNotRewrittenByBeingChecked(t *testing.T) {
	const bio = "Οδηγώ  από  το 2010."
	before := bio

	HasContactDetails(bio)

	if bio != before {
		t.Error("the input changed")
	}
	got, err := NormaliseBio(bio)
	if err != nil {
		t.Fatalf("NormaliseBio: %v", err)
	}
	if got != "Οδηγώ  από  το 2010." {
		t.Errorf("NormaliseBio rewrote the text: %q", got)
	}
}

// A pathological input must not take the process with it. The bound is the bio
// length limit, applied before any of this runs.
func TestALongInputIsBounded(t *testing.T) {
	long := make([]byte, 200_000)
	for i := range long {
		long[i] = 'a'
	}
	// Not a hang, and not a match.
	if got, _ := HasContactDetails(string(long)); got {
		t.Error("a wall of letters looked like contact details")
	}
}
