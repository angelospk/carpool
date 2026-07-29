package asks_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
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
	// No detour is tolerated at all, and the pickup lies off the driver's line.
	mustExec(t, f, `UPDATE driver_profiles SET detour_budget_min = 0`)
	mustExec(t, f, `UPDATE ride_requests SET origin = ST_SetSRID(ST_MakePoint(25.82, 40.858), 4326)`)

	_, err := f.matcher().AskNext(context.Background(), f.requestID)

	if !errors.Is(err, asks.ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates when the detour exceeds the budget", err)
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
