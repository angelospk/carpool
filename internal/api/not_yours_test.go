package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// QA 2026-10-17 finding 7. Somebody else's ask or booking has to answer exactly
// like one that does not exist: same status, same code, same message. Anything
// else tells a caller holding an id whether it names something.
func TestSomeoneElsesAskOrBookingAnswersLikeAMissingOne(t *testing.T) {
	w := newOwnedWorld(t)

	cases := []struct {
		name, method, real, missing string
	}{
		{"accept", http.MethodPost, "/v1/asks/" + w.pendingAsk.String() + "/accept", "/v1/asks/" + uuid.NewString() + "/accept"},
		{"decline", http.MethodPost, "/v1/asks/" + w.pendingAsk.String() + "/decline", "/v1/asks/" + uuid.NewString() + "/decline"},
		{"cancel booking", http.MethodDelete, "/v1/bookings/" + w.booking.String(), "/v1/bookings/" + uuid.NewString()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			theirs := w.do(tc.method, tc.real, nil, w.stranger)
			gone := w.do(tc.method, tc.missing, nil, w.stranger)
			if theirs.Code != gone.Code || theirs.Body.String() != gone.Body.String() {
				t.Fatalf("someone else's: %d %s\nmissing: %d %s",
					theirs.Code, theirs.Body.String(), gone.Code, gone.Body.String())
			}
			if gone.Code != http.StatusNotFound {
				t.Fatalf("status %d, want 404: %s", gone.Code, gone.Body.String())
			}
		})
	}
}
