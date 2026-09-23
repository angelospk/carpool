package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/store/testdb"
)

// anotherDriverOnTheRoad publishes a second departure on the fixture corridor,
// half an hour after the booked one, and returns its id.
func (h *harness) anotherDriverOnTheRoad(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	h.signUp("+306900000041", "Δεύτερος Οδηγός")
	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000041'`, &driverID)
	h.mustExec(`INSERT INTO driver_profiles (user_id, car_model, car_plate) VALUES ($1, 'Polo', 'XYZ-9876')`, driverID)
	testdb.GiveEveryDriverACar(t, h.pool)

	var templateID, occurrenceID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, osrm_data_version)
		VALUES ($1, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
			ST_SetSRID(ST_MakePoint(25.80,40.85),4326), ST_SetSRID(ST_MakePoint(26.00,40.85),4326),
			'Αλεξανδρούπολη', 'Φέρες', ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 3, 'test')
		RETURNING id`, driverID).Scan(&templateID); err != nil {
		t.Fatalf("seeding the second template: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1, (SELECT vehicle_id FROM route_templates WHERE id = $1), '2026-07-30',
			'2026-07-30T05:00:00Z', '2026-07-30T05:30:00Z', 3, 3, 1,
			ST_GeomFromText('LINESTRING(25.80 40.85, 26.00 40.85)',4326), 17, 20, 'formula')
		RETURNING id`, templateID).Scan(&occurrenceID); err != nil {
		t.Fatalf("seeding the second occurrence: %v", err)
	}
	return occurrenceID
}

// A passenger whose driver takes the seat back sees that the search restarted
// by itself, and which cars are still on the road — without being handed the
// names of drivers who have agreed to nothing.
func TestAStrandedPassengerSeesTheCarsStillOnTheirRoad(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.anotherDriverOnTheRoad(t)

	var bookingID, requestID string
	h.mustQuery(`SELECT id::text, request_id::text FROM bookings WHERE occurrence_id = $1`,
		&bookingID, &requestID, occurrenceID)

	if rec := h.do(http.MethodDelete, "/v1/bookings/"+bookingID, nil, driver); rec.Code/100 != 2 {
		t.Fatalf("driver cancels the seat: %d %s", rec.Code, rec.Body.String())
	}
	// What the dispatcher does with the reopen the cancellation queued. The
	// API harness runs no dispatcher, and what it does is asserted in its own
	// package.
	h.mustExec(`UPDATE ride_requests SET status = 'open' WHERE id = $1`, requestID)

	var mine struct {
		Requests []struct {
			ID       string `json:"id"`
			Stranded bool   `json:"stranded"`
		} `json:"requests"`
	}
	h.decode(h.do(http.MethodGet, "/v1/requests/mine", nil, passenger), &mine)
	if len(mine.Requests) != 1 || !mine.Requests[0].Stranded {
		t.Fatalf("requests/mine = %+v, want one stranded request", mine.Requests)
	}

	rec := h.do(http.MethodGet, "/v1/requests/"+requestID+"/alternatives", nil, passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("alternatives: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var out struct {
		Alternatives []struct {
			DepartEarliest time.Time `json:"depart_earliest"`
			OriginLabel    string    `json:"origin_label"`
			SeatsFree      int       `json:"seats_free"`
		} `json:"alternatives"`
	}
	h.decode(rec, &out)
	// The driver who cancelled was already asked, so only the other car is left.
	if len(out.Alternatives) != 1 || out.Alternatives[0].OriginLabel != "Αλεξανδρούπολη" ||
		!out.Alternatives[0].DepartEarliest.Equal(time.Date(2026, time.July, 30, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("alternatives = %s", body)
	}
	for _, leak := range []string{"Δεύτερος", "XYZ-9876", "driver", "phone"} {
		if strings.Contains(body, leak) {
			t.Errorf("an unasked driver's %q reached the passenger: %s", leak, body)
		}
	}
}

func TestSomebodyElsesRequestHasNoAlternatives(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	var requestID string
	h.mustQuery(`SELECT request_id::text FROM bookings WHERE occurrence_id = $1`, &requestID, occurrenceID)

	for _, path := range []string{
		"/v1/requests/" + requestID + "/alternatives",
		"/v1/requests/" + uuid.NewString() + "/alternatives",
	} {
		if rec := h.do(http.MethodGet, path, nil, driver); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}

// A request that got its ride has nothing left to find, and a list of other
// cars under it would read as a search still running.
func TestAMatchedRequestListsNoAlternatives(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)
	h.anotherDriverOnTheRoad(t)
	var requestID string
	h.mustQuery(`SELECT request_id::text FROM bookings WHERE occurrence_id = $1`, &requestID, occurrenceID)

	var out struct {
		Alternatives []any `json:"alternatives"`
	}
	h.decode(h.do(http.MethodGet, "/v1/requests/"+requestID+"/alternatives", nil, passenger), &out)
	if len(out.Alternatives) != 0 {
		t.Fatalf("a matched request listed %d alternatives", len(out.Alternatives))
	}
}
