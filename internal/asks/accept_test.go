package asks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/asks"
	"carpool/internal/clock"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

type fixture struct {
	t            *testing.T
	pool         *pgxpool.Pool
	clock        *clock.Fake
	driverID     uuid.UUID
	passengerID  uuid.UUID
	occurrenceID uuid.UUID
	requestID    uuid.UUID
	askID        uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	f := &fixture{t: t, pool: pool,
		clock: clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))}

	var regionID uuid.UUID
	must(t, pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)))
		RETURNING id`).Scan(&regionID), "region")
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES (NULL, 'petrol', 1.859, 'test'), (NULL, 'diesel', 1.729, 'test')`); err != nil {
		t.Fatalf("seeding fuel prices: %v", err)
	}

	must(t, pool.QueryRow(ctx,
		`INSERT INTO users (date_of_birth, phone, name, home_region_id) VALUES (DATE '1990-01-01', '+306900000001','Driver',$1) RETURNING id`,
		regionID).Scan(&f.driverID), "driver")
	must(t, pool.QueryRow(ctx,
		`INSERT INTO users (date_of_birth, phone, name, home_region_id) VALUES (DATE '1990-01-01', '+306900000002','Passenger',$1) RETURNING id`,
		regionID).Scan(&f.passengerID), "passenger")

	_, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Yaris','ABC-1234')`, f.driverID)
	must(t, err, "driver profile")
	testdb.GiveEveryDriverACar(t, pool)

	var templateID uuid.UUID
	must(t, pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, f.driverID).Scan(&templateID), "template")

	must(t, pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-30', '2026-07-30T04:30:00Z', '2026-07-30T05:00:00Z',
			3, 3, 1, ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, templateID).Scan(&f.occurrenceID), "occurrence")

	f.requestID, f.askID = f.newAsk("+306900000002", f.passengerID)
	return f
}

// newAsk creates a request and a pending ask against the fixture occurrence.
func (f *fixture) newAsk(_ string, passengerID uuid.UUID) (requestID, askID uuid.UUID) {
	f.t.Helper()
	ctx := context.Background()

	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T04:00:00Z', '2026-07-30T06:00:00Z', 1, 'open', 'asking')
		RETURNING id`, passengerID).Scan(&requestID), "request")

	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			2.50, 0, 'pending', $3)
		RETURNING id`, requestID, f.occurrenceID, f.clock.Now().Add(asks.DefaultTTL)).Scan(&askID), "ask")
	return requestID, askID
}

func (f *fixture) service() *asks.Service { return asks.NewService(f.pool, f.clock, nil) }

func (f *fixture) seatsFree() int {
	f.t.Helper()
	var n int
	must(f.t, f.pool.QueryRow(context.Background(),
		`SELECT seats_free FROM trip_occurrences WHERE id = $1`, f.occurrenceID).Scan(&n), "seats")
	return n
}

func (f *fixture) askStatus(id uuid.UUID) string {
	f.t.Helper()
	var s string
	must(f.t, f.pool.QueryRow(context.Background(),
		`SELECT status FROM asks WHERE id = $1`, id).Scan(&s), "ask status")
	return s
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func TestAcceptCreatesABookingAndReservesTheSeat(t *testing.T) {
	f := newFixture(t)

	bookingID, err := f.service().Accept(context.Background(), f.askID, f.driverID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if bookingID == uuid.Nil {
		t.Fatal("no booking id returned")
	}
	if got := f.seatsFree(); got != 2 {
		t.Errorf("seats_free = %d, want 2", got)
	}
	if got := f.askStatus(f.askID); got != "accepted" {
		t.Errorf("ask status = %q, want accepted", got)
	}

	var requestStatus string
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, f.requestID).Scan(&requestStatus), "request")
	if requestStatus != "matched" {
		t.Errorf("request status = %q, want matched", requestStatus)
	}
}

func TestAcceptBumpsTheItineraryVersion(t *testing.T) {
	f := newFixture(t)

	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var version int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT itinerary_version FROM trip_occurrences WHERE id = $1`, f.occurrenceID).Scan(&version), "version")
	if version != 1 {
		t.Fatalf("itinerary_version = %d, want 1", version)
	}
}

func TestAcceptQueuesAConfirmationForThePassenger(t *testing.T) {
	f := newFixture(t)

	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var queued int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'push' AND idempotency_key LIKE 'booking_confirmed:%'`).
		Scan(&queued), "outbox")
	if queued != 1 {
		t.Fatalf("queued %d confirmations, want 1", queued)
	}
}

func TestAcceptRejectsADriverWhoWasNotAsked(t *testing.T) {
	f := newFixture(t)
	stranger := uuid.New()

	_, err := f.service().Accept(context.Background(), f.askID, stranger)

	if !errors.Is(err, asks.ErrNotYours) {
		t.Fatalf("error = %v, want ErrNotYours", err)
	}
	if got := f.seatsFree(); got != 3 {
		t.Errorf("seats_free = %d; a rejected accept must not reserve a seat", got)
	}
}

func TestAcceptRejectsAnExpiredAsk(t *testing.T) {
	f := newFixture(t)
	f.clock.Advance(asks.DefaultTTL + time.Minute)

	_, err := f.service().Accept(context.Background(), f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskExpired) {
		t.Fatalf("error = %v, want ErrAskExpired", err)
	}
	if got := f.seatsFree(); got != 3 {
		t.Errorf("seats_free = %d; an expired accept must not reserve a seat", got)
	}
}

func TestAcceptIsRejectedOnceTheAskIsDeclined(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.service().Decline(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("Decline: %v", err)
	}

	_, err := f.service().Accept(ctx, f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskNotPending) {
		t.Fatalf("error = %v, want ErrAskNotPending", err)
	}
}

func TestAcceptRejectsAFullOccurrence(t *testing.T) {
	f := newFixture(t)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE trip_occurrences SET seats_free = 0 WHERE id = $1`, f.occurrenceID); err != nil {
		t.Fatalf("filling the trip: %v", err)
	}

	_, err := f.service().Accept(context.Background(), f.askID, f.driverID)

	if !errors.Is(err, asks.ErrNoSeats) {
		t.Fatalf("error = %v, want ErrNoSeats", err)
	}
}

// staleVerifier mimics the driver's plan changing while OSRM was consulted.
type staleVerifier struct {
	pool         *pgxpool.Pool
	occurrenceID uuid.UUID
}

func (v staleVerifier) Verify(ctx context.Context, _ uuid.UUID, _ int) error {
	_, err := v.pool.Exec(ctx,
		`UPDATE trip_occurrences SET itinerary_version = itinerary_version + 1 WHERE id = $1`, v.occurrenceID)
	return err
}

// The detour is verified outside the transaction, so the itinerary can move
// underneath it. The version check must catch that rather than committing a
// booking against a stale quote.
func TestAcceptRejectsAStaleItineraryVersion(t *testing.T) {
	f := newFixture(t)
	svc := asks.NewService(f.pool, f.clock, staleVerifier{pool: f.pool, occurrenceID: f.occurrenceID})

	_, err := svc.Accept(context.Background(), f.askID, f.driverID)

	if !errors.Is(err, asks.ErrStaleItinerary) {
		t.Fatalf("error = %v, want ErrStaleItinerary", err)
	}
	if got := f.seatsFree(); got != 3 {
		t.Errorf("seats_free = %d; a stale accept must not reserve a seat", got)
	}
}

// Two taps on "accept" (a double tap, or a retry after a flaky response) must
// produce one booking, not two.
func TestConcurrentAcceptsOfOneAskProduceASingleBooking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const racers = 4
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.service().Accept(ctx, f.askID, f.driverID)
		}()
	}
	close(start)
	wg.Wait()

	var ok int
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d of %d concurrent accepts succeeded, want exactly 1 (errors: %v)", ok, racers, errs)
	}

	var bookings int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM bookings`).Scan(&bookings), "bookings")
	if bookings != 1 {
		t.Fatalf("created %d bookings, want 1", bookings)
	}
	if got := f.seatsFree(); got != 2 {
		t.Fatalf("seats_free = %d, want exactly one seat taken", got)
	}
}

// Two different passengers racing for the last seat: one wins, one is told the
// trip is full. Seats must never go negative.
func TestConcurrentAcceptsForTheLastSeatLeaveNoOverbooking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`UPDATE trip_occurrences SET seats_free = 1 WHERE id = $1`, f.occurrenceID); err != nil {
		t.Fatalf("setting one free seat: %v", err)
	}

	var otherPassenger uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		SELECT DATE '1990-01-01', '+306900000003', 'Other', home_region_id FROM users WHERE id = $1
		RETURNING id`, f.passengerID).Scan(&otherPassenger), "other passenger")
	_, secondAsk := f.newAsk("+306900000003", otherPassenger)

	errs := make([]error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, askID := range []uuid.UUID{f.askID, secondAsk} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.service().Accept(ctx, askID, f.driverID)
		}()
	}
	close(start)
	wg.Wait()

	var ok int
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d accepts succeeded for one seat, want exactly 1 (errors: %v)", ok, errs)
	}
	if got := f.seatsFree(); got != 0 {
		t.Fatalf("seats_free = %d, want 0 and never negative", got)
	}
}

func TestSweepExpiresAnOverdueAsk(t *testing.T) {
	f := newFixture(t)
	f.clock.Advance(asks.DefaultTTL + time.Minute)

	n, err := f.service().SweepExpired(context.Background())
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}

	if n != 1 {
		t.Fatalf("expired %d asks, want 1", n)
	}
	if got := f.askStatus(f.askID); got != "expired" {
		t.Fatalf("ask status = %q, want expired", got)
	}
}

func TestSweepLeavesALiveAskAlone(t *testing.T) {
	f := newFixture(t)

	n, err := f.service().SweepExpired(context.Background())
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}

	if n != 0 {
		t.Fatalf("expired %d asks, want 0 while the TTL is unexpired", n)
	}
}

// Expiring an ask must hand the request on, or an unanswered ask would strand
// the passenger forever.
func TestSweepQueuesTheNextCandidate(t *testing.T) {
	f := newFixture(t)
	f.clock.Advance(asks.DefaultTTL + time.Minute)

	if _, err := f.service().SweepExpired(context.Background()); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}

	var queued int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&queued), "outbox")
	if queued != 1 {
		t.Fatalf("queued %d ask_next messages, want 1", queued)
	}

	var status string
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, f.requestID).Scan(&status), "request")
	if status != "open" {
		t.Fatalf("request status = %q, want open so it can be re-matched", status)
	}
}

// The sweeper and a driver tapping accept at the same moment must not both win.
func TestSweepDoesNotExpireAnAskThatWasJustAccepted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	f.clock.Advance(asks.DefaultTTL + time.Minute)
	n, err := f.service().SweepExpired(ctx)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}

	if n != 0 {
		t.Fatalf("expired %d asks, want 0 — the ask was already accepted", n)
	}
	if got := f.askStatus(f.askID); got != "accepted" {
		t.Fatalf("ask status = %q, want it to stay accepted", got)
	}
}

func TestDeclineReopensTheRequestForTheNextCandidate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.service().Decline(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("Decline: %v", err)
	}

	if got := f.askStatus(f.askID); got != "declined" {
		t.Errorf("ask status = %q, want declined", got)
	}
	var status string
	must(t, f.pool.QueryRow(ctx,
		`SELECT status FROM ride_requests WHERE id = $1`, f.requestID).Scan(&status), "request")
	if status != "open" {
		t.Errorf("request status = %q, want open", status)
	}
	if got := f.seatsFree(); got != 3 {
		t.Errorf("seats_free = %d; declining must not touch seats", got)
	}
}

// --- regression tests for issues found in adversarial review ---

// The TTL is authoritative. Without an expiry check in Decline, a late tap would
// land as 'declined' or 'expired' purely depending on whether the driver or the
// sweeper got there first.
func TestDeclineIsRejectedOnceTheAskHasExpired(t *testing.T) {
	f := newFixture(t)
	f.clock.Advance(asks.DefaultTTL + time.Minute)

	err := f.service().Decline(context.Background(), f.askID, f.driverID)

	if !errors.Is(err, asks.ErrAskExpired) {
		t.Fatalf("error = %v, want ErrAskExpired", err)
	}
	if got := f.askStatus(f.askID); got == "declined" {
		t.Error("a late decline was recorded, overwriting what the sweeper should expire")
	}
}

// Expiring an ask whose request has already moved on must not send another
// driver after the passenger.
//
// The asks_one_pending index makes this hard to reach through the API — it
// permits only one pending ask per request — so the state is forced directly.
// The guard is defence in depth behind that index, not a substitute for it.
func TestExpiringAnAskDoesNotRequeueARequestThatMovedOn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The ask stays pending while its request is already matched.
	mustExec(t, f, `UPDATE ride_requests SET status = 'matched'`)
	mustExec(t, f, `DELETE FROM outbox WHERE kind = 'ask_next'`)

	f.clock.Advance(asks.DefaultTTL + time.Minute)
	if _, err := f.service().SweepExpired(ctx); err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}

	if got := f.askStatus(f.askID); got != "expired" {
		t.Errorf("ask status = %q, want expired", got)
	}
	var queued int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next'`).Scan(&queued), "outbox")
	if queued != 0 {
		t.Fatalf("queued %d ask_next messages for a request that already moved on, want 0", queued)
	}
}

// luggageAsk is newAsk with a stated number of items to carry.
func (f *fixture) luggageAsk(phone string, count int) (passenger, requestID, askID uuid.UUID) {
	f.t.Helper()
	ctx := context.Background()

	must(f.t, f.pool.QueryRow(ctx,
		`INSERT INTO users (date_of_birth, phone, name, home_region_id)
		 SELECT DATE '1990-01-01', $1, 'Επιβάτης', home_region_id FROM users WHERE id = $2
		 RETURNING id`, phone, f.passengerID).Scan(&passenger), "passenger")

	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
			seats, kind, status, luggage, luggage_count)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T04:00:00Z', '2026-07-30T06:00:00Z', 1, 'open', 'asking', 'suitcase', $2)
		RETURNING id`, passenger, count).Scan(&requestID), "request")

	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
			ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			2.50,
			-- Whatever the plan is right now. Each accepted booking bumps the
			-- version, so an ask quoted against 0 is stale the moment anybody
			-- else has been let on.
			(SELECT itinerary_version FROM trip_occurrences WHERE id = $2),
			'pending', $3)
		RETURNING id`, requestID, f.occurrenceID, f.clock.Now().Add(asks.DefaultTTL)).Scan(&askID), "ask")
	return passenger, requestID, askID
}

// setCapacity puts a known number of luggage places on the departure.
func (f *fixture) setCapacity(n int) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE trip_occurrences SET luggage_capacity = $2 WHERE id = $1`, f.occurrenceID, n); err != nil {
		f.t.Fatalf("setting capacity: %v", err)
	}
}

// The boot is shared, not handed to each passenger in turn.
//
// Checking each booking against the whole capacity lets three passengers with
// two suitcases each into a car with room for two, because every one of them
// looks fine on their own. What has already been promised has to count.
func TestLuggageCapacityIsSharedBetweenPassengers(t *testing.T) {
	f := newFixture(t)
	f.setCapacity(3)
	svc := f.service()
	ctx := context.Background()

	_, _, firstAsk := f.luggageAsk("+306900000021", 2)
	if _, err := svc.Accept(ctx, firstAsk, f.driverID); err != nil {
		t.Fatalf("first booking of two places: %v", err)
	}

	// One place left, and this one wants two.
	_, _, secondAsk := f.luggageAsk("+306900000022", 2)
	if _, err := svc.Accept(ctx, secondAsk, f.driverID); !errors.Is(err, asks.ErrNoLuggageRoom) {
		t.Errorf("accepting two more places into room for one: %v, want ErrNoLuggageRoom", err)
	}

	// And the one that does fit still goes in.
	_, _, thirdAsk := f.luggageAsk("+306900000023", 1)
	if _, err := svc.Accept(ctx, thirdAsk, f.driverID); err != nil {
		t.Errorf("accepting the one place that is left: %v", err)
	}
}

// Space is derived from the bookings that hold it, so a cancellation gives it
// back by doing nothing in particular. This is the property that makes the
// derived sum worth having over a counter.
func TestCancellingABookingReleasesItsLuggage(t *testing.T) {
	f := newFixture(t)
	f.setCapacity(2)
	svc := f.service()
	ctx := context.Background()

	_, _, first := f.luggageAsk("+306900000024", 2)
	bookingID, err := svc.Accept(ctx, first, f.driverID)
	if err != nil {
		t.Fatalf("first booking: %v", err)
	}

	_, _, second := f.luggageAsk("+306900000025", 1)
	if _, err := svc.Accept(ctx, second, f.driverID); !errors.Is(err, asks.ErrNoLuggageRoom) {
		t.Fatalf("the boot should be full: %v", err)
	}

	if _, err := f.pool.Exec(ctx,
		`UPDATE bookings SET status = 'cancelled_by_passenger' WHERE id = $1`, bookingID); err != nil {
		t.Fatalf("cancelling: %v", err)
	}

	_, _, third := f.luggageAsk("+306900000026", 1)
	if _, err := svc.Accept(ctx, third, f.driverID); err != nil {
		t.Errorf("the cancelled booking did not give its luggage back: %v", err)
	}
}

// Two drivers' worth of the same last place. Exactly one gets it.
func TestTwoBookingsCannotShareTheLastLuggagePlace(t *testing.T) {
	f := newFixture(t)
	f.setCapacity(1)
	svc := f.service()

	_, _, a := f.luggageAsk("+306900000027", 1)
	_, _, b := f.luggageAsk("+306900000028", 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, ask := range []uuid.UUID{a, b} {
		wg.Add(1)
		go func(i int, ask uuid.UUID) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Accept(context.Background(), ask, f.driverID)
		}(i, ask)
	}
	close(start)
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of two racing bookings took the last luggage place, want 1 (%v)", won, errs)
	}
}
