package asks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/drives"
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
		    depart_earliest = $1, depart_latest = $1, drive_set_at = $2,
		    offers_until = $1::timestamptz + route_min * interval '1 minute'`, departs, f.clock.Now())
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

// The whole path for a driver who taps «Τώρα»: the pinned minute is already
// this minute, so everything that used to stop at the departure has to keep
// going for a drive. Somebody halfway along the road, whom the car passes ten
// minutes from now, is offered the seat with an offer that ends when they
// would have to set off, and the driver can accept it after the car has left.
// Somebody two minutes down the road is not offered it at all: they could not
// get to the road in time.
func TestADriveLeavingNowIsOfferedAndAcceptedOnTheRoad(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := f.clock.Now()

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		now.Add(5*time.Minute), now.Add(35*time.Minute))
	mustExec(t, f, `UPDATE ride_requests
		SET status = 'open', origin = ST_SetSRID(ST_MakePoint(25.90, 40.85), 4326),
		    window_start = $1, window_end = $2`, now.Add(-time.Hour), now.Add(2*time.Hour))

	if _, err := drives.NewService(f.pool, f.clock).Start(ctx, f.driverID, f.occurrenceID,
		drives.Start{DepartsAt: now}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	f.clock.Advance(30 * time.Second) // the car has left

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext after the car left: %v", err)
	}
	var expires time.Time
	must(t, f.pool.QueryRow(ctx, `SELECT expires_at FROM asks WHERE id = $1`, askID).Scan(&expires), "expiry")
	if want := now.Add(7 * time.Minute); expires.Sub(want).Abs() > 5*time.Second {
		t.Fatalf("the offer ends at %v, want about %v: ten minutes away, less the margin", expires, want)
	}

	f.clock.Advance(4 * time.Minute)
	if _, err := f.service().Accept(ctx, askID, f.driverID); err != nil {
		t.Fatalf("Accept on the road: %v", err)
	}

	// Two minutes down the road is behind the margin already.
	_, nearID := f.newAsk("", f.passengerID)
	mustExec(t, f, `DELETE FROM asks WHERE id = $1`, nearID)
	var nearRequest uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		UPDATE ride_requests SET status = 'open', window_start = $1, window_end = $2
		 WHERE id <> $3 RETURNING id`, now.Add(-time.Hour), now.Add(2*time.Hour), f.requestID).Scan(&nearRequest), "near request")
	if _, err := f.matcher().AskNext(ctx, nearRequest); !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("AskNext for somebody the car is about to pass: %v, want ErrNoCandidates", err)
	}
}
