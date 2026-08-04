package api_test

import (
	"net/http"
	"testing"
	"time"
)

// A driver, a passenger with a seat, and the departure they share.
func bookedTrip(t *testing.T, h *harness, driverPhone, passengerPhone string) (driver, passenger, tripID, bookingID string) {
	t.Helper()
	driver, _ = h.signUp(driverPhone, "Οδηγός")
	passenger, _ = h.signUp(passengerPhone, "Επιβάτης")

	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, driver); rec.Code != http.StatusOK {
		t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	var driverID, passengerID, templateID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &driverID, driverPhone)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, passengerPhone)

	h.mustQuery(`
		INSERT INTO route_templates (
			driver_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        'Αλεξανδρούπολη', 'Φέρες',
		        ST_SetSRID(ST_MakeLine(ST_MakePoint(25.8743, 40.8476), ST_MakePoint(26.1717, 40.8975)), 4326),
		        31.2, 28, 3, 'formula', 'test')
		RETURNING id::text`, &templateID, driverID)

	// Tomorrow on the harness clock, so it has not departed.
	depart := h.clock.Now().Add(24 * time.Hour)
	h.mustQuery(`
		INSERT INTO trip_occurrences (
			template_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1::uuid, $2::date, $2, $3, 3, 2, 1,
		        ST_SetSRID(ST_MakeLine(ST_MakePoint(25.8743, 40.8476), ST_MakePoint(26.1717, 40.8975)), 4326),
		        31.2, 28, 'formula')
		RETURNING id::text`, &tripID, templateID, depart, depart.Add(30*time.Minute))

	// A booking has to come from somewhere: either a matched request or a
	// standing arrangement. The arrangement is the shorter of the two to build.
	var arrangementID string
	h.mustQuery(`
		INSERT INTO standing_arrangements (template_id, passenger_id, weekday, seats,
		                                   pickup_point, dropoff_point, price_eur, status, until_date)
		VALUES ($1::uuid, $2::uuid, 1, 1,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        2.50, 'active', $3::date)
		RETURNING id::text`, &arrangementID, templateID, passengerID, depart.AddDate(0, 1, 0))

	h.mustQuery(`
		INSERT INTO bookings (occurrence_id, passenger_id, arrangement_id, seats, status, price_eur,
		                      pickup_point, dropoff_point)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'confirmed', 2.50,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326))
		RETURNING id::text`, &bookingID, tripID, passengerID, arrangementID)

	return driver, passenger, tripID, bookingID
}

func TestADriverCanCallOffADeparture(t *testing.T) {
	// The thing a driver actually needs before anyone is in the car, and the
	// thing the trip screen was not offering.
	h := newHarness(t)
	driver, _, tripID, bookingID := bookedTrip(t, h, "+306900001001", "+306900001002")

	if rec := h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}

	var tripStatus, bookingStatus string
	h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &tripStatus, tripID)
	h.mustQuery(`SELECT status FROM bookings WHERE id = $1::uuid`, &bookingStatus, bookingID)

	if tripStatus != "cancelled" {
		t.Errorf("trip status %q", tripStatus)
	}
	// Through the same status the passenger-facing path uses, so a passenger
	// sees a cancellation the rest of the app has heard of.
	if bookingStatus != "cancelled_by_driver" {
		t.Errorf("booking status %q", bookingStatus)
	}
}

func TestThePassengerIsToldTheirRideIsOff(t *testing.T) {
	// The whole reason this is not just an UPDATE. Somebody is planning to be
	// at a corner tomorrow morning.
	h := newHarness(t)
	driver, _, tripID, bookingID := bookedTrip(t, h, "+306900001003", "+306900001004")

	h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, driver)

	var queued int
	h.mustQuery(`SELECT count(*) FROM outbox WHERE idempotency_key = $1`,
		&queued, "booking_cancelled:"+bookingID)
	if queued != 1 {
		t.Errorf("%d notifications queued for the cancelled booking", queued)
	}
}

func TestAPassengerCannotCallOffTheWholeTrip(t *testing.T) {
	// They can give back their own seat; the departure is not theirs to end.
	h := newHarness(t)
	_, passenger, tripID, _ := bookedTrip(t, h, "+306900001005", "+306900001006")

	if rec := h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, passenger); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}

	var status string
	h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &status, tripID)
	if status == "cancelled" {
		t.Error("a passenger cancelled the whole departure")
	}
}

func TestCancellingTwiceIsNotAnError(t *testing.T) {
	h := newHarness(t)
	driver, _, tripID, _ := bookedTrip(t, h, "+306900001007", "+306900001008")

	for i := range 2 {
		if rec := h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, driver); rec.Code != http.StatusNoContent {
			t.Errorf("attempt %d: %d", i+1, rec.Code)
		}
	}
}

func TestADepartedTripCannotBeCalledOff(t *testing.T) {
	// Once it has set off, whether it happened is a different question, with a
	// different answer and different consequences for the rating.
	h := newHarness(t)
	driver, _, tripID, _ := bookedTrip(t, h, "+306900001009", "+306900001010")

	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $2
	             WHERE id = $1::uuid`, tripID, h.clock.Now().Add(-time.Hour))

	if rec := h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, driver); rec.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", rec.Code)
	}
}
