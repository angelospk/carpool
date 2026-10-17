package api_test

import (
	"net/http"
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
