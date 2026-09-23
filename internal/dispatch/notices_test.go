package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/dispatch"
	"carpool/internal/notices"
	"carpool/internal/outbox"
)

// Every notice a producer can send has to be one the dispatcher can deliver.
//
// This is the test whose absence let the app go silent. Six places queued a
// cancellation or a standing-arrangement notice; the dispatcher read a payload
// shape none of them wrote, found no recipient in it, and dead-lettered every
// one as permanently undeliverable. Each producer had a test, and each of those
// tests asserted the row had been written to the outbox. Not one of them
// carried the row the rest of the way, so the two halves of the contract drifted
// apart in silence.
//
// The table is deliberately exhaustive and checked against notices.All below:
// adding an event without writing the words somebody reads fails here rather
// than at somebody's locked phone.
func TestEveryNoticeReachesThePersonItNames(t *testing.T) {
	cases := map[notices.Event]struct {
		title string
		body  string
	}{
		notices.BookingCancelledForPassenger: {
			"Η διαδρομή ακυρώθηκε",
			"Η κράτησή σου δεν ισχύει πια. Αν το αίτημά σου ισχύει ακόμα, ψάχνουμε ήδη άλλον οδηγό.",
		},
		notices.BookingCancelledForDriver: {
			"Ακυρώθηκε μια κράτηση",
			"Ένας επιβάτης δεν θα έρθει τελικά, και η θέση είναι πάλι ελεύθερη.",
		},
		notices.ArrangementRequested: {
			"Νέο αίτημα για σταθερή διαδρομή",
			"Κάποιος ζητά να έρχεται μαζί σου τακτικά.",
		},
		notices.ArrangementApproved: {
			"Η σταθερή διαδρομή εγκρίθηκε",
			"Ο οδηγός δέχτηκε να ταξιδεύετε μαζί τακτικά.",
		},
		notices.ArrangementDeclined: {
			"Το αίτημα δεν έγινε δεκτό",
			"Ο οδηγός δεν μπορεί να σε παίρνει τακτικά.",
		},
		notices.ArrangementCancelled: {
			"Η σταθερή διαδρομή σταμάτησε",
			"Οι επόμενες εβδομάδες ακυρώθηκαν.",
		},
	}

	for _, event := range notices.All() {
		if _, ok := cases[event]; !ok {
			t.Fatalf("%s can be sent and nobody wrote what it says", event)
		}
	}
	if len(cases) != len(notices.All()) {
		t.Fatalf("the table has %d events, notices.All has %d", len(cases), len(notices.All()))
	}

	for event, want := range cases {
		t.Run(string(event), func(t *testing.T) {
			pool, snd, recipient := newRouter(t)
			addToken(t, pool, recipient, "phone", false)

			resource := uuid.New()
			msg, err := build(event, recipient, resource)
			if err != nil {
				t.Fatalf("building the notice: %v", err)
			}

			r := dispatch.NewRouter(pool, clk, snd, noAsk)
			if err := r.Handle(context.Background(), msg); err != nil {
				t.Fatalf("Handle: %v", err)
			}

			if len(snd.sent) != 1 {
				t.Fatalf("delivered %d notifications, want exactly one", len(snd.sent))
			}
			got := snd.sent[0]
			if got.Token != "phone" {
				t.Errorf("delivered to %q", got.Token)
			}
			if got.Title != want.title || got.Body != want.body {
				t.Errorf("said %q / %q, want %q / %q", got.Title, got.Body, want.title, want.body)
			}
			// The key travels so the app can ignore a notice it has already
			// acted on, and the resource so it can open the right screen.
			if got.Data["idempotency_key"] != msg.IdempotencyKey {
				t.Errorf("carried key %v, want %q", got.Data["idempotency_key"], msg.IdempotencyKey)
			}
			field, absent := "booking_id", "arrangement_id"
			if event != notices.BookingCancelledForPassenger && event != notices.BookingCancelledForDriver {
				field, absent = absent, field
			}
			if got.Data[field] != resource.String() {
				t.Errorf("carried %s %v, want %s", field, got.Data[field], resource)
			}
			// And nothing else. An id the event does not use has to be absent
			// rather than present and zero, or the app follows it to a booking
			// that was never made.
			if _, ok := got.Data[absent]; ok {
				t.Errorf("carried %s %v, which this event has none of", absent, got.Data[absent])
			}
		})
	}
}

func build(event notices.Event, recipient, resource uuid.UUID) (outbox.Message, error) {
	if event == notices.BookingCancelledForPassenger || event == notices.BookingCancelledForDriver {
		return notices.BookingCancelled(event, recipient, resource)
	}
	return notices.Arrangement(event, recipient, resource)
}

// A notice nothing knows how to say must fail loudly and by name.
//
// Loudly, because the alternative — logging and calling it delivered — is how
// this whole class of bug stays invisible: a version skew between a producer
// and a dispatcher would then look exactly like a quiet day. The dead-lettered
// row keeps its payload, so it can be read and replayed by hand afterwards.
func TestANoticeNobodyCanSayIsRefusedByName(t *testing.T) {
	pool, snd, recipient := newRouter(t)
	addToken(t, pool, recipient, "phone", false)

	raw, err := json.Marshal(map[string]any{
		"event": "somebody_added_this_later", "recipient_id": recipient,
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	r := dispatch.NewRouter(pool, clk, snd, noAsk)
	err = r.Handle(context.Background(), outbox.Message{
		Kind: "push", Payload: raw, IdempotencyKey: "later:1"})

	if !errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("Handle returned %v, want a permanent failure", err)
	}
	if err == nil || !strings.Contains(err.Error(), "somebody_added_this_later") {
		t.Errorf("the failure does not name the event: %v", err)
	}
	if len(snd.sent) != 0 {
		t.Errorf("it was delivered to somebody anyway: %v", snd.sent)
	}
}

// A notice that names an event but nobody to send it to is the failure the old
// code produced for every cancellation. It has to be refused rather than
// guessed at from whatever other ids are lying around in the payload.
func TestANoticeThatNamesNobodyIsRefusedRatherThanGuessedAt(t *testing.T) {
	pool, snd, driverID := newRouter(t)
	addToken(t, pool, driverID, "phone", false)

	// A recipient the JSON simply does not carry, alongside an id that the
	// older, shape-guessing code would happily have delivered on.
	raw, err := json.Marshal(map[string]any{
		"event": notices.BookingCancelledForDriver, "booking_id": uuid.New(),
		"passenger_id": driverID,
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	r := dispatch.NewRouter(pool, clk, snd, noAsk)
	err = r.Handle(context.Background(), outbox.Message{
		Kind: "push", Payload: raw, IdempotencyKey: "nobody:1"})

	if !errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("Handle returned %v, want a permanent failure", err)
	}
	if len(snd.sent) != 0 {
		t.Errorf("it was delivered to somebody anyway: %v", snd.sent)
	}
}

// The two notices that predate the envelope still work, because outbox rows
// written by the version running before a deploy are still in the table when
// the version after it starts reading them.
func TestTheNoticesThatPredateTheEnvelopeStillArrive(t *testing.T) {
	pool, snd, userID := newRouter(t)
	addToken(t, pool, userID, "phone", false)
	r := dispatch.NewRouter(pool, clk, snd, noAsk)

	confirmed, err := json.Marshal(map[string]any{
		"booking_id": uuid.New(), "passenger_id": userID})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, m := range []outbox.Message{
		askMessage(t, userID),
		{Kind: "push", Payload: confirmed, IdempotencyKey: "booking_confirmed:1"},
	} {
		if err := r.Handle(context.Background(), m); err != nil {
			t.Fatalf("Handle %s: %v", m.IdempotencyKey, err)
		}
	}
	if len(snd.sent) != 2 {
		t.Fatalf("delivered %d of the two older notices", len(snd.sent))
	}
	for _, n := range snd.sent {
		if n.Title == "" || n.Body == "" {
			t.Errorf("%q arrived with nothing written on it", n.Data["idempotency_key"])
		}
	}
}
