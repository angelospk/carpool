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
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος', ST_Multi(ST_GeomFromText('POLYGON((25 40, 27 40, 27 42, 25 42, 25 40))', 4326)), 1.8)
		RETURNING id`).Scan(&regionID), "region")

	must(t, pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, home_region_id) VALUES ('+306900000001','Driver',$1) RETURNING id`,
		regionID).Scan(&f.driverID), "driver")
	must(t, pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, home_region_id) VALUES ('+306900000002','Passenger',$1) RETURNING id`,
		regionID).Scan(&f.passengerID), "passenger")

	_, err := pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1,'Yaris','ABC-1234')`, f.driverID)
	must(t, err, "driver profile")

	var templateID uuid.UUID
	must(t, pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1,
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, f.driverID).Scan(&templateID), "template")

	must(t, pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, '2026-07-30', '2026-07-30T04:30:00Z', '2026-07-30T05:00:00Z',
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
		INSERT INTO users (phone, name, home_region_id)
		SELECT '+306900000003', 'Other', home_region_id FROM users WHERE id = $1
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
