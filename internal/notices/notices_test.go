package notices_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/notices"
)

// A notice that could not be delivered must not be buildable.
//
// This is where the guarantee actually lives. The dispatcher refusing a broken
// payload only tells somebody at delivery time, hours after the transaction
// that should have caught it; a constructor that will not return one means the
// producer's own error path runs while there is still a transaction to roll
// back.
func TestANoticeNobodyCouldReceiveIsNotBuilt(t *testing.T) {
	someone, something := uuid.New(), uuid.New()

	cases := map[string]func() error{
		"a cancellation addressed to nobody": func() error {
			_, err := notices.BookingCancelled(notices.BookingCancelledForPassenger, uuid.Nil, something)
			return err
		},
		"a cancellation about no booking": func() error {
			_, err := notices.BookingCancelled(notices.BookingCancelledForDriver, someone, uuid.Nil)
			return err
		},
		"an arrangement notice addressed to nobody": func() error {
			_, err := notices.Arrangement(notices.ArrangementApproved, uuid.Nil, something)
			return err
		},
		"an arrangement notice about no arrangement": func() error {
			_, err := notices.Arrangement(notices.ArrangementRequested, someone, uuid.Nil)
			return err
		},
		"an event that is not a cancellation, built as one": func() error {
			_, err := notices.BookingCancelled(notices.ArrangementApproved, someone, something)
			return err
		},
		"an event nothing has ever heard of": func() error {
			_, err := notices.Arrangement("συνέβη κάτι", someone, something)
			return err
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if err := build(); err == nil {
				t.Fatal("it was built and would have been queued")
			}
		})
	}
}

// The id an event does not use has to be absent from the wire, not present and
// zero: uuid.UUID is an array, omitempty cannot drop it, and an app handed
// "booking_id":"00000000-…" would follow it to a booking nobody ever made.
func TestANoticeCarriesOnlyTheIdItsEventUses(t *testing.T) {
	cancellation, err := notices.BookingCancelled(
		notices.BookingCancelledForPassenger, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("BookingCancelled: %v", err)
	}
	arrangement, err := notices.Arrangement(notices.ArrangementApproved, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Arrangement: %v", err)
	}

	for _, tc := range []struct{ raw, absent string }{
		{string(cancellation.Payload), "arrangement_id"},
		{string(arrangement.Payload), "booking_id"},
	} {
		if strings.Contains(tc.raw, tc.absent) {
			t.Errorf("%s carries a %s", tc.raw, tc.absent)
		}
	}
}

// And what a constructor builds is what the dispatcher reads back, which is the
// half of the contract that was broken: the two sides now share this type
// rather than each having their own idea of the field names.
func TestWhatIsBuiltIsWhatIsRead(t *testing.T) {
	recipient, booking := uuid.New(), uuid.New()
	msg, err := notices.BookingCancelled(notices.BookingCancelledForDriver, recipient, booking)
	if err != nil {
		t.Fatalf("BookingCancelled: %v", err)
	}
	if msg.Kind != "push" {
		t.Errorf("queued as %q", msg.Kind)
	}
	if msg.IdempotencyKey != "booking_cancelled:"+booking.String() {
		t.Errorf("keyed %q", msg.IdempotencyKey)
	}

	var p notices.Payload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		t.Fatalf("unmarshalling what was built: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("what was built does not survive the round trip: %v", err)
	}
	if p.Event != notices.BookingCancelledForDriver || p.RecipientID != recipient {
		t.Errorf("came back as %s to %s", p.Event, p.RecipientID)
	}
	if p.BookingID == nil || *p.BookingID != booking {
		t.Errorf("came back naming %v, want %s", p.BookingID, booking)
	}
}
