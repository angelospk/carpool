package asks_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/domain"
	"carpool/internal/geo"
)

// straightLineRouter stands in for OSRM at a fixed 60 km/h so expected detours
// are arithmetic rather than recorded fixtures.
type straightLineRouter struct{ err error }

func (r straightLineRouter) Route(_ context.Context, waypoints []geo.Point) (geo.Route, error) {
	if r.err != nil {
		return geo.Route{}, r.err
	}
	var metres float64
	for i := 1; i < len(waypoints); i++ {
		metres += haversine(waypoints[i-1], waypoints[i])
	}
	return geo.Route{
		DistanceMeters: metres,
		Duration:       time.Duration(metres / 1000 / 60 * float64(time.Hour)),
	}, nil
}

func haversine(a, b geo.Point) float64 {
	const r = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLon := rad(b.Lat-a.Lat), rad(b.Lon-a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

func (f *fixture) matcher() *asks.Service {
	return f.service().WithRouter(straightLineRouter{})
}

func TestAskNextCreatesAnAskForTheMatchingDriver(t *testing.T) {
	f := newFixture(t)
	// Drop the ask the fixture pre-created so this request starts clean.
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	askID, err := f.matcher().AskNext(context.Background(), f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	if askID == uuid.Nil {
		t.Fatal("no ask created")
	}
	if got := f.askStatus(askID); got != "pending" {
		t.Errorf("ask status = %q, want pending", got)
	}

	var occurrenceID uuid.UUID
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT occurrence_id FROM asks WHERE id = $1`, askID).Scan(&occurrenceID), "ask occurrence")
	if occurrenceID != f.occurrenceID {
		t.Errorf("ask points at %v, want the fixture occurrence %v", occurrenceID, f.occurrenceID)
	}
}

func TestAskNextMarksTheRequestAsAsking(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	if _, err := f.matcher().AskNext(context.Background(), f.requestID); err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	var status string
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT status FROM ride_requests WHERE id = $1`, f.requestID).Scan(&status), "request")
	if status != "asking" {
		t.Fatalf("request status = %q, want asking", status)
	}
}

func TestAskNextQueuesAPushForTheDriver(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `DELETE FROM outbox`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	askID, err := f.matcher().AskNext(context.Background(), f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	var queued int
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE idempotency_key = $1`, "ask:"+askID.String()).
		Scan(&queued), "outbox")
	if queued != 1 {
		t.Fatalf("queued %d pushes for the ask, want 1", queued)
	}
}

// The whole point of the sequential flow: a driver who declined is not asked
// again about the same request.
func TestAskNextNeverReAsksADriverWhoAlreadyDeclined(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.service().Decline(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("Decline: %v", err)
	}

	_, err := f.matcher().AskNext(ctx, f.requestID)

	// The fixture has exactly one driver, so declining exhausts the candidates.
	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates once the only driver declined", err)
	}
}

func TestAskNextIgnoresARequestThatIsAlreadyMatched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	_, err := f.matcher().AskNext(ctx, f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates for a matched request", err)
	}
}

func TestAskNextSkipsADriverWhoseDetourBudgetIsTooSmall(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	// The budget is read from the occurrence, not the profile: the profile is
	// only what fills in a new route. Setting it there would change nothing,
	// which is the whole point of moving it.
	mustExec(t, f, `UPDATE trip_occurrences SET detour_min = 0, detour_km = 0`)
	mustExec(t, f, `UPDATE ride_requests SET origin = ST_SetSRID(ST_MakePoint(25.82, 40.858), 4326)`)

	_, err := f.matcher().AskNext(context.Background(), f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates when the detour exceeds the budget", err)
	}
}

// The profile is a default for creating a route, not a live setting. A driver
// who tightens it should not silently change the terms of a trip somebody may
// already be looking at.
func TestTighteningTheProfileDoesNotChangeAPublishedTrip(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	mustExec(t, f, `UPDATE driver_profiles SET detour_budget_min = 0, default_detour_km = 0, default_allow_stops = false`)

	if _, err := f.matcher().AskNext(context.Background(), f.requestID); err != nil {
		t.Fatalf("AskNext: %v, want the published trip to keep its own terms", err)
	}
}

// Refusing intermediate stops has to mean no, not "none that cost anything".
func TestADriverWhoRefusedStopsIsNotAsked(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	mustExec(t, f, `UPDATE trip_occurrences SET allow_stops = false`)

	_, err := f.matcher().AskNext(context.Background(), f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates when the driver takes no stops", err)
	}
}

func TestAskNextExcludesABlockedDriver(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`,
		f.passengerID, f.driverID); err != nil {
		t.Fatalf("blocking the driver: %v", err)
	}

	_, err := f.matcher().AskNext(context.Background(), f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates when the only driver is blocked", err)
	}
}

// The full M4 loop: an open request becomes an ask, the driver accepts, and a
// booking exists on both sides.
func TestEndToEndRequestToAskToAcceptedBooking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	bookingID, err := f.service().Accept(ctx, askID, f.driverID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var (
		passengerID  uuid.UUID
		occurrenceID uuid.UUID
		status       string
		price        float64
	)
	must(t, f.pool.QueryRow(ctx, `
		SELECT passenger_id, occurrence_id, status, price_eur FROM bookings WHERE id = $1`,
		bookingID).Scan(&passengerID, &occurrenceID, &status, &price), "booking")

	if passengerID != f.passengerID {
		t.Errorf("booking passenger = %v, want %v", passengerID, f.passengerID)
	}
	if occurrenceID != f.occurrenceID {
		t.Errorf("booking occurrence = %v, want %v", occurrenceID, f.occurrenceID)
	}
	if status != "confirmed" {
		t.Errorf("booking status = %q, want confirmed", status)
	}
	if price < 0 {
		t.Errorf("price_eur = %v, want a non-negative suggestion", price)
	}
	if got := f.seatsFree(); got != 2 {
		t.Errorf("seats_free = %d, want 2", got)
	}
}

// An unanswered ask must hand the request to the next driver rather than
// stranding the passenger.
func TestEndToEndAskExpiryAdvancesToTheNextDriver(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	secondOccurrence := f.addSecondDriver(t)

	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

	first, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("first AskNext: %v", err)
	}

	// Nobody answers.
	f.clock.Advance(asks.DefaultTTL + time.Minute)
	if n, err := f.service().SweepExpired(ctx); err != nil || n != 1 {
		t.Fatalf("SweepExpired = %d, %v; want 1, nil", n, err)
	}

	second, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("second AskNext: %v", err)
	}

	if second == first {
		t.Fatal("the same ask was returned; the request did not advance")
	}

	// Both drivers serve the identical corridor, so the ranking ties and breaks
	// on id: either may be approached first. What matters is that the expiry
	// moved the request to the *other* one.
	occurrenceOf := func(askID uuid.UUID) uuid.UUID {
		var id uuid.UUID
		must(t, f.pool.QueryRow(ctx,
			`SELECT occurrence_id FROM asks WHERE id = $1`, askID).Scan(&id), "ask occurrence")
		return id
	}
	firstOcc, secondOcc := occurrenceOf(first), occurrenceOf(second)
	if firstOcc == secondOcc {
		t.Fatalf("both asks went to occurrence %v; the same driver was asked twice", firstOcc)
	}
	for _, want := range []uuid.UUID{f.occurrenceID, secondOccurrence} {
		if firstOcc != want && secondOcc != want {
			t.Errorf("occurrence %v was never asked; the two asks were %v and %v",
				want, firstOcc, secondOcc)
		}
	}
	if got := f.askStatus(first); got != "expired" {
		t.Errorf("first ask status = %q, want expired", got)
	}
}

// addSecondDriver publishes a second driver on the same corridor and returns
// their occurrence id.
func (f *fixture) addSecondDriver(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var regionID uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT id FROM regions LIMIT 1`).Scan(&regionID), "region")

	var driverID uuid.UUID
	must(t, f.pool.QueryRow(ctx,
		`INSERT INTO users (phone, name, home_region_id) VALUES ('+306900000009','Driver2',$1) RETURNING id`,
		regionID).Scan(&driverID), "second driver")
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1,'Clio','ZZZ-9999')`,
		driverID); err != nil {
		t.Fatalf("second driver profile: %v", err)
	}

	var templateID uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, driverID).Scan(&templateID), "second template")

	var occurrenceID uuid.UUID
	must(t, f.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, '2026-07-30', '2026-07-30T04:35:00Z', '2026-07-30T05:05:00Z',
			3, 3, 1, ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, templateID).Scan(&occurrenceID), "second occurrence")
	return occurrenceID
}

func mustExec(t *testing.T, f *fixture, sql string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// The suggested contribution has to carry the barriers the passenger will
// actually sit through. This drives the whole production path — gross match,
// insertion, pricing — over a route that crosses the Μέστης plaza on the
// Egnatia, and checks the toll reaches the ask.
func TestAskPriceIncludesTheTollsThePassengerRidesThrough(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)

	// The Egnatia through Μέστης (41.0172, 25.5288), running south-east. Both
	// the template and the occurrence carry it: the matcher reads one, the toll
	// share reads the other.
	const egnatia = `LINESTRING(25.45 41.06, 25.5288 41.0172, 25.62 40.97, 25.70 40.92)`
	mustExec(t, f, `UPDATE route_templates SET
		origin = ST_SetSRID(ST_MakePoint(25.45, 41.06), 4326),
		destination = ST_SetSRID(ST_MakePoint(25.70, 40.92), 4326),
		route_geom = ST_GeomFromText('`+egnatia+`', 4326), route_km = 30`)
	mustExec(t, f, `UPDATE trip_occurrences SET
		route_geom = ST_GeomFromText('`+egnatia+`', 4326), route_km = 30`)

	// The passenger rides the first half — through the plaza.
	mustExec(t, f, `UPDATE ride_requests SET status = 'open',
		origin = ST_SetSRID(ST_MakePoint(25.46, 41.055), 4326),
		destination = ST_SetSRID(ST_MakePoint(25.60, 40.985), 4326)`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}
	if askID == uuid.Nil {
		t.Fatal("no ask created")
	}

	var price float64
	must(t, f.pool.QueryRow(ctx,
		`SELECT suggested_price_eur FROM asks WHERE id = $1`, askID).Scan(&price), "ask price")

	// Μέστης costs 2.75 €, shared with the driver: 1.37 or 1.38 depending on
	// which side the odd cent falls. The fuel part is rounded to 50-cent steps,
	// so the toll is what puts a non-multiple of 0.50 on the end.
	cents := int(price*100 + 0.5)
	if cents%50 == 0 {
		t.Fatalf("suggested %.2f € is a whole 50-cent step; the toll never reached it", price)
	}
	if got := cents % 50; got != 37 && got != 38 {
		t.Fatalf("suggested %.2f €; the toll part is %d cents, want 137 or 138 over the fuel step",
			price, got)
	}
}

// The mirror image: a passenger who joins after the last barrier owes nothing
// towards it, so the suggestion stays on a whole fuel step.
func TestAskPriceCarriesNoTollForAPassengerWhoJoinsAfterThePlaza(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)

	const egnatia = `LINESTRING(25.45 41.06, 25.5288 41.0172, 25.62 40.97, 25.70 40.92)`
	mustExec(t, f, `UPDATE route_templates SET
		origin = ST_SetSRID(ST_MakePoint(25.45, 41.06), 4326),
		destination = ST_SetSRID(ST_MakePoint(25.70, 40.92), 4326),
		route_geom = ST_GeomFromText('`+egnatia+`', 4326), route_km = 30`)
	mustExec(t, f, `UPDATE trip_occurrences SET
		route_geom = ST_GeomFromText('`+egnatia+`', 4326), route_km = 30`)

	// Boarding past Μέστης, riding to the end.
	mustExec(t, f, `UPDATE ride_requests SET status = 'open',
		origin = ST_SetSRID(ST_MakePoint(25.62, 40.97), 4326),
		destination = ST_SetSRID(ST_MakePoint(25.70, 40.92), 4326)`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}
	if askID == uuid.Nil {
		t.Fatal("no ask created")
	}

	var price float64
	must(t, f.pool.QueryRow(ctx,
		`SELECT suggested_price_eur FROM asks WHERE id = $1`, askID).Scan(&price), "ask price")

	if cents := int(price*100 + 0.5); cents%50 != 0 {
		t.Fatalf("suggested %.2f € carries a toll the passenger drove past before boarding", price)
	}
}

// The divisor in the suggested contribution has to come from the trip's own
// published seat count, and this is the only test that proves it does.
//
// Every other price assertion here checks a shape: non-negative, a multiple of
// fifty cents. A bug that read the seat count as zero would satisfy all of them,
// because a zero divisor short-circuits to nothing and nothing is a fine price.
// So this pins the two things that would be silently lost: the figure is
// actually a figure, and widening the car lowers it.
func TestTheSuggestedPriceIsDividedByTheSeatsTheTripPublishes(t *testing.T) {
	ctx := context.Background()

	suggest := func(t *testing.T, seats int) float64 {
		t.Helper()
		f := newFixture(t)
		mustExec(t, f, `UPDATE trip_occurrences SET seats_total = `+itoa(seats)+`, seats_free = `+itoa(seats))
		mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)
		mustExec(t, f, `DELETE FROM asks`)

		askID, err := f.matcher().AskNext(ctx, f.requestID)
		if err != nil {
			t.Fatalf("AskNext with %d seats: %v", seats, err)
		}
		var price float64
		must(t, f.pool.QueryRow(ctx,
			`SELECT suggested_price_eur FROM asks WHERE id = $1`, askID).Scan(&price), "ask price")
		return price
	}

	narrow := suggest(t, 1)
	wide := suggest(t, 4)

	if narrow <= 0 {
		t.Fatalf("a one-seat car suggested %.2f €: the seat count is not reaching the formula", narrow)
	}
	if wide >= narrow {
		t.Errorf("four seats suggested %.2f € and one seat %.2f €; spreading the same fuel over more people has to cost each of them less",
			wide, narrow)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// The receipt, checked where it actually matters: on a real booking, against a
// real database, recomputed from nothing but the stored row.
//
// Every input to the price lives somewhere that changes — the driver's car, the
// vehicle dataset, the region's fuel price, the route. Six months from now this
// row is the only thing that can answer "why did I pay that", and a basis that
// does not add up is worse than none, because it looks like an answer.
func TestABookingCarriesAReceiptThatStillAddsUp(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// A long trip on purpose. The fixture's default hop is 13 km, which after
	// the free-below waiver and the 50-cent floor prices at zero, and a receipt
	// that reproduces zero reproduces nothing.
	const long = "LINESTRING(25.00 40.85, 27.00 40.85)"
	mustExec(t, f, `UPDATE route_templates SET
		origin = ST_SetSRID(ST_MakePoint(25.00, 40.85), 4326),
		destination = ST_SetSRID(ST_MakePoint(27.00, 40.85), 4326),
		route_geom = ST_GeomFromText('`+long+`', 4326), route_km = 168`)
	mustExec(t, f, `UPDATE trip_occurrences SET
		route_geom = ST_GeomFromText('`+long+`', 4326), route_km = 168`)
	mustExec(t, f, `UPDATE ride_requests SET status = 'open',
		origin = ST_SetSRID(ST_MakePoint(25.10, 40.85), 4326),
		destination = ST_SetSRID(ST_MakePoint(26.90, 40.85), 4326)`)
	mustExec(t, f, `DELETE FROM asks`) // the fixture seeds one; AskNext must be free to pick
	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}
	bookingID, err := f.service().Accept(ctx, askID, f.driverID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var price float64
	var raw []byte
	must(t, f.pool.QueryRow(ctx,
		`SELECT price_eur, pricing_basis FROM bookings WHERE id = $1`,
		bookingID).Scan(&price, &raw), "booking")

	if len(raw) == 0 {
		t.Fatal("the booking carries no pricing basis, so the price is unexplainable")
	}
	var basis domain.Basis
	if err := json.Unmarshal(raw, &basis); err != nil {
		t.Fatalf("decoding the stored basis: %v", err)
	}

	if basis.Version != domain.BasisVersion {
		t.Errorf("basis version %d, want %d", basis.Version, domain.BasisVersion)
	}
	if basis.ConsumptionSource == "" {
		t.Error("the basis does not say where the consumption came from")
	}
	if basis.OfferedSeats == 0 || basis.TripKm == 0 || basis.FuelPriceEUR == 0 {
		t.Fatalf("the basis is missing inputs and cannot be replayed: %+v", basis)
	}
	if price <= 0 {
		t.Fatalf("this trip priced at %v, so replaying it proves nothing", price)
	}

	// Checked against the database rather than against the basis's own
	// arithmetic: a divisor that is recorded wrong but consistently wrong would
	// replay perfectly and still misdescribe the booking.
	var seatsTotal int
	must(t, f.pool.QueryRow(ctx,
		`SELECT o.seats_total FROM trip_occurrences o
		 JOIN bookings b ON b.occurrence_id = o.id WHERE b.id = $1`,
		bookingID).Scan(&seatsTotal), "seats")
	if basis.OfferedSeats != seatsTotal {
		t.Errorf("the receipt says the cost was split over %d seats; the trip published %d",
			basis.OfferedSeats, seatsTotal)
	}

	// Nothing below reads the fixture. This is what somebody holding only the
	// stored row could do.
	replayed := domain.SuggestedPriceCents(domain.PricingInputs{
		TripKm:                   basis.TripKm,
		FuelConsumptionLPer100Km: basis.ConsumptionLPer100Km,
		FuelPriceEUR:             basis.FuelPriceEUR,
		WearFactor:               basis.WearFactor,
		FreeBelowKm:              basis.FreeBelowKm,
		OfferedSeats:             basis.OfferedSeats,
		TollShareCents:           basis.TollShareCents,
		Mode:                     domain.PricingMode(basis.Mode),
	})
	if want := int(price*100 + 0.5); replayed != want {
		t.Errorf("replaying the receipt gave %d cents, the booking says %d", replayed, want)
	}
}
