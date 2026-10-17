package bookings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// A driver calling off a departure has to hand its passengers back to the
// matcher, not just tell them it is off.
//
// Cancelling a departure cancels its bookings and sends each passenger a push,
// and there it stopped. Their ride_request stays at 'matched' for ever: nothing
// reads that state looking for work, so the request that already says where
// they are going, when, with how many seats and how much luggage is never shown
// to another driver. The app told them their ride was cancelled and then did
// nothing, while a driver on the same corridor an hour later was never asked.
//
// This is the same hole that was fixed for a suspension, one layer down. It is
// worth fixing at both because a driver calling off their own trip is the
// ordinary case and a suspension is the rare one.
//
// Reopening is what they already agreed to: a request is standing permission to
// be matched inside its window, which is how it got its first driver.

func requestFor(t *testing.T, w *world, bookingID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.pool.QueryRow(context.Background(),
		`SELECT request_id FROM bookings WHERE id = $1`, bookingID).Scan(&id); err != nil {
		t.Fatalf("finding the request behind the booking: %v", err)
	}
	return id
}

// reopensQueuedFor counts the recovery events recorded for a request.
//
// The cancelling transaction records that a seat was lost; the dispatcher does
// the reopening and the asking, where the passenger's rows can be taken in the
// ordinary order. So this is what the cancellation itself is answerable for.
// What the dispatcher then does with the event is asserted where it happens,
// against every state the passenger could have moved to in between.
func reopensQueuedFor(t *testing.T, w *world, requestID uuid.UUID) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox
		WHERE kind = 'reopen_request' AND payload->>'request_id' = $1::text`, requestID).Scan(&n); err != nil {
		t.Fatalf("counting queued reopens: %v", err)
	}
	return n
}

func TestCallingOffADepartureLooksForAnotherDriverForItsPassengers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if err := w.svc.CancelOccurrence(ctx, w.occurrenceID, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	if n := reopensQueuedFor(t, w, requestID); n != 1 {
		t.Fatalf("%d recovery events for a passenger whose ride was called off, want 1", n)
	}
}

// A booking made from a standing arrangement has no request behind it: the
// arrangement is the standing permission, and it survives one cancelled week.
// Nothing to reopen, and nothing that falls over trying.
func TestCallingOffADepartureLeavesAStandingArrangementAlone(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	if err := w.svc.CancelOccurrence(ctx, w.secondOccurrence, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	// Asserted before the interesting part, because "no ask was queued" is also
	// what a departure nobody cancelled would look like.
	var status string
	if err := w.pool.QueryRow(ctx,
		`SELECT status FROM bookings WHERE id = $1`, w.fromArrangement).Scan(&status); err != nil {
		t.Fatalf("reading the booking: %v", err)
	}
	if status != "cancelled_by_driver" {
		t.Fatalf("the arrangement's seat is %q, want cancelled_by_driver", status)
	}

	var queued int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'reopen_request'`).Scan(&queued); err != nil {
		t.Fatalf("counting queued reopens: %v", err)
	}
	if queued != 0 {
		t.Fatalf("%d recovery events for a booking that never came from a request", queued)
	}
}

// Being stranded twice has to ask twice.
//
// A passenger whose driver cancels is put back in the queue and can be matched
// again, and that second driver can cancel too. The queued ask carries an
// idempotency key, so if the key names only the request the second stranding
// looks like a repeat of the first and is dropped — and that passenger waits
// for an ask nobody was ever going to send. The key names the seat that was
// lost, which is a different one each time.
func TestBeingStrandedTwiceAsksTwice(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if err := w.svc.CancelOccurrence(ctx, w.occurrenceID, w.driverID); err != nil {
		t.Fatalf("first CancelOccurrence: %v", err)
	}

	// Matched again, to a later departure of the same driver's, exactly as the
	// queued ask would have done.
	var third uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode)
		SELECT o.template_id, o.vehicle_id, DATE '2026-08-18', '2026-08-18T06:30:00Z',
		       '2026-08-18T07:00:00Z', 3, 2, 1, o.route_geom, o.route_km, o.route_min, 'formula'
		  FROM trip_occurrences o WHERE o.id = $1
		RETURNING id`, w.occurrenceID).Scan(&third); err != nil {
		t.Fatalf("seeding the later departure: %v", err)
	}

	var askID, secondBooking uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted', $3::timestamptz)
		RETURNING id`, requestID, third, testNow).Scan(&askID); err != nil {
		t.Fatalf("seeding the second ask: %v", err)
	}
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
		                      pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
		RETURNING id`, third, requestID, w.passengerID, askID).Scan(&secondBooking); err != nil {
		t.Fatalf("seeding the second booking: %v", err)
	}
	if _, err := w.pool.Exec(ctx,
		`UPDATE ride_requests SET status = 'matched' WHERE id = $1`, requestID); err != nil {
		t.Fatalf("rematching: %v", err)
	}

	if err := w.svc.CancelOccurrence(ctx, third, w.driverID); err != nil {
		t.Fatalf("second CancelOccurrence: %v", err)
	}

	if n := reopensQueuedFor(t, w, requestID); n != 2 {
		t.Fatalf("%d recovery events for somebody stranded twice, want 2", n)
	}
}

// The same promise when the driver takes back one seat rather than calling off
// the whole departure.
//
// Only CancelOccurrence handed its passengers back to the matcher. A driver who
// told one passenger "not today" through Cancel left that passenger's request
// at 'matched' for ever, with the same push and nothing behind it — the
// guaranteed ride home kept for a cancelled car and broken for a cancelled seat.
func TestADriverCancellingOneSeatLooksForAnotherDriverForThatPassenger(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if err := w.svc.Cancel(ctx, w.fromAsk, w.driverID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if n := reopensQueuedFor(t, w, requestID); n != 1 {
		t.Fatalf("%d recovery events for a seat the driver took back, want 1", n)
	}

	// Asking twice is the same act and asks once.
	if err := w.svc.Cancel(ctx, w.fromAsk, w.driverID); err != nil {
		t.Fatalf("second Cancel: %v", err)
	}
	if n := reopensQueuedFor(t, w, requestID); n != 1 {
		t.Fatalf("%d recovery events after cancelling twice, want 1", n)
	}
}

// A passenger giving their own seat back has changed their mind, and finding
// them another driver would be doing the opposite of what they asked.
func TestAPassengerCancellingTheirOwnSeatIsNotLookedAfter(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	requestID := requestFor(t, w, w.fromAsk)

	if err := w.svc.Cancel(ctx, w.fromAsk, w.passengerID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if n := reopensQueuedFor(t, w, requestID); n != 0 {
		t.Fatalf("%d recovery events for a passenger who cancelled themselves", n)
	}
}

// A seat from a standing arrangement has no request to reopen.
func TestADriverCancellingAnArrangedSeatQueuesNothing(t *testing.T) {
	w := newWorld(t)
	if err := w.svc.Cancel(context.Background(), w.fromArrangement, w.driverID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	var queued int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'reopen_request'`).Scan(&queued); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if queued != 0 {
		t.Fatalf("%d recovery events for an arranged seat", queued)
	}
}

// QA 2026-10-17 F2: an offer still waiting for the driver's answer on a
// departure the driver calls off. It used to stay pending on a cancelled trip
// for half an hour, holding its passenger (one live offer per request) while
// another car could have taken them.
func TestCallingOffADepartureWithdrawsItsPendingOffers(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var other, requestID, askID uuid.UUID
	scanRow := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := w.pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	scanRow(&other, `INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', '+306900000009', 'Τρίτος',
		        (SELECT home_region_id FROM users WHERE id = $1)) RETURNING id`, w.passengerID)
	scanRow(&requestID, `INSERT INTO ride_requests (passenger_id, origin, destination,
		window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		'2026-08-04T03:00:00Z','2026-08-04T07:00:00Z', 1, 'open', 'asking') RETURNING id`, other)
	scanRow(&askID, `INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		pickup_point, dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', '2026-08-04T06:30:00Z')
		RETURNING id`, requestID, w.occurrenceID)

	if err := w.svc.CancelOccurrence(ctx, w.occurrenceID, w.driverID); err != nil {
		t.Fatalf("CancelOccurrence: %v", err)
	}

	var askState, requestState string
	var asked int
	if err := w.pool.QueryRow(ctx, `
		SELECT (SELECT status FROM asks WHERE id = $1),
		       (SELECT status FROM ride_requests WHERE id = $2),
		       (SELECT count(*) FROM outbox WHERE kind = 'ask_next' AND payload->>'request_id' = $2::text)`,
		askID, requestID).Scan(&askState, &requestState, &asked); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if askState != "cancelled" {
		t.Errorf("offer on the cancelled departure = %q, want cancelled", askState)
	}
	if requestState != "open" {
		t.Errorf("its request = %q, want open", requestState)
	}
	if asked != 1 {
		t.Errorf("%d ask_next queued for its passenger, want 1", asked)
	}
}
