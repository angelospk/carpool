package asks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/asks"
	"carpool/internal/geo"
	"carpool/internal/match"
)

// placedAsk adds a passenger with a pending ask on the fixture occurrence,
// quoted against itinerary version 0, from origin to dest.
func (f *fixture) placedAsk(phone string, seats int, origin, dest geo.Point) (passenger, requestID, askID uuid.UUID) {
	f.t.Helper()
	ctx := context.Background()
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO users (date_of_birth, phone, name, home_region_id)
		VALUES (DATE '1990-01-01', $1, 'Rider', (SELECT home_region_id FROM users WHERE id = $2)) RETURNING id`,
		phone, f.passengerID).Scan(&passenger), "passenger")
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint($2,$3),4326), ST_SetSRID(ST_MakePoint($4,$5),4326),
			'2026-07-30T04:00:00Z', '2026-07-30T06:00:00Z', $6, 'open', 'asking')
		RETURNING id`, passenger, origin.Lon, origin.Lat, dest.Lon, dest.Lat, seats).Scan(&requestID), "request")
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint($3,$4),4326), ST_SetSRID(ST_MakePoint($5,$6),4326),
			2.50, 0, 'pending', $7)
		RETURNING id`, requestID, f.occurrenceID, origin.Lon, origin.Lat, dest.Lon, dest.Lat,
		f.clock.Now().Add(asks.DefaultTTL)).Scan(&askID), "ask")
	return passenger, requestID, askID
}

func (f *fixture) askNextQueued(requestID uuid.UUID) int {
	f.t.Helper()
	var n int
	must(f.t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE kind = 'ask_next' AND payload->>'request_id' = $1`,
		requestID.String()).Scan(&n), "ask_next")
	return n
}

var (
	onRoadFrom = geo.Point{Lon: 25.84, Lat: 40.85}
	onRoadTo   = geo.Point{Lon: 25.96, Lat: 40.85}
)

// QA 2026-10-17 F1: two passengers asked about the same morning before the
// driver answered. Accepting the first used to leave the second quoted
// against the old plan, and every accept of it then failed with
// itinerary_changed until it expired half an hour later.
func TestASecondPendingAskCanBeAcceptedAfterTheFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		svc  func(*fixture) *asks.Service
	}{
		{"with a router", (*fixture).matcher},
		{"without a router", (*fixture).service},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			svc := tc.svc(f)
			_, _, second := f.placedAsk("+306900000031", 1, onRoadFrom, onRoadTo)

			if _, err := svc.Accept(ctx, f.askID, f.driverID); err != nil {
				t.Fatalf("first accept: %v", err)
			}
			var askV, occV int
			must(t, f.pool.QueryRow(ctx, `
				SELECT a.itinerary_version, o.itinerary_version FROM asks a
				  JOIN trip_occurrences o ON o.id = a.occurrence_id WHERE a.id = $1`, second).
				Scan(&askV, &occV), "versions")
			if askV != occV {
				t.Errorf("second ask quoted against version %d, plan is at %d: it was not re-quoted", askV, occV)
			}
			if _, err := svc.Accept(ctx, second, f.driverID); err != nil {
				t.Fatalf("second accept: %v", err)
			}
			if got := f.seatsFree(); got != 1 {
				t.Errorf("seats_free = %d, want 1", got)
			}
		})
	}
}

// The re-quote carries the new figures, not only the new version: the detour
// a driver is shown is what this passenger adds to the plan as it now stands.
func TestARequotedAskCarriesTheDetourOfTheNewPlan(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `UPDATE trip_occurrences SET detour_km = 20, detour_min = 30`)
	_, _, second := f.placedAsk("+306900000032", 1,
		geo.Point{Lon: 25.90, Lat: 40.87}, onRoadTo)

	if _, err := f.matcher().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	var km float64
	must(t, f.pool.QueryRow(ctx, `SELECT detour_km FROM asks WHERE id = $1`, second).Scan(&km), "detour")
	if km == 2 {
		t.Errorf("detour_km still the stored 2 km; the re-quote did not recompute it")
	}
	if km <= 0 {
		t.Errorf("detour_km = %v, want the cost of a pickup 2 km off the road", km)
	}
}

// A second offer that fitted the driver's budget alone, and does not with the
// first passenger aboard, is withdrawn at once and its passenger looked for
// elsewhere, rather than left on the driver's phone as an offer that can never
// be taken.
func TestAPendingAskThatNoLongerFitsIsWithdrawnAndRequeued(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Each pickup alone costs under half a kilometre; both together, on
	// opposite sides of the road, close to two.
	mustExec(t, f, `UPDATE trip_occurrences SET detour_km = 1, detour_min = 30`)
	mustExec(t, f, `UPDATE asks SET pickup_point = ST_SetSRID(ST_MakePoint(25.85, 40.865), 4326)`)
	mustExec(t, f, `UPDATE ride_requests SET origin = ST_SetSRID(ST_MakePoint(25.85, 40.865), 4326)`)
	_, secondReq, second := f.placedAsk("+306900000033", 1,
		geo.Point{Lon: 25.90, Lat: 40.835}, onRoadTo)

	if _, err := f.matcher().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if got := f.askStatus(second); got != "cancelled" {
		t.Errorf("second ask = %q, want cancelled once it no longer fits", got)
	}
	if got := f.requestStatus(t, secondReq); got != "open" {
		t.Errorf("second request = %q, want open again", got)
	}
	if got := f.askNextQueued(secondReq); got != 1 {
		t.Errorf("%d ask_next queued for the second passenger, want 1", got)
	}
	if _, err := f.matcher().Accept(ctx, second, f.driverID); !errors.Is(err, asks.ErrAskNotPending) {
		t.Errorf("accepting the withdrawn ask: %v, want ErrAskNotPending", err)
	}
}

// The party cap is enforced where the seat is given, not only where the offer
// is made: three offers can be in flight on a four-seat car, and only two
// groups may ride.
func TestTheThirdPartyIsNotLetOnAtAccept(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `UPDATE trip_occurrences SET seats_total = 4, seats_free = 4`)
	_, _, second := f.placedAsk("+306900000034", 1, onRoadFrom, onRoadTo)
	_, thirdReq, third := f.placedAsk("+306900000035", 1, onRoadFrom, onRoadTo)
	svc := f.matcher()

	if _, err := svc.Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Accept(ctx, second, f.driverID); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := f.askStatus(third); got != "cancelled" {
		t.Errorf("third ask = %q, want cancelled once two groups are aboard", got)
	}
	if got := f.requestStatus(t, thirdReq); got != "open" {
		t.Errorf("third request = %q, want open again", got)
	}
	if got := f.seatsFree(); got != 2 {
		t.Errorf("seats_free = %d, want 2", got)
	}
}

// And an offer that reached the accept some other way, with the car already
// carrying two groups, is refused by name and withdrawn.
func TestAcceptRefusesAPartyOverTheCap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `UPDATE trip_occurrences SET seats_total = 4, seats_free = 4`)
	_, _, second := f.placedAsk("+306900000036", 1, onRoadFrom, onRoadTo)
	svc := f.service()
	if _, err := svc.Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Accept(ctx, second, f.driverID); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Made behind the re-quote's back, at the current version.
	_, thirdReq, third := f.placedAsk("+306900000037", 1, onRoadFrom, onRoadTo)
	mustExec(t, f, `UPDATE asks SET itinerary_version = (SELECT itinerary_version FROM trip_occurrences) WHERE id = $1`, third)

	_, err := svc.Accept(ctx, third, f.driverID)
	if !errors.Is(err, match.ErrPartyCapReached) {
		t.Fatalf("third accept: %v, want ErrPartyCapReached", err)
	}
	if got := f.askStatus(third); got != "cancelled" {
		t.Errorf("third ask = %q, want cancelled", got)
	}
	if got := f.requestStatus(t, thirdReq); got != "open" {
		t.Errorf("third request = %q, want open again", got)
	}
}

// Anything else that moves the plan (a drive, a standing arrangement) leaves
// an offer quoted against an older version. It is re-quoted at its own
// accept: taken if it still fits, withdrawn if not. Never left pending.
func TestAStaleAskIsRequotedAtItsOwnAccept(t *testing.T) {
	t.Run("still fits", func(t *testing.T) {
		f := newFixture(t)
		mustExec(t, f, `UPDATE trip_occurrences SET itinerary_version = 7`)
		if _, err := f.matcher().Accept(context.Background(), f.askID, f.driverID); err != nil {
			t.Fatalf("accept: %v", err)
		}
	})
	t.Run("no longer fits", func(t *testing.T) {
		f := newFixture(t)
		mustExec(t, f, `UPDATE trip_occurrences SET itinerary_version = 7, allow_stops = false`)
		_, err := f.matcher().Accept(context.Background(), f.askID, f.driverID)
		if !errors.Is(err, asks.ErrStaleItinerary) {
			t.Fatalf("accept: %v, want ErrStaleItinerary", err)
		}
		if got := f.askStatus(f.askID); got != "cancelled" {
			t.Errorf("ask = %q, want cancelled", got)
		}
		if got := f.requestStatus(t, f.requestID); got != "open" {
			t.Errorf("request = %q, want open again", got)
		}
		if got := f.seatsFree(); got != 3 {
			t.Errorf("seats_free = %d, want 3", got)
		}
	})
}

// Every refusal that is about the car rather than the click withdraws the
// offer, so the passenger is not held behind it.
func TestAFullCarWithdrawsTheOffer(t *testing.T) {
	f := newFixture(t)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_free = 0`)
	if _, err := f.service().Accept(context.Background(), f.askID, f.driverID); !errors.Is(err, asks.ErrNoSeats) {
		t.Fatalf("accept: %v, want ErrNoSeats", err)
	}
	if got := f.askStatus(f.askID); got != "cancelled" {
		t.Errorf("ask = %q, want cancelled", got)
	}
	if got := f.requestStatus(t, f.requestID); got != "open" {
		t.Errorf("request = %q, want open again", got)
	}
}

// QA 2026-10-17 6c: the driver answers two offers at once (two phones, or a
// double tap across two cards). Both fit, so both land: the one that waited
// for the lock finds itself re-quoted and starts again.
func TestTwoAcceptsAtOnceOnOneTripBothLand(t *testing.T) {
	f := newFixture(t)
	_, _, second := f.placedAsk("+306900000038", 1, onRoadFrom, onRoadTo)
	svc := f.matcher()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, ask := range []uuid.UUID{f.askID, second} {
		wg.Add(1)
		go func(i int, ask uuid.UUID) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Accept(context.Background(), ask, f.driverID)
		}(i, ask)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("accept %d: %v", i, err)
		}
	}
	if got := f.seatsFree(); got != 1 {
		t.Errorf("seats_free = %d, want 1", got)
	}
}

// Codex review, wave 1: two passengers, each asking for the same two days,
// each with an offer on each day. Accepting X on day one and Y on day two at
// once makes each accept re-quote the other's offer and withdraw its own
// passenger's other day: without the passengers' locks taken in one order,
// the two deadlock.
func TestCrossDayAcceptsDoNotDeadlock(t *testing.T) {
	for i := 0; i < 3; i++ {
		f := newFixture(t)
		ctx := context.Background()
		mustExec(t, f, `UPDATE trip_occurrences SET seats_total = 4, seats_free = 4`)
		var day2 uuid.UUID
		must(t, f.pool.QueryRow(ctx, `
			INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
			SELECT template_id, vehicle_id, '2026-07-31', '2026-07-31T04:30:00Z', '2026-07-31T05:00:00Z',
				4, 4, 1, route_geom, 17, 20, 'formula' FROM trip_occurrences WHERE id = $1
			RETURNING id`, f.occurrenceID).Scan(&day2), "day two")

		x, xReq1, xAsk1 := f.placedAsk(fmt20("+3069000002", i, 0), 1, onRoadFrom, onRoadTo)
		y, yReq1, _ := f.placedAsk(fmt20("+3069000002", i, 1), 1, onRoadFrom, onRoadTo)
		xAsk2 := f.otherDay(x, xReq1, day2)
		yAsk2 := f.otherDay(y, yReq1, day2)
		_ = xAsk2

		// Each accept waits here, briefly, for the other to have re-quoted
		// too: that is when both hold the other's request.
		var arrived sync.WaitGroup
		arrived.Add(2)
		svc := f.service().WithAfterRequote(func() {
			arrived.Done()
			done := make(chan struct{})
			go func() { arrived.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
			}
		})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for k, ask := range []uuid.UUID{xAsk1, yAsk2} {
			wg.Add(1)
			go func(k int, ask uuid.UUID) {
				defer wg.Done()
				<-start
				_, errs[k] = svc.Accept(ctx, ask, f.driverID)
			}(k, ask)
		}
		close(start)
		wg.Wait()
		for k, err := range errs {
			if err != nil {
				t.Fatalf("run %d accept %d: %v", i, k, err)
			}
		}
	}
}

func fmt20(prefix string, i, k int) string {
	return prefix + string(rune('0'+i)) + string(rune('0'+k))
}

// otherDay adds another day of the passenger's request rootReq, with a
// pending offer on occurrence.
func (f *fixture) otherDay(passenger, rootReq, occurrence uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()
	var req, ask uuid.UUID
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, seats, kind, status, alternative_of)
		SELECT passenger_id, origin, destination, window_start + interval '1 day', window_end + interval '1 day',
		       seats, kind, 'asking', id FROM ride_requests WHERE id = $1
		RETURNING id`, rootReq).Scan(&req), "other day")
	must(f.t, f.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		SELECT $1, $2, 5, 2, origin, destination, 2.50, 0, 'pending', $3 FROM ride_requests WHERE id = $1
		RETURNING id`, req, occurrence, f.clock.Now().Add(asks.DefaultTTL)).Scan(&ask), "other day's ask")
	_ = passenger
	return ask
}

// Codex review, wave 1: an offer that has lapsed but not been swept yet holds
// no seat, so it must not crowd a live offer out at re-quote.
func TestALapsedOfferDoesNotCrowdOutALiveOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _, lapsed := f.placedAsk("+306900000051", 2, onRoadFrom, onRoadTo)
	_, _, live := f.placedAsk("+306900000052", 1, onRoadFrom, onRoadTo)
	mustExec(t, f, `UPDATE asks SET expires_at = $2 WHERE id = $1`, lapsed, f.clock.Now().Add(-time.Minute))

	if _, err := f.matcher().Accept(ctx, f.askID, f.driverID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got := f.askStatus(live); got != "pending" {
		t.Errorf("live offer = %q, want pending: the lapsed one took its seats", got)
	}
}
