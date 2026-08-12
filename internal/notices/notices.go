// Package notices is the wire contract for the notifications the app sends:
// what happened, and who is being told.
//
// It exists because that contract was broken and nobody noticed. Six places
// enqueued a push describing an event, the dispatcher inferred the recipient
// from which ids happened to be present in the payload, and the two shapes had
// nothing in common — so every cancellation notice ever queued was
// dead-lettered as "payload names no recipient". Nobody was told their ride had
// been called off, on any of the four paths that call one off. Every producer
// test asserted the row was enqueued; none asserted it could be delivered.
//
// So the payload is built here, by a constructor that refuses to build an
// incomplete one, and read back here by the only thing that decides who gets
// told. A producer cannot hand-roll a shape the dispatcher cannot read, because
// producers no longer hand-roll shapes.
//
// The Greek words a person actually reads are not here. They belong with the
// consumer, in internal/dispatch, so that adding an event and forgetting to say
// anything is a compile-time hole rather than a silent one.
package notices

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"carpool/internal/outbox"
)

// Event names what happened. It is the whole of what decides who is told and
// what they read, which is why it travels in the payload rather than being
// guessed at from the fields around it.
type Event string

const (
	// BookingCancelledForPassenger is a seat that no longer exists, told to the
	// person who was going to sit in it. A driver calling off the departure,
	// their account closing, and their being suspended all reach the passenger
	// as the same fact, and the notice says the fact rather than guessing at
	// the reason.
	BookingCancelledForPassenger Event = "booking_cancelled_for_passenger"
	// BookingCancelledForDriver is the other direction: somebody who had a seat
	// is not coming, and it is free again.
	BookingCancelledForDriver Event = "booking_cancelled_for_driver"

	// ArrangementRequested reaches a driver: somebody wants to travel with them
	// every week rather than once.
	ArrangementRequested Event = "arrangement_requested"
	// ArrangementApproved reaches the passenger who asked.
	ArrangementApproved Event = "arrangement_approved"
	// ArrangementDeclined reaches the passenger who asked.
	ArrangementDeclined Event = "arrangement_declined"
	// ArrangementCancelled reaches whichever of the two did not end it.
	ArrangementCancelled Event = "arrangement_cancelled"
)

// All is every event a producer can emit, and the reason a new one cannot be
// added without the dispatcher's test noticing that nobody wrote its words.
func All() []Event {
	return []Event{
		BookingCancelledForPassenger,
		BookingCancelledForDriver,
		ArrangementRequested,
		ArrangementApproved,
		ArrangementDeclined,
		ArrangementCancelled,
	}
}

// Payload is the wire shape. RecipientID is who is told; the id that follows is
// what they are being told about, and which one is present depends on the
// event.
type Payload struct {
	Event       Event     `json:"event"`
	RecipientID uuid.UUID `json:"recipient_id"`

	// Pointers, so that the one this event does not use is absent from the JSON
	// rather than present and zero. omitempty cannot drop a uuid.UUID — it is an
	// array, and an array of zeroes is not empty — so a value type here would
	// put "booking_id":"00000000-…" on every arrangement notice, and the app
	// would be handed an id to a booking that does not exist.
	BookingID     *uuid.UUID `json:"booking_id,omitempty"`
	ArrangementID *uuid.UUID `json:"arrangement_id,omitempty"`
}

// Validate refuses a payload that could not be delivered to anybody, or that
// names an event nothing knows how to say.
//
// A zero uuid is refused as firmly as a missing field: JSON turns an absent
// recipient into uuid.Nil rather than an error, and a notification addressed to
// nobody is exactly the failure this package exists to stop.
func (p Payload) Validate() error {
	switch p.Event {
	case "":
		return fmt.Errorf("notices: the payload names no event")
	case BookingCancelledForPassenger, BookingCancelledForDriver:
		if p.BookingID == nil || *p.BookingID == uuid.Nil {
			return fmt.Errorf("notices: %s names no booking", p.Event)
		}
	case ArrangementRequested, ArrangementApproved, ArrangementDeclined, ArrangementCancelled:
		if p.ArrangementID == nil || *p.ArrangementID == uuid.Nil {
			return fmt.Errorf("notices: %s names no arrangement", p.Event)
		}
	default:
		return fmt.Errorf("notices: unknown event %q", p.Event)
	}
	if p.RecipientID == uuid.Nil {
		return fmt.Errorf("notices: %s names no recipient", p.Event)
	}
	return nil
}

// BookingCancelled builds the notice for a seat that has gone.
//
// The idempotency key is the booking rather than the booking and the audience,
// because only one side of a booking is ever told: whoever did not do the
// cancelling. A booking cancelled twice is a booking that was already not
// confirmed the second time.
func BookingCancelled(event Event, recipient, booking uuid.UUID) (outbox.Message, error) {
	if event != BookingCancelledForPassenger && event != BookingCancelledForDriver {
		return outbox.Message{}, fmt.Errorf("notices: %q is not a cancellation", event)
	}
	return message(Payload{Event: event, RecipientID: recipient, BookingID: &booking},
		"booking_cancelled:"+booking.String())
}

// Arrangement builds the notice for one of the four things that can happen to a
// standing arrangement.
func Arrangement(event Event, recipient, arrangement uuid.UUID) (outbox.Message, error) {
	return message(Payload{Event: event, RecipientID: recipient, ArrangementID: &arrangement},
		string(event)+":"+arrangement.String())
}

func message(p Payload, key string) (outbox.Message, error) {
	if err := p.Validate(); err != nil {
		return outbox.Message{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return outbox.Message{}, fmt.Errorf("notices: encoding %s: %w", p.Event, err)
	}
	return outbox.Message{Kind: "push", Payload: raw, IdempotencyKey: key}, nil
}
