package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/store/testdb"
)

// bookedTrip drives the real flow to a confirmed booking and returns the
// participants' tokens plus the occurrence id.
func (h *harness) bookedTrip(t *testing.T) (driverToken, passengerToken string, occurrenceID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	driverToken, _ = h.signUp("+306900000031", "Οδηγός")
	passengerToken, _ = h.signUp("+306900000032", "Επιβάτης")

	var driverID, passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000032'`, &passengerID)

	h.mustExec(`INSERT INTO driver_profiles (user_id, car_model, car_plate)
	            VALUES ($1, 'Yaris', 'ABC-1234')`, driverID)
	testdb.GiveEveryDriverACar(t, h.pool)

	var templateID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default), ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'A','B', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, driverID).Scan(&templateID); err != nil {
		t.Fatalf("seeding template: %v", err)
	}

	if err := h.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-30', '2026-07-30T04:30:00Z', '2026-07-30T05:00:00Z',
			3, 2, 1, ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, templateID).Scan(&occurrenceID); err != nil {
		t.Fatalf("seeding occurrence: %v", err)
	}

	var requestID, askID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
			'2026-07-30T04:00:00Z','2026-07-30T06:00:00Z','open','matched')
		RETURNING id`, passengerID).Scan(&requestID); err != nil {
		t.Fatalf("seeding request: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2,5,2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'accepted', now()+interval '1 hour')
		RETURNING id`, requestID, occurrenceID).Scan(&askID); err != nil {
		t.Fatalf("seeding ask: %v", err)
	}
	h.mustExec(`INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
	                                  pickup_point, dropoff_point)
	            VALUES ($1, $2,$3,$4,1,2.50, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
	                    ST_SetSRID(ST_MakePoint(25.98,40.85),4326))`,
		occurrenceID, requestID, passengerID, askID)

	return driverToken, passengerToken, occurrenceID
}

// departed moves the fixture trip into the past, since a ride can only be
// confirmed once it was due to leave.
// departed moves the trip into the past — by the harness clock, not the
// database's. The handler decides "has it set off" against the injected clock,
// so using now() here compared two different times: the tests passed or failed
// on the time of day the suite happened to run.
func (h *harness) departed() {
	now := h.clock.Now()
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $1, depart_latest = $2`,
		now.Add(-2*time.Hour), now.Add(-90*time.Minute))
}

func TestUpcomingTripsShowsTheTripToBothSides(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)

	for name, token := range map[string]string{"driver": driver, "passenger": passenger} {
		t.Run(name, func(t *testing.T) {
			rec := h.do(http.MethodGet, "/v1/trips/upcoming", nil, token)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Trips []struct {
					ID   string `json:"id"`
					Role string `json:"role"`
				} `json:"trips"`
			}
			h.decode(rec, &body)
			if len(body.Trips) != 1 {
				t.Fatalf("got %d trips, want 1", len(body.Trips))
			}
			if body.Trips[0].ID != occurrenceID.String() {
				t.Errorf("trip id = %s, want %s", body.Trips[0].ID, occurrenceID)
			}
			if body.Trips[0].Role != name {
				t.Errorf("role = %q, want %q", body.Trips[0].Role, name)
			}
		})
	}
}

func TestTripDetailIsHiddenFromStrangers(t *testing.T) {
	h := newHarness(t)
	_, _, occurrenceID := h.bookedTrip(t)
	stranger, _ := h.signUp("+306900000039", "Ξένος")

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, stranger)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a non-participant", rec.Code)
	}
}

// The plate is deliberately withheld until a booking exists, per SPEC.md.
func TestTripDetailRevealsThePlateToTheMatchedPassenger(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, passenger)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Driver struct {
			Name     string `json:"name"`
			CarModel string `json:"car_model"`
			CarPlate string `json:"car_plate"`
		} `json:"driver"`
		Passengers []struct {
			Name string `json:"name"`
		} `json:"passengers"`
	}
	h.decode(rec, &body)
	if body.Driver.CarPlate != "ABC-1234" {
		t.Errorf("car_plate = %q, want it revealed after accept", body.Driver.CarPlate)
	}
	if len(body.Passengers) != 1 {
		t.Errorf("got %d passengers, want 1", len(body.Passengers))
	}
}

func TestConfirmMarksOnlyTheCallersSide(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	h.departed()

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	var driverConfirmed, passengerConfirmed bool
	h.mustQuery(`SELECT driver_confirmed, passenger_confirmed FROM bookings`,
		&driverConfirmed, &passengerConfirmed)
	if !driverConfirmed {
		t.Error("the driver's confirmation was not recorded")
	}
	if passengerConfirmed {
		t.Error("the driver's confirmation must not confirm for the passenger")
	}
}

func TestConfirmIsIdempotent(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	h.departed()

	for range 2 {
		if rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver); rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	}
}

// A ride counts as completed only when both sides say it happened.
func TestBookingCompletesWhenBothSidesConfirm(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.departed()

	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)

	var status string
	h.mustQuery(`SELECT status FROM bookings`, &status)
	if status != "completed" {
		t.Fatalf("booking status = %q, want completed once both sides confirmed", status)
	}
}

func TestRatingRequiresACompletedRide(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate",
		map[string]any{"stars": 5}, passenger)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 before the ride is completed", rec.Code)
	}
}

func TestRatingIsStoredAgainstTheOtherParty(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.departed()
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate",
		map[string]any{"stars": 5, "tag": "punctual"}, passenger)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	var stars int
	var rateePhone string
	h.mustQuery(`SELECT r.stars, u.phone FROM ratings r JOIN users u ON u.id = r.ratee_id`,
		&stars, &rateePhone)
	if stars != 5 {
		t.Errorf("stars = %d, want 5", stars)
	}
	if rateePhone != "+306900000031" {
		t.Errorf("the rating landed on %s, want the driver", rateePhone)
	}
}

func TestRatingCannotBeSubmittedTwice(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.departed()
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate", map[string]any{"stars": 5}, passenger)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate",
		map[string]any{"stars": 1}, passenger)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a second rating", rec.Code)
	}
}

func TestRatingRejectsAnOutOfRangeScore(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.departed()
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)

	for _, stars := range []int{0, 6, -1} {
		rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate",
			map[string]any{"stars": stars}, passenger)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("stars=%d: status = %d, want 400", stars, rec.Code)
		}
	}
}

func TestChatIsReadableAndWritableByParticipants(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	path := "/v1/trips/" + occurrenceID.String() + "/messages"

	if rec := h.do(http.MethodPost, path, map[string]any{"body": "Καλημέρα!"}, passenger); rec.Code != http.StatusCreated {
		t.Fatalf("posting: status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodGet, path, nil, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("reading: status = %d", rec.Code)
	}
	var body struct {
		Messages []struct {
			Body string `json:"body"`
		} `json:"messages"`
	}
	h.decode(rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].Body != "Καλημέρα!" {
		t.Fatalf("messages = %+v, want the one that was posted", body.Messages)
	}
}

func TestChatIsClosedToStrangers(t *testing.T) {
	h := newHarness(t)
	_, _, occurrenceID := h.bookedTrip(t)
	stranger, _ := h.signUp("+306900000038", "Ξένος")
	path := "/v1/trips/" + occurrenceID.String() + "/messages"

	if rec := h.do(http.MethodGet, path, nil, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("reading: status = %d, want 404", rec.Code)
	}
	if rec := h.do(http.MethodPost, path, map[string]any{"body": "hi"}, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("posting: status = %d, want 404", rec.Code)
	}
}

func TestChatRejectsAnEmptyMessage(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/messages",
		map[string]any{"body": "   "}, passenger)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBlockingAUserTakesEffectImmediately(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)
	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)

	rec := h.do(http.MethodPost, "/v1/users/"+driverID.String()+"/block", nil, passenger)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	var blocked int
	h.mustQuery(`SELECT count(*) FROM blocks WHERE blocked_id = $1`, &blocked, driverID)
	if blocked != 1 {
		t.Fatalf("blocks = %d, want 1", blocked)
	}
}

func TestReportingAUserStoresAnAuditRecord(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)
	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)

	rec := h.do(http.MethodPost, "/v1/users/"+driverID.String()+"/report",
		map[string]any{"reason": "Δεν εμφανίστηκε"}, passenger)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	var reason string
	h.mustQuery(`SELECT reason FROM reports`, &reason)
	if reason != "Δεν εμφανίστηκε" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestYouCannotBlockYourself(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)
	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000032'`, &passengerID)

	rec := h.do(http.MethodPost, "/v1/users/"+passengerID.String()+"/block", nil, passenger)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- regression tests for issues found in adversarial review ---

// Confirming must not be possible before the trip has actually departed,
// otherwise a pair could complete and rate a ride next week.
func TestConfirmIsRejectedBeforeTheTripHasDeparted(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	// The fixture trip departs 2026-07-30; the harness clock is 2026-07-29.
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = now() + interval '2 days',
	                                        depart_latest   = now() + interval '2 days'`)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a trip that has not departed", rec.Code)
	}
	var confirmed bool
	h.mustQuery(`SELECT driver_confirmed FROM bookings`, &confirmed)
	if confirmed {
		t.Error("a future trip was confirmed anyway")
	}
}

// A driver carrying two passengers must be able to rate each of them, not just
// whichever booking happens to be oldest.
func TestDriverCanRateEachPassengerSeparately(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	second := h.addSecondPassenger(t, occurrenceID)
	h.departed()
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, second)

	for i, target := range []string{"+306900000032", "+306900000033"} {
		var targetID uuid.UUID
		h.mustQuery(`SELECT id FROM users WHERE phone = $1`, &targetID, target)

		rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/rate",
			map[string]any{"stars": 4, "ratee_id": targetID.String()}, driver)
		if rec.Code != http.StatusCreated {
			t.Fatalf("rating passenger %d: status = %d, body %s", i, rec.Code, rec.Body.String())
		}
	}

	var ratings int
	h.mustQuery(`SELECT count(*) FROM ratings`, &ratings)
	if ratings != 2 {
		t.Fatalf("stored %d ratings, want one per passenger", ratings)
	}
}

// A cursor with a time but no id must not silently skip every message sharing
// that timestamp.
func TestChatRejectsAHalfSpecifiedCursor(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)
	path := "/v1/trips/" + occurrenceID.String() + "/messages"

	rec := h.do(http.MethodGet, path+"?after_time=2026-07-29T12:00:00Z", nil, passenger)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a cursor missing after_id", rec.Code)
	}
}

// A posted message must look the same live as it does on refetch.
func TestPostedMessageCarriesTheSenderName(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/messages",
		map[string]any{"body": "γεια"}, passenger)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		SenderName string `json:"sender_name"`
	}
	h.decode(rec, &body)
	if body.SenderName != "Επιβάτης" {
		t.Fatalf("sender_name = %q, want the poster's name", body.SenderName)
	}
}

// addSecondPassenger books another rider onto the same trip and returns their
// access token.
func (h *harness) addSecondPassenger(t *testing.T, occurrenceID uuid.UUID) string {
	t.Helper()
	token, _ := h.signUp("+306900000033", "Δεύτερος")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000033'`, &passengerID)

	var requestID, askID uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.83,40.85),4326), ST_SetSRID(ST_MakePoint(25.97,40.85),4326),
			'2026-07-30T04:00:00Z','2026-07-30T06:00:00Z','open','matched')
		RETURNING id`, passengerID).Scan(&requestID); err != nil {
		t.Fatalf("seeding second request: %v", err)
	}
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
			suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2,5,2, ST_SetSRID(ST_MakePoint(25.83,40.85),4326),
			ST_SetSRID(ST_MakePoint(25.97,40.85),4326), 2.50, 0, 'accepted', now()+interval '1 hour')
		RETURNING id`, requestID, occurrenceID).Scan(&askID); err != nil {
		t.Fatalf("seeding second ask: %v", err)
	}
	h.mustExec(`INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur,
	                                  pickup_point, dropoff_point)
	            VALUES ($1, $2,$3,$4,1,2.50, ST_SetSRID(ST_MakePoint(25.83,40.85),4326),
	                    ST_SetSRID(ST_MakePoint(25.97,40.85),4326))`,
		occurrenceID, requestID, passengerID, askID)
	return token
}

// "3.50 €" answers nothing. The question a passenger has is whether that is a
// fare, and the answer is the division — the whole car's fuel over every seat
// plus the driver, floored, plus the barriers they are in the car for.
func TestAPassengerCanSeeWhatTheyArePayingFor(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	// The receipt the ask flow freezes beside the price, restated here as
	// arithmetic that actually holds: 5.6 l/100km at 1.729 €/l over 103 km is
	// 9.97 €, over four (three seats and the driver) is 2.49, floored to 2.00.
	h.mustExec(`UPDATE bookings SET price_eur = 2.00, pricing_basis = $1::jsonb`,
		`{"v":2,"consumption_l_100km":5.6,"consumption_source":"dataset",
		  "fuel_price_eur":1.729,"fuel":"diesel","region_name":"Περιφερειακή Ενότητα Έβρου",
		  "trip_km":103,"wear_factor":1.0,"offered_seats":3,"free_below_km":5,
		  "toll_share_cents":0,"mode":"formula","cost_eur":9.97,"seat_price_cents":200}`)

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		PriceEUR  float64 `json:"price_eur"`
		Breakdown *struct {
			FuelCostCents int     `json:"fuel_cost_cents"`
			Divisor       int     `json:"divisor"`
			RoundedCents  int     `json:"rounded_cents"`
			TotalCents    int     `json:"total_cents"`
			TripKm        float64 `json:"trip_km"`
			Fuel          string  `json:"fuel"`
		} `json:"price_breakdown"`
	}
	h.decode(rec, &body)

	if body.Breakdown == nil {
		t.Fatalf("no breakdown, so the price is unexplainable: %s", rec.Body.String())
	}
	if body.Breakdown.Divisor < 2 {
		t.Errorf("divisor = %d; it is every offered seat plus the driver", body.Breakdown.Divisor)
	}
	if body.Breakdown.FuelCostCents <= 0 || body.Breakdown.TripKm <= 0 {
		t.Errorf("the breakdown has no arithmetic in it: %+v", *body.Breakdown)
	}
	// The whole point: what the passenger pays is a fraction of the trip.
	if body.Breakdown.TotalCents >= body.Breakdown.FuelCostCents {
		t.Errorf("a seat at %d cents on a %d cent trip is not a share of it",
			body.Breakdown.TotalCents, body.Breakdown.FuelCostCents)
	}
}

// The receipt says what one person paid and what their driver's car drinks. It
// is not a thing to hand the rest of the car, and the driver's own view of the
// trip has no seat to explain.
func TestTheDriverDoesNotGetSomebodyElsesReceipt(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)

	// The passenger's seat does have a receipt, so this cannot pass merely
	// because there was nothing to hand over.
	h.mustExec(`UPDATE bookings SET pricing_basis = $1::jsonb`,
		`{"v":2,"consumption_l_100km":5.6,"fuel_price_eur":1.729,"trip_km":103,
		  "wear_factor":1.0,"offered_seats":3,"free_below_km":5,"toll_share_cents":0,
		  "mode":"formula","cost_eur":9.97,"seat_price_cents":200}`)

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	h.decode(rec, &body)
	if _, present := body["price_breakdown"]; present {
		t.Error("the driver was shown a passenger's receipt")
	}
}
