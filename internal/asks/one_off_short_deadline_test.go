package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"carpool/internal/asks"
)

// A one-off that is not pinned as a drive is capped by its departure too: a
// «Φεύγω τώρα» route published for the next quarter of an hour would otherwise
// get offers that end seconds after they are sent. Leaving in a minute nobody
// is asked; leaving in ten the same offer goes out.
func TestAOneOffOfferTooShortToAnswerIsNotCreated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := f.clock.Now()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open', window_start = $1, window_end = $2`,
		now.Add(-time.Hour), now.Add(3*time.Hour))

	mustExec(t, f, `UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		now.Add(time.Minute), now.Add(15*time.Minute))
	if _, err := f.matcher().AskNext(ctx, f.requestID); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext for a trip leaving in 60 seconds: %v, want ErrNoCandidates", err)
	}
	if got := f.asksFor(f.requestID); got != 0 {
		t.Fatalf("%d asks created, want none", got)
	}

	mustExec(t, f, `UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		now.Add(10*time.Minute), now.Add(25*time.Minute))
	if _, err := f.matcher().AskNext(ctx, f.requestID); err != nil {
		t.Fatalf("control: AskNext for a trip leaving in 10 minutes: %v", err)
	}
}
