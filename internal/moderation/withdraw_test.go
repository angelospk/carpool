package moderation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// What the cascade itself must do, as opposed to what the matcher refuses
// afterwards. The two are tested apart on purpose: the guards in the matcher
// would keep every one of these tests passing with the cascade deleted, and
// then a suspended person's offers would sit on drivers' phones until they
// timed out, holding seats nobody could use.

// liveAsk puts a pending offer in front of the fixture's driver about the
// fixture's passenger, and returns its id.
func (w *world) liveAsk(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var requestID, askID uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        $2::timestamptz, $2::timestamptz + interval '2 hours', 1, 'open', 'asking')
		RETURNING id`, w.passenger, testNow).Scan(&requestID); err != nil {
		t.Fatalf("seeding the request: %v", err)
	}
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', $3::timestamptz)
		RETURNING id`, requestID, w.tomorrow, testNow.Add(time.Hour)).Scan(&askID); err != nil {
		t.Fatalf("seeding the ask: %v", err)
	}
	return askID
}

func (w *world) askStatus(id uuid.UUID) string {
	w.t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM asks WHERE id = $1`, id).Scan(&s); err != nil {
		w.t.Fatalf("reading ask status: %v", err)
	}
	return s
}

func TestSuspendingSomebodyWithdrawsTheOfferOnADriversPhone(t *testing.T) {
	w := newWorld(t)
	askID := w.liveAsk(t)

	// The passenger this time, so the ask being withdrawn is about them rather
	// than addressed to them.
	if err := w.svc.Suspend(context.Background(), w.passenger, w.moderator, "harassment"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	if got := w.askStatus(askID); got != "cancelled" {
		t.Fatalf("the offer is %q; a tap on it is still a seat given to a suspended passenger", got)
	}
}

func TestSuspendingADriverWithdrawsTheOffersTheyWereSittingOn(t *testing.T) {
	w := newWorld(t)
	askID := w.liveAsk(t)

	w.suspend() // the driver

	if got := w.askStatus(askID); got != "cancelled" {
		t.Fatalf("the offer is %q, want cancelled: a suspended driver cannot answer it", got)
	}
}

func TestSuspendingSomebodyTakesTheirRequestsOutOfTheQueue(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.liveAsk(t)

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "harassment"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	var stillLooking int
	if err := w.pool.QueryRow(ctx, `
		SELECT count(*) FROM ride_requests
		WHERE passenger_id = $1 AND status IN ('open','asking')`, w.passenger).Scan(&stillLooking); err != nil {
		t.Fatalf("counting live requests: %v", err)
	}
	if stillLooking != 0 {
		t.Fatalf("%d requests are still looking for a driver, want none", stillLooking)
	}
}

// The seat that offer was holding goes back into the count, and somebody who
// was told the trip was full is asked again.
func TestWithdrawingAnOfferWakesTheTripItWasHolding(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.liveAsk(t)

	// Their booked seats are given back too, and that wakes the same trip. With
	// those out of the way the only thing left that could wake anything is the
	// offer being withdrawn, which is what this is about.
	if _, err := w.pool.Exec(ctx,
		`UPDATE bookings SET status = 'cancelled_by_passenger' WHERE passenger_id = $1`, w.passenger); err != nil {
		t.Fatalf("clearing their bookings: %v", err)
	}

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "harassment"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	var wakes int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`).Scan(&wakes); err != nil {
		t.Fatalf("counting wakes: %v", err)
	}
	if wakes == 0 {
		t.Fatal("no trip was woken: the seat that offer was holding is free and nobody is told")
	}
}

// A weekly agreement books a seat every week without anybody pressing anything.
// Leaving one running is how a suspended person keeps a seat held for months.
func TestSuspendingSomebodyEndsTheirStandingArrangements(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if _, err := w.pool.Exec(ctx, `
		INSERT INTO standing_arrangements (template_id, passenger_id, weekday, seats,
		        pickup_point, dropoff_point, price_eur, until_date, status)
		SELECT t.id, $1, 4, 1,
		       ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		       2.50, '2026-12-31', 'active'
		  FROM route_templates t WHERE t.driver_id = $2`, w.passenger, w.driver); err != nil {
		t.Fatalf("seeding the arrangement: %v", err)
	}

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "harassment"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	var live int
	if err := w.pool.QueryRow(ctx, `
		SELECT count(*) FROM standing_arrangements
		WHERE passenger_id = $1 AND status IN ('pending','active')`, w.passenger).Scan(&live); err != nil {
		t.Fatalf("counting arrangements: %v", err)
	}
	if live != 0 {
		t.Fatalf("%d weekly arrangements survive the suspension, want none", live)
	}
}

// The driver's side of the same agreement.
func TestSuspendingADriverEndsTheArrangementsTheirPassengersHold(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if _, err := w.pool.Exec(ctx, `
		INSERT INTO standing_arrangements (template_id, passenger_id, weekday, seats,
		        pickup_point, dropoff_point, price_eur, until_date, status)
		SELECT t.id, $1, 4, 1,
		       ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		       2.50, '2026-12-31', 'active'
		  FROM route_templates t WHERE t.driver_id = $2`, w.passenger, w.driver); err != nil {
		t.Fatalf("seeding the arrangement: %v", err)
	}

	w.suspend()

	var live int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM standing_arrangements WHERE status IN ('pending','active')`).Scan(&live); err != nil {
		t.Fatalf("counting arrangements: %v", err)
	}
	if live != 0 {
		t.Fatalf("%d weekly arrangements survive a suspended driver, want none", live)
	}
}

// Withdrawing an offer must not strand the person on the other end of it.
//
// A request being put to a driver sits at 'asking' and is waiting on exactly one
// answer. Cancelling that offer because the driver was suspended removes the
// only thing that was ever going to answer it, and nothing else looks at a
// request in that state: the matcher wants 'open', and so does the reconciler
// that exists to catch what everything else missed. The passenger is left on the
// searching screen for ever, with no driver being asked and no way to find out.
func TestWithdrawingAnOfferPutsItsPassengerBackInTheQueue(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	askID := w.liveAsk(t)
	var requestID uuid.UUID
	if err := w.pool.QueryRow(ctx,
		`SELECT request_id FROM asks WHERE id = $1`, askID).Scan(&requestID); err != nil {
		t.Fatalf("finding the request: %v", err)
	}

	w.suspend() // the driver, so the passenger is untouched

	var status string
	if err := w.pool.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1`, requestID).Scan(&status); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if status != "open" {
		t.Fatalf("the passenger's request is %q; nothing looks at one in that state again", status)
	}

	var queued int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&queued); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if queued == 0 {
		t.Fatal("no next driver is queued: the passenger waits for the hourly sweep at best")
	}
}

// The suspended person's own requests stay withdrawn. Reopening those is the
// bug this cascade exists to prevent.
func TestReopeningDoesNotResurrectTheSuspendedPersonsOwnRequest(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.liveAsk(t)

	if err := w.svc.Suspend(ctx, w.passenger, w.moderator, "harassment"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	var live int
	if err := w.pool.QueryRow(ctx, `
		SELECT count(*) FROM ride_requests
		WHERE passenger_id = $1 AND status IN ('open','asking')`, w.passenger).Scan(&live); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if live != 0 {
		t.Fatalf("%d of a suspended person's requests are back in the queue", live)
	}
}
