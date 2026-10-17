package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// QA 2026-10-17 A7 remainder: "ride again" on somebody else's booking answered
// 403 not_yours while a missing booking answered 409 nothing_to_repeat, which
// told a caller holding an id whether it names a booking.
func TestRidingAgainOnSomeoneElsesBookingAnswersLikeAMissingOne(t *testing.T) {
	w := newOwnedWorld(t)

	theirs := w.do(http.MethodPost, "/v1/bookings/"+w.booking.String()+"/again", nil, w.stranger)
	gone := w.do(http.MethodPost, "/v1/bookings/"+uuid.NewString()+"/again", nil, w.stranger)

	if theirs.Code != gone.Code || theirs.Body.String() != gone.Body.String() {
		t.Fatalf("someone else's: %d %s\nmissing: %d %s",
			theirs.Code, theirs.Body.String(), gone.Code, gone.Body.String())
	}
}

// The same probe through "repeat this ride": someone else's booking answered
// 403 not_yours and a missing one 409 not_repeatable.
func TestRepeatingSomeoneElsesBookingAnswersLikeAMissingOne(t *testing.T) {
	w := newOwnedWorld(t)

	theirs := w.do(http.MethodPost, "/v1/bookings/"+w.booking.String()+"/repeat", nil, w.stranger)
	gone := w.do(http.MethodPost, "/v1/bookings/"+uuid.NewString()+"/repeat", nil, w.stranger)

	if theirs.Code != gone.Code || theirs.Body.String() != gone.Body.String() {
		t.Fatalf("someone else's: %d %s\nmissing: %d %s",
			theirs.Code, theirs.Body.String(), gone.Code, gone.Body.String())
	}
}
