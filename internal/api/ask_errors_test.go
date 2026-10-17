package api

import (
	"errors"
	"net/http"
	"testing"

	"carpool/internal/asks"
	"carpool/internal/match"
)

// Every way answering an ask can fail has to reach the driver as itself.
//
// Written as one table rather than a case per error because the failure it
// guards against is an error added to the lifecycle and not added to the map,
// and a case per error is the thing whoever adds one forgets to write. What
// falls through the switch is a 500 saying "something went wrong", which is
// both untrue and unactionable: the boot is full, or the car has gone, and the
// driver can do something about either if they are told which.
//
// ErrNoLuggageRoom was reached this way. Its own declaration says it is a
// separate conversation from a full car — the driver may well take somebody
// with one bag instead of three — and it was arriving as an internal error.
func TestEveryAskFailureReachesTheDriverAsItself(t *testing.T) {
	seen := map[string]error{}
	for _, err := range []error{
		asks.ErrNotYours,
		asks.ErrAskExpired,
		asks.ErrAskNotPending,
		asks.ErrNoSeats,
		asks.ErrNoLuggageRoom,
		asks.ErrStaleItinerary,
		asks.ErrTripDeparted,
		match.ErrPartyCapReached,
		asks.ErrBusy,
	} {
		t.Run(err.Error(), func(t *testing.T) {
			status, code, message := askErrorStatus(err)
			if status == http.StatusInternalServerError {
				t.Fatalf("%v arrives as a 500 %q: %s", err, code, message)
			}
			if code == "" || message == "" {
				t.Fatalf("%v arrives as %d with code %q and message %q", err, status, code, message)
			}
			// Distinct, not merely non-500. Two failures sharing a code is the
			// same loss of meaning one step further along: the app cannot show
			// a different message for something it cannot tell apart.
			if other, ok := seen[code]; ok {
				t.Fatalf("%v and %v both arrive as %q", other, err, code)
			}
			seen[code] = err
		})
	}
}

// And the default is still there for what it is for.
func TestAnUnknownFailureIsStillInternal(t *testing.T) {
	status, code, _ := askErrorStatus(errors.New("the database fell over"))
	if status != http.StatusInternalServerError || code != "internal" {
		t.Fatalf("an unrecognised failure arrives as %d %q", status, code)
	}
}
