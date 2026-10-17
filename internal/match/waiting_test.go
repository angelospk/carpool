package match_test

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/match"
)

// requestOpts are the ways a waiting request can differ from the ordinary one.
type requestOpts struct {
	origin       match.Point
	destination  match.Point
	windowStart  time.Time
	windowEnd    time.Time
	seats        int
	status       string
	kind         string
	target       *uuid.UUID
	passenger    *uuid.UUID
	createdAt    time.Time
	luggage      string
	luggageCount int
}

func withRequestLuggage(size string, count int) func(*requestOpts) {
	return func(o *requestOpts) { o.luggage, o.luggageCount = size, count }
}

func withRequestStatus(s string) func(*requestOpts) {
	return func(o *requestOpts) { o.status = s }
}
func withRequestSeats(n int) func(*requestOpts) {
	return func(o *requestOpts) { o.seats = n }
}
func withRequestEnds(originLon, destLon float64) func(*requestOpts) {
	return func(o *requestOpts) {
		o.origin.Lon, o.destination.Lon = originLon, destLon
	}
}
func withRequestEndsAt(oLon, oLat, dLon, dLat float64) func(*requestOpts) {
	return func(o *requestOpts) {
		o.origin = match.Point{Lon: oLon, Lat: oLat}
		o.destination = match.Point{Lon: dLon, Lat: dLat}
	}
}
func withRequestWindow(start, end time.Time) func(*requestOpts) {
	return func(o *requestOpts) { o.windowStart, o.windowEnd = start, end }
}
func withRequestPassenger(id uuid.UUID) func(*requestOpts) {
	return func(o *requestOpts) { o.passenger = &id }
}
func withTarget(id uuid.UUID) func(*requestOpts) {
	return func(o *requestOpts) { o.kind, o.target = "targeted", &id }
}
func withCreatedAt(at time.Time) func(*requestOpts) {
	return func(o *requestOpts) { o.createdAt = at }
}

// seededPhones numbers the passengers addRequest makes up, in a range of
// their own (+30698…) that no fixed phone in these tests uses.
var seededPhones atomic.Int64

// addRequest inserts somebody waiting for the corridor the fixture's routes run
// along, travelling in the same direction as query().

func (f *fixture) addRequest(t *testing.T, opts ...func(*requestOpts)) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	q := query()
	o := requestOpts{
		origin:      q.Origin,
		destination: q.Destination,
		windowStart: q.WindowStart,
		windowEnd:   q.WindowEnd,
		seats:       1,
		status:      "open",
		kind:        "open",
		createdAt:   time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
		luggage:     "none",
	}
	for _, opt := range opts {
		opt(&o)
	}

	passenger := o.passenger
	if passenger == nil {
		// A counter, not a random number: thousands of random eight-digit
		// phones collide often enough to fail a run now and then.
		var id uuid.UUID
		err := f.pool.QueryRow(ctx, `
			INSERT INTO users (date_of_birth, phone, name, home_region_id)
			VALUES (DATE '1990-01-01', $2, 'Passenger', $1)
			RETURNING id`, f.regionID, fmt.Sprintf("+30698%07d", seededPhones.Add(1))).Scan(&id)
		if err != nil {
			t.Fatalf("seeding passenger: %v", err)
		}
		passenger = &id
	}

	var requestID uuid.UUID
	err := f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
			seats, kind, target_occurrence_id, status, created_at, luggage, luggage_count)
		VALUES ($1, ST_SetSRID(ST_MakePoint($2,$3),4326), ST_SetSRID(ST_MakePoint($4,$5),4326),
			$6, $7, $8, $9, $10, $11, $12, $13::luggage_size, $14)
		RETURNING id`,
		*passenger, o.origin.Lon, o.origin.Lat, o.destination.Lon, o.destination.Lat,
		o.windowStart, o.windowEnd, o.seats, o.kind, o.target, o.status, o.createdAt, o.luggage, o.luggageCount).Scan(&requestID)
	if err != nil {
		t.Fatalf("inserting ride request: %v", err)
	}
	return requestID
}

func waitingQuery(occurrenceID uuid.UUID) match.WaitingQuery {
	return match.WaitingQuery{
		OccurrenceID: occurrenceID,
		Now:          time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
		Limit:        50,
	}
}

func waiting(t *testing.T, f *fixture, occurrenceID uuid.UUID) []uuid.UUID {
	t.Helper()
	got, err := match.Waiting(context.Background(), f.pool, waitingQuery(occurrenceID))
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	return got
}

func TestWaitingFindsSomebodyAlreadyLookingForThisJourney(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t)

	if got := waiting(t, f, occurrenceID); !slices.Contains(got, requestID) {
		t.Fatalf("Waiting returned %v, want it to contain the open request %s", got, requestID)
	}
}

// Everything the forward search would match, the reverse search must surface.
//
// The two directions are separate queries, and the failure mode of a reverse
// query that is stricter than the forward one is silent: a driver publishes,
// nobody is woken, and the request dies of old age exactly as it did before any
// of this was built. So the property is stated as containment and tested, not
// asserted by reading the two SQL strings side by side.
func TestWaitingReturnsEverythingTheForwardSearchWouldMatch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// A spread of shapes: the plain case, a short hop inside the corridor, a
	// party of two, a window that only just overlaps, and a late departure.
	occurrences := []uuid.UUID{
		f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85)),
		f.addOccurrence(t, wktLine(25.75, 40.85, 26.05, 40.85), withSeatsFree(2)),
		f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85), withWindow(
			time.Date(2026, time.July, 30, 8, 30, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 9, 30, 0, 0, time.UTC))),
	}
	requests := []uuid.UUID{
		f.addRequest(t),
		f.addRequest(t, withRequestSeats(2)),
		f.addRequest(t, withRequestEnds(25.84, 25.94)),
		f.addRequest(t, withRequestWindow(
			time.Date(2026, time.July, 30, 8, 45, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 11, 0, 0, 0, time.UTC))),
		// The two window edges. A request that overlaps the departure by ten
		// minutes at either end is still a match, and these are what catch a
		// reverse query that trims the window even slightly: without them a
		// twenty-minute haircut on either side goes unnoticed.
		f.addRequest(t, withRequestWindow(
			time.Date(2026, time.July, 30, 5, 0, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 7, 10, 0, 0, time.UTC))),
		f.addRequest(t, withRequestWindow(
			time.Date(2026, time.July, 30, 7, 50, 0, 0, time.UTC),
			time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC))),
		// Luggage exactly at what the trip allows. Anything short of the
		// boundary lets a reverse query that demands strictly more room look
		// correct, because the default request carries nothing at all.
		f.addRequest(t, withRequestLuggage("suitcase", 1)),
	}

	for _, occurrenceID := range occurrences {
		woken := waiting(t, f, occurrenceID)

		for _, requestID := range requests {
			q, err := forwardQuery(ctx, f, requestID)
			if err != nil {
				t.Fatalf("reading request %s: %v", requestID, err)
			}
			candidates, err := match.Gross(ctx, f.pool, q)
			if err != nil {
				t.Fatalf("Gross: %v", err)
			}
			matched := slices.ContainsFunc(candidates, func(c match.Candidate) bool {
				return c.OccurrenceID == occurrenceID
			})
			if matched && !slices.Contains(woken, requestID) {
				t.Errorf("the forward search matches request %s to occurrence %s, "+
					"but the reverse search does not wake it: a driver publishing "+
					"this trip would never reach that passenger",
					requestID, occurrenceID)
			}
		}
	}
}

// forwardQuery reads a stored request back as the gross filter sees it, so the
// containment test compares the two directions on the same facts.
func forwardQuery(ctx context.Context, f *fixture, requestID uuid.UUID) (match.GrossQuery, error) {
	var q match.GrossQuery
	err := f.pool.QueryRow(ctx, `
		SELECT ST_X(origin), ST_Y(origin), ST_X(destination), ST_Y(destination),
		       window_start, window_end, seats, passenger_id, target_occurrence_id,
		       ST_Distance(origin::geography, destination::geography) / 1000,
		       luggage, luggage_count
		FROM ride_requests WHERE id = $1`, requestID).Scan(
		&q.Origin.Lon, &q.Origin.Lat, &q.Destination.Lon, &q.Destination.Lat,
		&q.WindowStart, &q.WindowEnd, &q.Seats, &q.PassengerID, &q.Target,
		&q.RequestKm, &q.Luggage, &q.LuggageCount)
	q.RequestID = requestID
	q.Now = waitingQuery(uuid.Nil).Now
	return q, err
}

func TestWaitingIgnoresARequestGoingTheOtherWay(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	// Dropoff west of pickup: the same road, the opposite direction.
	requestID := f.addRequest(t, withRequestEnds(25.98, 25.82))

	if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
		t.Fatalf("Waiting woke a request travelling the other way (%s)", requestID)
	}
}

func TestWaitingIgnoresARequestThatIsNotOpen(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t, withRequestStatus("matched"))

	if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
		t.Fatalf("Waiting woke a request that is already settled (%s)", requestID)
	}
}

// A driver who is also looking for a lift is not woken about their own car.
func TestWaitingIgnoresTheDriversOwnRequest(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t, withRequestPassenger(f.driverID))

	if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
		t.Fatalf("Waiting woke the driver about their own trip (%s)", requestID)
	}
}

// A request that has already been put to this trip is not put to it again,
// whatever the driver said. Without this a wake would re-offer a trip somebody
// already declined every time a seat freed up on it.
func TestWaitingIgnoresARequestThisTripHasAlreadyBeenOffered(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	requestID := f.addRequest(t)

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
			ST_SetSRID(ST_MakePoint(25.82,40.851),4326), ST_SetSRID(ST_MakePoint(25.98,40.851),4326),
			2.50, 0, 'declined', '2026-07-29T12:30:00Z')`, requestID, occurrenceID); err != nil {
		t.Fatalf("recording the earlier offer: %v", err)
	}

	if got := waiting(t, f, occurrenceID); slices.Contains(got, requestID) {
		t.Fatalf("Waiting re-offered a trip this request had already seen (%s)", requestID)
	}
}

// A repeat names the trip it wants. It is woken by that trip and by no other.
func TestWaitingWakesATargetedRequestOnlyForItsOwnTrip(t *testing.T) {
	f := setup(t)
	wanted := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))
	other := f.addOccurrence(t, wktLine(25.75, 40.85, 26.05, 40.85))
	requestID := f.addRequest(t, withTarget(wanted))

	if got := waiting(t, f, wanted); !slices.Contains(got, requestID) {
		t.Fatalf("Waiting did not wake a repeat for the trip it named (%v)", got)
	}
	if got := waiting(t, f, other); slices.Contains(got, requestID) {
		t.Fatalf("Waiting woke a repeat for a trip nobody asked about (%s)", requestID)
	}
}

// Seats already promised to somebody are not offered again, so a wake on a full
// trip wakes nobody rather than everybody.
func TestWaitingIgnoresRequestsWhenEverySeatIsSpokenFor(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85), withSeatsFree(1))
	taken := f.addRequest(t)
	waitingRequest := f.addRequest(t)

	if _, err := f.pool.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2,
			ST_SetSRID(ST_MakePoint(25.82,40.851),4326), ST_SetSRID(ST_MakePoint(25.98,40.851),4326),
			2.50, 0, 'pending', '2026-07-29T12:30:00Z')`, taken, occurrenceID); err != nil {
		t.Fatalf("recording the live offer: %v", err)
	}

	if got := waiting(t, f, occurrenceID); slices.Contains(got, waitingRequest) {
		t.Fatalf("Waiting woke a request for a seat already promised to somebody (%s)", waitingRequest)
	}
}

// Oldest first. The wake fans out into independent messages the dispatcher runs
// unordered, so this decides who gets into the shortlist at all, not who is
// served first. Somebody who has been waiting since Monday should not lose
// their place to somebody who asked a minute ago.
func TestWaitingPutsTheLongestWaitFirst(t *testing.T) {
	f := setup(t)
	occurrenceID := f.addOccurrence(t, wktLine(25.80, 40.85, 26.00, 40.85))

	recent := f.addRequest(t, withCreatedAt(time.Date(2026, time.July, 29, 11, 0, 0, 0, time.UTC)))
	oldest := f.addRequest(t, withCreatedAt(time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC)))

	got := waiting(t, f, occurrenceID)

	if len(got) < 2 {
		t.Fatalf("Waiting returned %v, want both requests", got)
	}
	if got[0] != oldest {
		t.Fatalf("Waiting led with %s, want the request that has waited longest (%s, not %s)",
			got[0], oldest, recent)
	}
}
