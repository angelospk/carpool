package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"carpool/internal/asks"
)

// A drive offer that would lapse within two minutes of being sent cannot be
// answered: the push arrives, the driver unlocks the phone, and it is gone.
// The car leaves at 12:10 and the passenger must be told by 12:09, so at
// 12:07:30 the deadline is ninety seconds away and nobody is asked; at 12:06 it
// is three minutes away and the same offer goes out.
func TestADriveOfferTooShortToAnswerIsNotCreated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	departs := f.clock.Now().Add(10 * time.Minute)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences
		SET scheduled_earliest = depart_earliest, scheduled_latest = depart_latest,
		    depart_earliest = $1, depart_latest = $1, drive_set_at = $2,
		    offers_until = $1::timestamptz + route_min * interval '1 minute'`, departs, f.clock.Now())
	mustExec(t, f, `UPDATE ride_requests SET status = 'open', window_start = $1, window_end = $2`,
		f.clock.Now().Add(-time.Hour), departs.Add(time.Hour))

	f.clock.Set(departs.Add(-150 * time.Second))
	if _, err := f.matcher().AskNext(ctx, f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext with a 90 second deadline: %v, want ErrNoCandidates", err)
	}
	if got := f.asksFor(f.requestID); got != 0 {
		t.Fatalf("%d asks created, want none", got)
	}

	f.clock.Set(departs.Add(-6 * time.Minute))
	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("control: AskNext with minutes to answer: %v", err)
	}
}
