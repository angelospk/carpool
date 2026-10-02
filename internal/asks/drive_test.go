package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"carpool/internal/asks"
)

// An offer on a drive is answered against the clock. The car leaves at 12:10
// and passes the passenger at 12:12; they are on the road, so they need only
// the three-minute margin and must be told by 12:09. An answer at 12:09:30
// would leave the car arriving before they could, and the driver waiting.
func TestAcceptOnADriveRefusesAPickupThePassengerCanNoLongerMake(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	departs := f.clock.Now().Add(10 * time.Minute)
	mustExec(t, f, `UPDATE trip_occurrences
		SET scheduled_earliest = depart_earliest, scheduled_latest = depart_latest,
		    depart_earliest = $1, depart_latest = $1, drive_set_at = $2`, departs, f.clock.Now())
	mustExec(t, f, `UPDATE ride_requests SET window_start = $1, window_end = $2`,
		f.clock.Now().Add(-time.Hour), departs.Add(time.Hour))
	mustExec(t, f, `UPDATE asks SET expires_at = $1 WHERE id = $2`, departs, f.askID)

	f.clock.Set(departs.Add(-30 * time.Second))
	seats := f.seatsFree()
	if _, err := f.service().Accept(ctx, f.askID, f.driverID); !errors.Is(err, asks.ErrAskExpired) {
		t.Fatalf("Accept at 12:09:30: %v, want ErrAskExpired", err)
	}
	if got := f.seatsFree(); got != seats {
		t.Fatalf("seats went from %d to %d", seats, got)
	}

	f.clock.Set(departs.Add(-2 * time.Minute))
	if _, err := f.service().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("control: Accept at 12:08 with time to reach the road: %v", err)
	}
}
