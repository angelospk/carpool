package dispatch_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/dispatch"
	"carpool/internal/outbox"
	"carpool/internal/store/testdb"
)

// The recovery of a passenger whose seat was cancelled out from under them.
//
// The transaction that cancels the seat records what happened and commits; the
// reopening happens here. That split is what keeps a suspension, a closure and
// a driver calling off a departure to one transaction each without any of them
// locking rows belonging to every passenger they strand.
//
// The price of the split is that time passes between the two, so everything the
// producer knew has to be read again. These are the states the passenger can be
// in by the time this runs. Each fixture differs from the one that works in
// exactly one respect, so a check removed from the handler shows up as one of
// them reopening a request that should have been left alone.

type strandedWorld struct {
	pool                 *pgxpool.Pool
	passengerID          uuid.UUID
	requestID, bookingID uuid.UUID
	occurrenceID         uuid.UUID
	router               *dispatch.Router
}

func newStrandedWorld(t *testing.T) *strandedWorld {
	t.Helper()
	ctx := context.Background()
	pool, snd, driverID := newRouter(t)
	w := &strandedWorld{pool: pool, router: dispatch.NewRouter(pool, clk, snd, noAsk)}

	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	scan(&w.passengerID, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		SELECT DATE '1990-01-01', '+306900000055', 'Επιβάτης', home_region_id FROM users WHERE id = $1
		RETURNING id`, driverID)
	if _, err := pool.Exec(ctx, `INSERT INTO driver_profiles (user_id, car_model, car_plate)
		VALUES ($1, 'Yaris','ΑΒΓ-1234')`, driverID); err != nil {
		t.Fatalf("seeding driver profile: %v", err)
	}
	testdb.GiveEveryDriverACar(t, pool)

	var templateID uuid.UUID
	scan(&templateID, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label,
			destination_label, route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, driverID)
	scan(&w.occurrenceID, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode, status)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), DATE '2026-07-30',
			'2026-07-30T06:30:00Z', '2026-07-30T07:00:00Z', 3, 2, 1,
			ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula', 'cancelled')
		RETURNING id`, templateID)

	// The state the producer leaves behind: the seat cancelled, the request
	// still saying matched, its window still open.
	var askID uuid.UUID
	scan(&w.requestID, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
			seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T05:00:00Z','2026-07-30T09:00:00Z', 1, 'open', 'matched')
		RETURNING id`, w.passengerID)
	scan(&askID, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point,
			dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted', '2026-07-29T13:00:00Z')
		RETURNING id`, w.requestID, w.occurrenceID)
	scan(&w.bookingID, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
			status, pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50, 'cancelled_by_driver',
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
		RETURNING id`, w.occurrenceID, w.requestID, w.passengerID, askID)
	return w
}

func (w *strandedWorld) deliver(t *testing.T) error {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"booking_id": w.bookingID, "request_id": w.requestID, "reason": "trip_cancelled",
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	return w.router.Handle(context.Background(), outbox.Message{
		Kind:           "reopen_request",
		Payload:        payload,
		IdempotencyKey: "reopen:" + w.bookingID.String(),
	})
}

func (w *strandedWorld) status(t *testing.T) string {
	t.Helper()
	var s string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, w.requestID).Scan(&s); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	return s
}

func (w *strandedWorld) asksQueued(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&n); err != nil {
		t.Fatalf("counting queued asks: %v", err)
	}
	return n
}

// The case it is for.
func TestAStrandedPassengerIsPutBackInTheQueueAndAsked(t *testing.T) {
	w := newStrandedWorld(t)

	if err := w.deliver(t); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if got := w.status(t); got != "open" {
		t.Fatalf("the request is %q, want open", got)
	}
	if n := w.asksQueued(t); n != 1 {
		t.Fatalf("%d asks queued, want 1", n)
	}
}

// Every way the passenger can have moved on between the cancellation and this.
//
// All of them are a success with nothing done: there is nothing to retry, and
// dead-lettering would say something went wrong when the answer is simply that
// this recovery is no longer wanted.
func TestARecoveryThatIsNoLongerWantedDoesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setUp string
		why   string
	}{
		{
			"they cancelled it themselves",
			`UPDATE ride_requests SET status = 'cancelled' WHERE id = $1`,
			"somebody who called off their own journey must not have it started again",
		},
		{
			"their time ran out",
			`UPDATE ride_requests SET window_start = '2026-07-29T09:00:00Z',
			                          window_end   = '2026-07-29T11:00:00Z' WHERE id = $1`,
			"there is no ride left to find for a morning that has passed",
		},
		{
			"they were already asked again",
			`UPDATE ride_requests SET status = 'asking' WHERE id = $1`,
			"a driver is already being asked; resetting it to open would ask a second",
		},
		{
			"they found another driver first",
			`UPDATE bookings SET status = 'confirmed' WHERE request_id = $1 AND status <> 'confirmed'`,
			"a request holding a live seat is not stranded, and reopening it would sell that seat twice",
		},

	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newStrandedWorld(t)
			if _, err := w.pool.Exec(context.Background(), tc.setUp, w.requestID); err != nil {
				t.Fatalf("setting up: %v", err)
			}
			before := w.status(t)

			if err := w.deliver(t); err != nil {
				t.Fatalf("Handle: %v, want a quiet success", err)
			}

			if got := w.status(t); got != before {
				t.Fatalf("the request moved from %q to %q: %s", before, got, tc.why)
			}
			if n := w.asksQueued(t); n != 0 {
				t.Fatalf("%d asks queued: %s", n, tc.why)
			}
		})
	}
}

// The two account states, which are the reason the passenger's row is read at
// all. A suspended or closed account must not have drivers approached on its
// behalf — that is the whole of the bug an earlier pass fixed at four other
// distances, and this is a fifth place work is created.
func TestARecoveryForSomebodyWhoCannotTravelDoesNothing(t *testing.T) {
	for _, status := range []string{"suspended", "deleted"} {
		t.Run(status, func(t *testing.T) {
			w := newStrandedWorld(t)
			if _, err := w.pool.Exec(context.Background(),
				`UPDATE users SET status = $2 WHERE id = $1`, w.passengerID, status); err != nil {
				t.Fatalf("setting up: %v", err)
			}

			if err := w.deliver(t); err != nil {
				t.Fatalf("Handle: %v, want a quiet success", err)
			}

			if got := w.status(t); got != "matched" {
				t.Fatalf("a %s passenger's request became %q", status, got)
			}
			if n := w.asksQueued(t); n != 0 {
				t.Fatalf("%d drivers were approached on behalf of a %s account", n, status)
			}
		})
	}
}

// The outbox is at-least-once, so this message arrives twice sooner or later.
func TestARecoveryDeliveredTwiceAsksOnce(t *testing.T) {
	w := newStrandedWorld(t)

	if err := w.deliver(t); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := w.deliver(t); err != nil {
		t.Fatalf("second Handle: %v", err)
	}

	if n := w.asksQueued(t); n != 1 {
		t.Fatalf("%d asks queued after two deliveries, want 1", n)
	}
}

// A replayed recovery must not act on a match that has since been and gone.
//
// The outbox is at-least-once, so a message can arrive long after it was
// written — after a lease expires, after a restart. Meanwhile the passenger can
// have been matched again and stranded again, which queues a second recovery.
// If the first one is replayed in that gap it finds exactly what it expects to
// find: a request saying matched, with no live booking, because the *second*
// driver just cancelled.
//
// Acting on it then is wrong twice over. It consumes the reopening that the
// second cancellation was going to do, and its ask carries the first
// cancellation's key, which the outbox has already seen and drops — so the
// request ends up open with nobody being asked, waiting for the hourly
// reconciler to notice.
//
// Being vacant is not the same as having been vacated by this booking.
func TestAReplayedRecoveryDoesNotActOnALaterMatch(t *testing.T) {
	w := newStrandedWorld(t)

	// The first recovery runs and does its job.
	if err := w.deliver(t); err != nil {
		t.Fatalf("first delivery: %v", err)
	}

	// The passenger is matched again, to a second driver, and stranded again.
	second := w.rematch(t)
	if n := w.asksQueued(t); n != 1 {
		t.Fatalf("%d asks queued before the replay, want the first one only", n)
	}

	// Now the first recovery arrives a second time.
	if err := w.deliver(t); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if got := w.status(t); got != "matched" {
		t.Fatalf("a replayed recovery reopened a request its own booking no longer owns: %q", got)
	}

	// And the recovery that is actually owed still works.
	if err := w.deliverFor(t, second); err != nil {
		t.Fatalf("the second recovery: %v", err)
	}
	if got := w.status(t); got != "open" {
		t.Fatalf("the second cancellation left the request at %q", got)
	}
	if n := w.asksQueued(t); n != 2 {
		t.Fatalf("%d asks queued after two genuine strandings, want 2", n)
	}
}

// rematch gives the request a second booking on another departure and cancels
// it, which is the state a second stranding leaves behind. It returns the new
// booking's id.
func (w *strandedWorld) rematch(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var occurrenceID, askID, bookingID uuid.UUID
	scan := func(dest *uuid.UUID, sql string, args ...any) {
		t.Helper()
		if err := w.pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("rematching: %v", err)
		}
	}
	scan(&occurrenceID, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest,
			depart_latest, seats_total, seats_free, template_revision, route_geom, route_km,
			route_min, pricing_mode, status)
		SELECT o.template_id, o.vehicle_id, o.service_date + 1, o.depart_earliest + interval '1 day',
		       o.depart_latest + interval '1 day', 3, 2, 1, o.route_geom, o.route_km, o.route_min,
		       'formula', 'cancelled'
		  FROM trip_occurrences o WHERE o.id = $1
		RETURNING id`, w.occurrenceID)
	scan(&askID, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point,
			dropoff_point, suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted', '2026-07-29T13:00:00Z')
		RETURNING id`, w.requestID, occurrenceID)
	scan(&bookingID, `
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
			status, pickup_point, dropoff_point)
		VALUES ($1, $2, $3, $4, 1, 2.50, 'cancelled_by_driver',
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326))
		RETURNING id`, occurrenceID, w.requestID, w.passengerID, askID)
	if _, err := w.pool.Exec(ctx,
		`UPDATE ride_requests SET status = 'matched' WHERE id = $1`, w.requestID); err != nil {
		t.Fatalf("rematching: %v", err)
	}
	return bookingID
}

// deliverFor is deliver for a booking other than the fixture's own.
func (w *strandedWorld) deliverFor(t *testing.T, bookingID uuid.UUID) error {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"booking_id": bookingID, "request_id": w.requestID, "reason": "trip_cancelled",
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	return w.router.Handle(context.Background(), outbox.Message{
		Kind:           "reopen_request",
		Payload:        payload,
		IdempotencyKey: "reopen:" + bookingID.String(),
	})
}

// The payload names a booking and a request, and nothing else makes the two
// agree. A message pairing somebody's cancelled seat with a different person's
// request must not reopen that request: the booking is the evidence, and it is
// evidence about its own request only.
func TestARecoveryNamingSomebodyElsesRequestDoesNothing(t *testing.T) {
	w := newStrandedWorld(t)
	ctx := context.Background()

	var other uuid.UUID
	if err := w.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
			seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T05:00:00Z','2026-07-30T09:00:00Z', 1, 'open', 'matched')
		RETURNING id`, w.passengerID).Scan(&other); err != nil {
		t.Fatalf("seeding a second request: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"booking_id": w.bookingID, "request_id": other, "reason": "trip_cancelled",
	})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if err := w.router.Handle(ctx, outbox.Message{
		Kind: "reopen_request", Payload: payload,
		IdempotencyKey: "reopen:mismatched",
	}); err != nil {
		t.Fatalf("Handle: %v, want a quiet success", err)
	}

	var status string
	if err := w.pool.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1`, other).Scan(&status); err != nil {
		t.Fatalf("reading the other request: %v", err)
	}
	if status != "matched" {
		t.Fatalf("a recovery for one person's seat reopened another person's request: %q", status)
	}
}
