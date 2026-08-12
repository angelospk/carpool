package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The repeat endpoint has accepted a weekday and an until_date in its body
// since before the service derived both, and it ignores them.
//
// That is what makes the two obvious nonsense arrangements unreachable: an end
// date already in the past, and a weekday the driver's schedule does not serve.
// Neither is validated anywhere, because neither is an input — the weekday is
// the Athens calendar day of the ride being repeated, and the end date is the
// rolling horizon the materializer keeps.
//
// Asserted through the endpoint rather than the service, because the service
// taking its own values proves nothing about what the door in front of it does
// with a body somebody sends. Somebody tidying up by "wiring those through"
// would add both bugs at once, and this is what fails when they do.
func TestTheRepeatEndpointIgnoresAWeekdayAndAnEndDateSentToIt(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrence := h.bookedTrip(t)

	var bookingID uuid.UUID
	h.mustQuery(`SELECT id FROM bookings WHERE occurrence_id = $1`, &bookingID, occurrence)

	rec := h.do(http.MethodPost, "/v1/bookings/"+bookingID.String()+"/repeat",
		map[string]any{
			// A weekday nothing runs on, and a date that has already been.
			"weekday":    1,
			"until_date": "2020-01-01",
		}, passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var weekday int
	var until time.Time
	h.mustQuery(`SELECT weekday, until_date FROM standing_arrangements
	              WHERE source_booking_id = $1`, &weekday, &until, bookingID)

	var serviceDate time.Time
	h.mustQuery(`SELECT service_date FROM trip_occurrences WHERE id = $1`, &serviceDate, occurrence)
	want := int(serviceDate.Weekday())
	if want == 0 {
		want = 7
	}
	if weekday != want {
		t.Errorf("the arrangement repeats weekday %d, the ride was on %d, the body asked for 1",
			weekday, want)
	}
	if !until.After(h.clock.Now()) {
		t.Errorf("the arrangement ends at %s, which the body chose and the calendar has passed",
			until.Format("2006-01-02"))
	}
}
