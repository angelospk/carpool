package api_test

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// A driver's offer to somebody waiting on a weekend plan (specs/WEEKEND.md).
// The departure is a day away, on the same road as the plan's requests.

type offerWorld struct {
	h              *harness
	driver         string
	driverID       string
	passenger      string
	passengerID    string
	planID         string
	occurrence     string
	requestID      string
	otherDayID     string
	stranger       string
	strangerUserID string
}

func (h *harness) weekendRequest(passengerID, planID string, start, end time.Time, alternativeOf string) string {
	h.t.Helper()
	var id string
	var alt any
	if alternativeOf != "" {
		alt = alternativeOf
	}
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status,
		                           weekend_plan_id, weekend_of, weekend_leg, alternative_of)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        'Αλεξανδρούπολη', 'Φέρες', $2, $3, 1, 'open', 'open',
		        $4::uuid, '2026-07-31', 'out', $5::uuid)
		RETURNING id::text`, &id, passengerID, start, end, planID, alt)
	return id
}

func (h *harness) weekendPlan(passengerID string) string {
	h.t.Helper()
	var id string
	h.mustQuery(`
		INSERT INTO weekend_plans (passenger_id, origin, destination, depart_dow, depart_time)
		VALUES ($1::uuid, ST_SetSRID(ST_MakePoint(25.8743,40.8476),4326), ST_SetSRID(ST_MakePoint(26.1717,40.8975),4326),
		        5, '15:00')
		RETURNING id::text`, &id, passengerID)
	return id
}

func newOfferWorld(t *testing.T) *offerWorld {
	t.Helper()
	h := newRoutedMatcherHarness(t)
	w := &offerWorld{h: h}
	w.occurrence = h.occurrenceOf("+306900001001", "Οδηγός")
	w.driver, _ = h.signUp("+306900001001", "Οδηγός")
	w.passenger, _ = h.signUp("+306900001002", "Ελένη")
	w.stranger, _ = h.signUp("+306900001003", "Άλλη")
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900001001'`, &w.driverID)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900001002'`, &w.passengerID)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900001003'`, &w.strangerUserID)
	w.planID = h.weekendPlan(w.passengerID)
	now := h.clock.Now()
	w.requestID = h.weekendRequest(w.passengerID, w.planID, now.Add(23*time.Hour), now.Add(26*time.Hour), "")
	// Another day of the same journey: accepting one withdraws it.
	w.otherDayID = h.weekendRequest(w.passengerID, w.planID, now.Add(47*time.Hour), now.Add(50*time.Hour), w.requestID)
	return w
}

type waitingRow struct {
	RequestID        string  `json:"request_id"`
	PassengerName    string  `json:"passenger_name"`
	Seats            int     `json:"seats"`
	Luggage          string  `json:"luggage"`
	LuggageCount     int     `json:"luggage_count"`
	WindowStart      string  `json:"window_start"`
	WindowEnd        string  `json:"window_end"`
	OriginLabel      string  `json:"origin_label"`
	DestinationLabel string  `json:"destination_label"`
	Phone            *string `json:"phone"`
}

func (w *offerWorld) waiting(token string) []waitingRow {
	w.h.t.Helper()
	rec := w.h.do(http.MethodGet, "/v1/trips/"+w.occurrence+"/waiting", nil, token)
	if rec.Code != http.StatusOK {
		w.h.t.Fatalf("waiting: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Waiting []waitingRow `json:"waiting"`
	}
	w.h.decode(rec, &body)
	return body.Waiting
}

func (w *offerWorld) offer(token string) (int, string) {
	rec := w.h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers",
		map[string]any{"request_id": w.requestID}, token)
	return rec.Code, rec.Body.String()
}

type offerRow struct {
	ID               string   `json:"id"`
	RequestID        string   `json:"request_id"`
	DriverName       string   `json:"driver_name"`
	DriverPhotoURL   *string  `json:"driver_photo_url"`
	DriverRating     *float64 `json:"driver_rating"`
	Car              struct{ Make, Model, Colour string }
	DepartEarliest   string  `json:"depart_earliest"`
	PriceEUR         float64 `json:"price_eur"`
	Seats            int     `json:"seats"`
	DetourMin        float64 `json:"detour_min"`
	ExpiresAt        string  `json:"expires_at"`
	OriginLabel      string  `json:"origin_label"`
	DestinationLabel string  `json:"destination_label"`
}

func (w *offerWorld) offers(token string) []offerRow {
	w.h.t.Helper()
	rec := w.h.do(http.MethodGet, "/v1/offers", nil, token)
	if rec.Code != http.StatusOK {
		w.h.t.Fatalf("offers: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Offers []offerRow `json:"offers"`
	}
	w.h.decode(rec, &body)
	return body.Offers
}

func TestTheDriverSeesWhoIsWaitingOnTheirRoad(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	now := h.clock.Now()

	// None of these is waiting for this departure: an ordinary request, a
	// weekend request whose hours do not meet it, and one with a live ask.
	h.openRequest(w.strangerUserID, "open")
	strangerPlan := h.weekendPlan(w.strangerUserID)
	h.weekendRequest(w.strangerUserID, strangerPlan, now.Add(100*time.Hour), now.Add(103*time.Hour), "")
	busy := h.weekendRequest(w.strangerUserID, strangerPlan, now.Add(23*time.Hour), now.Add(26*time.Hour), "")
	h.mustExec(`UPDATE ride_requests SET status = 'asking' WHERE id = $1::uuid`, busy)

	got := w.waiting(w.driver)
	if len(got) != 1 {
		t.Fatalf("waiting = %+v, want only the one who can be carried", got)
	}
	g := got[0]
	if g.RequestID != w.requestID || g.PassengerName != "Ελένη" || g.Seats != 1 || g.Luggage != "none" ||
		g.OriginLabel != "Αλεξανδρούπολη" || g.DestinationLabel != "Φέρες" || g.WindowStart == "" || g.WindowEnd == "" {
		t.Errorf("waiting row = %+v", g)
	}
	if g.Phone != nil {
		t.Errorf("a phone number reached the driver: %v", *g.Phone)
	}
}

func TestOnlyTheDriverOfADepartureSeesWhoIsWaiting(t *testing.T) {
	w := newOfferWorld(t)
	for name, token := range map[string]string{"a passenger": w.passenger, "a stranger": w.stranger} {
		rec := w.h.do(http.MethodGet, "/v1/trips/"+w.occurrence+"/waiting", nil, token)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", name, rec.Code)
		}
	}
	rec := w.h.do(http.MethodGet, "/v1/trips/11111111-1111-1111-1111-111111111111/waiting", nil, w.driver)
	if rec.Code != http.StatusNotFound {
		t.Errorf("a missing trip: %d, want 404", rec.Code)
	}
	if rec := w.h.do(http.MethodGet, "/v1/trips/"+w.occurrence+"/waiting", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", rec.Code)
	}
}

func TestADriverOffersASeatAndThePassengerIsTold(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h

	rec := h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers",
		map[string]any{"request_id": w.requestID}, w.driver)
	if rec.Code != http.StatusCreated {
		t.Fatalf("offer: %d %s", rec.Code, rec.Body.String())
	}

	var offeredBy, status, reqStatus string
	var expires, departs time.Time
	h.mustQuery(`SELECT offered_by, status, expires_at FROM asks WHERE request_id = $1::uuid`,
		&offeredBy, &status, &expires, w.requestID)
	h.mustQuery(`SELECT depart_earliest FROM trip_occurrences WHERE id = $1::uuid`, &departs, w.occurrence)
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &reqStatus, w.requestID)
	if offeredBy != "driver" || status != "pending" || reqStatus != "asking" {
		t.Errorf("ask offered_by=%s status=%s, request %s", offeredBy, status, reqStatus)
	}
	// A day away: two hours before it is sooner than a day from now.
	if want := departs.Add(-2 * time.Hour); !expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v", expires, want)
	}

	// The passenger, not the driver, gets the push.
	var n int
	h.mustQuery(`SELECT count(*) FROM outbox
		WHERE kind = 'push' AND payload->>'event' = 'driver_offer' AND payload->>'recipient_id' = $1`,
		&n, w.passengerID)
	if n != 1 {
		t.Errorf("%d driver_offer pushes for the passenger, want 1", n)
	}
	h.mustQuery(`SELECT count(*) FROM outbox WHERE idempotency_key LIKE 'ask:%'`, &n)
	if n != 0 {
		t.Errorf("%d pushes for the driver, who is the one offering", n)
	}

	// Nobody is asked about it twice.
	if code, _ := w.offer(w.driver); code != http.StatusNotFound && code != http.StatusConflict {
		t.Errorf("a second offer: %d, want a refusal", code)
	}
	h.mustQuery(`SELECT count(*) FROM asks`, &n)
	if n != 1 {
		t.Errorf("%d asks after a second offer", n)
	}
}

func TestAnOfferLivesAtMostADay(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	now := h.clock.Now()
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1::uuid`,
		w.occurrence, now.Add(72*time.Hour), now.Add(73*time.Hour))
	h.mustExec(`UPDATE ride_requests SET window_start = $2, window_end = $3 WHERE id = $1::uuid`,
		w.requestID, now.Add(71*time.Hour), now.Add(76*time.Hour))

	if rec := h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers",
		map[string]any{"request_id": w.requestID}, w.driver); rec.Code != http.StatusCreated {
		t.Fatalf("offer: %d %s", rec.Code, rec.Body.String())
	}
	var expires time.Time
	h.mustQuery(`SELECT expires_at FROM asks`, &expires)
	if want := now.Add(24 * time.Hour); !expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v", expires, want)
	}
}

func TestAnOfferThatWouldLapseAtOnceIsTooLate(t *testing.T) {
	for name, tc := range map[string]struct {
		leavesIn time.Duration
		want     int
	}{
		"leaving in two hours":        {2 * time.Hour, http.StatusConflict},
		"nine minutes of life":        {2*time.Hour + 9*time.Minute, http.StatusConflict},
		"eleven minutes of life":      {2*time.Hour + 11*time.Minute, http.StatusCreated},
		"already gone, no ask exists": {-time.Hour, http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			w := newOfferWorld(t)
			h := w.h
			now := h.clock.Now()
			h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1::uuid`,
				w.occurrence, now.Add(tc.leavesIn), now.Add(tc.leavesIn+30*time.Minute))
			h.mustExec(`UPDATE ride_requests SET window_start = $2, window_end = $3 WHERE id = $1::uuid`,
				w.requestID, now.Add(-2*time.Hour), now.Add(10*time.Hour))

			rec := h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers",
				map[string]any{"request_id": w.requestID}, w.driver)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.leavesIn > 0 && tc.want == http.StatusConflict {
				if got := h.errorCode(rec); got != "too_late" {
					t.Errorf("error code %q, want too_late", got)
				}
			}
			var n int
			h.mustQuery(`SELECT count(*) FROM asks`, &n)
			if want := map[bool]int{true: 1, false: 0}[tc.want == http.StatusCreated]; n != want {
				t.Errorf("%d asks, want %d", n, want)
			}
		})
	}
}

func TestOnlyTheDriverOffersAndOnlyAWeekendRequestStillOpen(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h

	for name, token := range map[string]string{"the passenger": w.passenger, "a stranger": w.stranger} {
		if code, _ := w.offer(token); code != http.StatusNotFound {
			t.Errorf("%s offering: %d, want 404", name, code)
		}
	}

	ordinary := h.openRequest(w.passengerID, "open")
	rec := h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers", map[string]any{"request_id": ordinary}, w.driver)
	if rec.Code != http.StatusNotFound {
		t.Errorf("an ordinary request: %d, want 404", rec.Code)
	}
	h.mustExec(`UPDATE ride_requests SET status = 'cancelled' WHERE id = $1::uuid`, w.requestID)
	if code, _ := w.offer(w.driver); code != http.StatusNotFound {
		t.Errorf("a cancelled request: %d, want 404", code)
	}
	rec = h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers", map[string]any{"request_id": "nope"}, w.driver)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed request id: %d, want 400", rec.Code)
	}
	var n int
	h.mustQuery(`SELECT count(*) FROM asks`, &n)
	if n != 0 {
		t.Errorf("%d asks made by refused offers", n)
	}
}

func TestABlockedPassengerIsNotOfferedASeat(t *testing.T) {
	w := newOfferWorld(t)
	w.h.mustExec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1::uuid, $2::uuid)`, w.passengerID, w.driverID)
	if got := w.waiting(w.driver); len(got) != 0 {
		t.Errorf("waiting = %+v, want nobody", got)
	}
	if code, _ := w.offer(w.driver); code != http.StatusConflict {
		t.Errorf("offer to a blocked passenger: %d, want 409", code)
	}
}

func TestADriverOfferIsTheirPassengersToSeAndNotTheDriversToAnswer(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	if code, _ := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d", code)
	}
	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)

	// Never in the driver's own list of questions, and not openable by id.
	if got := h.pendingAsks(w.driver); len(got) != 0 {
		t.Errorf("the driver's pending asks show their own offer: %+v", got)
	}
	if rec := h.do(http.MethodGet, "/v1/asks/"+askID, nil, w.driver); rec.Code != http.StatusNotFound {
		t.Errorf("driver GET of their own offer: %d, want 404", rec.Code)
	}
	// And the driver cannot answer for the passenger.
	if rec := h.do(http.MethodPost, "/v1/asks/"+askID+"/accept", nil, w.driver); rec.Code != http.StatusNotFound {
		t.Errorf("driver accepting their own offer: %d, want 404", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/asks/"+askID+"/decline", nil, w.driver); rec.Code != http.StatusNotFound {
		t.Errorf("driver declining their own offer: %d, want 404", rec.Code)
	}
	var n int
	h.mustQuery(`SELECT count(*) FROM bookings`, &n)
	if n != 0 {
		t.Errorf("%d bookings made by the driver answering their own offer", n)
	}

	got := w.offers(w.passenger)
	if len(got) != 1 {
		t.Fatalf("offers = %+v, want the one", got)
	}
	o := got[0]
	if o.ID != askID || o.RequestID != w.requestID || o.DriverName != "Οδηγός" || o.Seats != 1 ||
		o.PriceEUR < 0 || o.DepartEarliest == "" || o.ExpiresAt == "" ||
		o.Car.Make != "Toyota" || o.Car.Model != "Yaris" || o.OriginLabel != "Αλεξανδρούπολη" {
		t.Errorf("offer = %+v", o)
	}
	if other := w.offers(w.stranger); len(other) != 0 {
		t.Errorf("a stranger sees %d offers", len(other))
	}
}

func TestAMatcherAskIsNotAnOfferToThePassenger(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	ordinary := h.openRequest(w.passengerID, "asking")
	h.ask(ordinary, w.occurrence, "pending", 10*time.Minute)

	if got := w.offers(w.passenger); len(got) != 0 {
		t.Errorf("offers = %+v, want none: that ask is the driver's to answer", got)
	}
	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)
	for _, path := range []string{"/accept", "/decline"} {
		if rec := h.do(http.MethodPost, "/v1/offers/"+askID+path, nil, w.passenger); rec.Code != http.StatusNotFound {
			t.Errorf("answering a matcher ask as an offer %s: %d, want 404", path, rec.Code)
		}
	}
}

func TestAcceptingAnOfferBooksTheSeatAndWithdrawsTheOtherDays(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	if code, _ := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d", code)
	}
	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)

	// Not somebody else's to accept, and the same answer as no such offer.
	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.stranger); rec.Code != http.StatusNotFound {
		t.Errorf("stranger accepting: %d, want 404", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.driver); rec.Code != http.StatusNotFound {
		t.Errorf("driver accepting as a passenger: %d, want 404", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/offers/11111111-1111-1111-1111-111111111111/accept", nil, w.passenger); rec.Code != http.StatusNotFound {
		t.Errorf("accepting a missing offer: %d, want 404", rec.Code)
	}

	rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		BookingID string `json:"booking_id"`
	}
	h.decode(rec, &body)
	var bookingStatus, passengerID, reqStatus, otherStatus, askStatus string
	var seatsFree int
	h.mustQuery(`SELECT status, passenger_id::text FROM bookings WHERE id = $1::uuid`, &bookingStatus, &passengerID, body.BookingID)
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &reqStatus, w.requestID)
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &otherStatus, w.otherDayID)
	h.mustQuery(`SELECT status FROM asks WHERE id = $1::uuid`, &askStatus, askID)
	h.mustQuery(`SELECT seats_free FROM trip_occurrences WHERE id = $1::uuid`, &seatsFree, w.occurrence)
	if bookingStatus != "confirmed" || passengerID != w.passengerID {
		t.Errorf("booking %s for %s", bookingStatus, passengerID)
	}
	if reqStatus != "matched" || otherStatus != "cancelled" || askStatus != "accepted" || seatsFree != 2 {
		t.Errorf("request %s, other day %s, ask %s, seats free %d", reqStatus, otherStatus, askStatus, seatsFree)
	}
	if got := w.offers(w.passenger); len(got) != 0 {
		t.Errorf("an accepted offer is still listed: %+v", got)
	}
	// Answered once.
	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.passenger); rec.Code != http.StatusConflict {
		t.Errorf("accepting twice: %d, want 409", rec.Code)
	}
}

func TestDecliningAnOfferLeavesTheRequestOpen(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	if code, _ := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d", code)
	}
	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)

	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/decline", nil, w.stranger); rec.Code != http.StatusNotFound {
		t.Errorf("stranger declining: %d, want 404", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/decline", nil, w.passenger); rec.Code != http.StatusNoContent {
		t.Fatalf("decline: %d %s", rec.Code, rec.Body.String())
	}
	var askStatus, reqStatus, otherStatus string
	h.mustQuery(`SELECT status FROM asks WHERE id = $1::uuid`, &askStatus, askID)
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &reqStatus, w.requestID)
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &otherStatus, w.otherDayID)
	if askStatus != "declined" || reqStatus != "open" || otherStatus != "open" {
		t.Errorf("ask %s, request %s, other day %s", askStatus, reqStatus, otherStatus)
	}
	if got := w.offers(w.passenger); len(got) != 0 {
		t.Errorf("a declined offer is still listed: %+v", got)
	}
	var bookings int
	h.mustQuery(`SELECT count(*) FROM bookings`, &bookings)
	if bookings != 0 {
		t.Errorf("%d bookings after a decline", bookings)
	}
	if rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/decline", nil, w.passenger); rec.Code != http.StatusConflict {
		t.Errorf("declining twice: %d, want 409", rec.Code)
	}
	// The same driver is not offered the same person again.
	if code, _ := w.offer(w.driver); code != http.StatusConflict {
		t.Errorf("offering again after a decline: %d, want 409", code)
	}
}

func TestALapsedOfferCannotBeAcceptedOrListed(t *testing.T) {
	w := newOfferWorld(t)
	h := w.h
	if code, _ := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d", code)
	}
	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)

	h.mustExec(`UPDATE asks SET expires_at = $2 WHERE id = $1::uuid`, askID, h.clock.Now().Add(-time.Minute))
	if got := w.offers(w.passenger); len(got) != 0 {
		t.Errorf("a lapsed offer is listed: %+v", got)
	}
	rec := h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.passenger)
	if rec.Code != http.StatusConflict || h.errorCode(rec) != "ask_expired" {
		t.Errorf("accepting a lapsed offer: %d %s, want 409 ask_expired", rec.Code, rec.Body.String())
	}
}

func TestOffersNeedASignedInCaller(t *testing.T) {
	h := newHarness(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/v1/offers"},
		{http.MethodPost, "/v1/offers/11111111-1111-1111-1111-111111111111/accept"},
		{http.MethodPost, "/v1/offers/11111111-1111-1111-1111-111111111111/decline"},
		{http.MethodPost, "/v1/trips/11111111-1111-1111-1111-111111111111/offers"},
	} {
		if rec := h.do(call.method, call.path, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d, want 401", call.method, call.path, rec.Code)
		}
	}
}

// pinDrive makes the departure a drive that leaves at departs, with the
// request's pickup half way along the road and its window opening after the
// car has left the start: the car reaches the pickup inside the window though
// the departure is before it.
func (w *offerWorld) pinDrive(departs time.Time) {
	h := w.h
	h.mustExec(`UPDATE trip_occurrences SET scheduled_earliest = depart_earliest, scheduled_latest = depart_latest
		WHERE id = $1::uuid`, w.occurrence)
	h.mustExec(`UPDATE trip_occurrences
		SET depart_earliest = $2, depart_latest = $2, drive_set_at = $3,
		    offers_until = $2::timestamptz + route_min * interval '1 minute'
		WHERE id = $1::uuid`, w.occurrence, departs, h.clock.Now())
	h.mustExec(`UPDATE ride_requests
		SET origin = ST_SetSRID(ST_MakePoint(26.0230, 40.87255), 4326), window_start = $2, window_end = $3
		WHERE id = $1::uuid`, w.requestID, departs.Add(10*time.Minute), departs.Add(3*time.Hour))
}

// The list a driver offers from is the same question the offer is judged by.
// A drive leaves before the window opens and reaches the pickup inside it, and
// the gross filter lets that pair through; a prefilter that compares the
// departure with the window would hide the passenger from the one person who
// could take them.
func TestADriveSeesAWaitingPassengerItReachesAfterTheWindowOpens(t *testing.T) {
	w := newOfferWorld(t)
	w.pinDrive(w.h.clock.Now().Add(3 * time.Hour))

	got := w.waiting(w.driver)
	if len(got) != 1 || got[0].RequestID != w.requestID {
		t.Fatalf("waiting = %+v, want the passenger the drive reaches inside their window", got)
	}
	if code, body := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d %s", code, body)
	}
}

// An offer on a drive ends two hours before the car leaves, whatever the
// pickup's own deadline is: the lead time is counted from the departure, not
// from when the car reaches this passenger.
func TestAnOfferOnADriveEndsTwoHoursBeforeTheCarLeaves(t *testing.T) {
	w := newOfferWorld(t)
	departs := w.h.clock.Now().Add(3 * time.Hour)
	w.pinDrive(departs)

	if code, body := w.offer(w.driver); code != http.StatusCreated {
		t.Fatalf("offer: %d %s", code, body)
	}
	var expires time.Time
	w.h.mustQuery(`SELECT expires_at FROM asks`, &expires)
	if want := departs.Add(-2 * time.Hour); !expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v (two hours before the departure)", expires, want)
	}
}

// offerAndGetID makes the driver's offer and returns the ask.
func (w *offerWorld) offerAndGetID() string {
	w.h.t.Helper()
	if code, body := w.offer(w.driver); code != http.StatusCreated {
		w.h.t.Fatalf("offer: %d %s", code, body)
	}
	var askID string
	w.h.mustQuery(`SELECT id::text FROM asks`, &askID)
	return askID
}

// bookSeat gives passengerID a confirmed seat on a departure, the way a
// finished accept leaves it.
func (h *harness) bookSeat(occurrenceID, passengerID string) {
	h.t.Helper()
	var requestID, askID string
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1::uuid, ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326), ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        $2, $3, 'open', 'matched')
		RETURNING id::text`, &requestID, passengerID, h.clock.Now().Add(time.Hour), h.clock.Now().Add(40*time.Hour))
	h.mustQuery(`
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1::uuid, $2::uuid, 4, 3.0, ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326), 2.50, 1, 'accepted', $3)
		RETURNING id::text`, &askID, requestID, occurrenceID, h.clock.Now().Add(time.Hour))
	h.mustExec(`INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
	                                  pickup_point, dropoff_point)
	            VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 1, 2.50,
	                    ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326), ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326))`,
		occurrenceID, requestID, passengerID, askID)
}

// What stops the driver's accept stops the passenger's too, with the same
// answer: the seat is taken by the same code under the same locks.
func TestAcceptingAnOfferIsRefusedForTheReasonsADriverAcceptIs(t *testing.T) {
	cases := map[string]struct {
		setup    func(w *offerWorld)
		wantCode string
	}{
		"the last seat went": {
			setup: func(w *offerWorld) {
				w.h.mustExec(`UPDATE trip_occurrences SET seats_free = 0 WHERE id = $1::uuid`, w.occurrence)
			},
			wantCode: "no_seats",
		},
		"no room left for the luggage": {
			setup: func(w *offerWorld) {
				// The boot holds one suitcase and somebody else has it.
				w.h.mustExec(`UPDATE trip_occurrences SET luggage = 'suitcase', luggage_capacity = 1 WHERE id = $1::uuid`, w.occurrence)
				w.h.bookSeat(w.occurrence, w.strangerUserID)
				w.h.mustExec(`UPDATE bookings SET luggage_count = 1 WHERE occurrence_id = $1::uuid`, w.occurrence)
				w.h.mustExec(`UPDATE ride_requests SET luggage = 'suitcase', luggage_count = 1 WHERE id = $1::uuid`, w.requestID)
			},
			wantCode: "no_luggage_room",
		},
		"the car already carries as many groups as it can": {
			setup: func(w *offerWorld) {
				w.h.bookSeat(w.occurrence, w.strangerUserID)
				var third string
				w.h.mustQuery(`INSERT INTO users (date_of_birth, phone, name, home_region_id)
					VALUES (DATE '1990-01-01', '+306900001004', 'Τρίτος', (SELECT id FROM regions LIMIT 1))
					RETURNING id::text`, &third)
				w.h.bookSeat(w.occurrence, third)
			},
			wantCode: "party_cap",
		},
		"the passenger holds a seat on another trip at that hour": {
			setup: func(w *offerWorld) {
				other := w.h.occurrenceOf("+306900001005", "Άλλος οδηγός")
				w.h.bookSeat(other, w.passengerID)
			},
			wantCode: "ask_closed",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newOfferWorld(t)
			askID := w.offerAndGetID()
			tc.setup(w)

			rec := w.h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.passenger)
			if rec.Code != http.StatusConflict || w.h.errorCode(rec) != tc.wantCode {
				t.Fatalf("accept: %d %s, want 409 %s", rec.Code, rec.Body.String(), tc.wantCode)
			}
			var mine int
			w.h.mustQuery(`SELECT count(*) FROM bookings WHERE passenger_id = $1::uuid AND occurrence_id = $2::uuid`,
				&mine, w.passengerID, w.occurrence)
			if mine != 0 {
				t.Errorf("%d bookings for a refused accept", mine)
			}
			var reqStatus string
			w.h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1::uuid`, &reqStatus, w.requestID)
			if reqStatus == "matched" {
				t.Errorf("request %s after a refused accept", reqStatus)
			}
		})
	}
}

// The passenger says yes at the moment the driver calls the departure off.
// Whichever the database sees first, the result is one story: a cancelled
// departure with no seat on it, or a seat that was booked and then cancelled
// with the departure. Never a seat on a cancelled car, and never a 500.
func TestAcceptingAnOfferWhileTheDriverCancelsTheDeparture(t *testing.T) {
	for round := 0; round < 8; round++ {
		w := newOfferWorld(t)
		askID := w.offerAndGetID()

		var wg sync.WaitGroup
		start := make(chan struct{})
		var acceptCode, cancelCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			acceptCode = w.h.do(http.MethodPost, "/v1/offers/"+askID+"/accept", nil, w.passenger).Code
		}()
		go func() {
			defer wg.Done()
			<-start
			cancelCode = w.h.do(http.MethodDelete, "/v1/trips/"+w.occurrence, nil, w.driver).Code
		}()
		close(start)
		wg.Wait()

		if cancelCode != http.StatusNoContent {
			t.Fatalf("round %d: cancelling the departure: %d, want 204", round, cancelCode)
		}
		if acceptCode != http.StatusOK && acceptCode != http.StatusConflict {
			t.Fatalf("round %d: accepting: %d, want 200 or 409", round, acceptCode)
		}
		var status string
		var live int
		w.h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &status, w.occurrence)
		w.h.mustQuery(`SELECT count(*) FROM bookings WHERE occurrence_id = $1::uuid AND status = 'confirmed'`, &live, w.occurrence)
		if status != "cancelled" || live != 0 {
			t.Fatalf("round %d: departure %s with %d confirmed seats (accept answered %d)", round, status, live, acceptCode)
		}
		var booked int
		w.h.mustQuery(`SELECT count(*) FROM bookings WHERE occurrence_id = $1::uuid`, &booked, w.occurrence)
		if (acceptCode == http.StatusOK) != (booked == 1) {
			t.Fatalf("round %d: accept answered %d but %d bookings exist", round, acceptCode, booked)
		}
	}
}

// Two passengers say yes to the one seat left. One gets it; the other is told
// it went, and nobody is overbooked.
func TestTwoOffersAcceptedTogetherForTheLastSeat(t *testing.T) {
	for round := 0; round < 8; round++ {
		w := newOfferWorld(t)
		strangerPlan := w.h.weekendPlan(w.strangerUserID)
		now := w.h.clock.Now()
		strangerReq := w.h.weekendRequest(w.strangerUserID, strangerPlan, now.Add(23*time.Hour), now.Add(26*time.Hour), "")

		first := w.offerAndGetID()
		rec := w.h.do(http.MethodPost, "/v1/trips/"+w.occurrence+"/offers",
			map[string]any{"request_id": strangerReq}, w.driver)
		if rec.Code != http.StatusCreated {
			t.Fatalf("second offer: %d %s", rec.Code, rec.Body.String())
		}
		var second string
		w.h.mustQuery(`SELECT id::text FROM asks WHERE request_id = $1::uuid`, &second, strangerReq)
		// One seat left, two offers out for it.
		w.h.mustExec(`UPDATE trip_occurrences SET seats_free = 1 WHERE id = $1::uuid`, w.occurrence)

		codes := make([]int, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, c := range []struct{ ask, token string }{{first, w.passenger}, {second, w.stranger}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i] = w.h.do(http.MethodPost, "/v1/offers/"+c.ask+"/accept", nil, c.token).Code
			}()
		}
		close(start)
		wg.Wait()

		ok, refused := 0, 0
		for _, c := range codes {
			switch c {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				refused++
			default:
				t.Fatalf("round %d: an accept answered %d (%v)", round, c, codes)
			}
		}
		var booked, free int
		w.h.mustQuery(`SELECT count(*) FROM bookings WHERE occurrence_id = $1::uuid AND status = 'confirmed'`, &booked, w.occurrence)
		w.h.mustQuery(`SELECT seats_free FROM trip_occurrences WHERE id = $1::uuid`, &free, w.occurrence)
		if ok != 1 || refused != 1 || booked != 1 || free != 0 {
			t.Fatalf("round %d: codes %v, %d bookings, %d seats free; want one seat, given once", round, codes, booked, free)
		}
	}
}
