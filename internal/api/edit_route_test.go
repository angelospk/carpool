package api_test

import (
	"context"
	"net/http"
	"testing"

	"carpool/internal/api"
	"carpool/internal/jobs"
)

func withMaterializer(d *api.Deps) {
	d.Materializer = jobs.NewMaterializer(d.Pool, d.Clock, d.Location)
}

// The publish form for the route bookedTrip makes, every day, at the time the
// time and seats given.
func editBody(earliest, latest string, seats int) map[string]any {
	return map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1717, "destination_lat": 40.8975,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Φέρες",
		"seats": seats, "kind": "recurring", "weekdays": []int{1, 2, 3, 4, 5, 6, 7},
		"depart_earliest": earliest, "depart_latest": latest,
	}
}

// Changing a route with a booked morning used to publish a second route and
// retire the first. The booked morning kept its departure and the new route
// added another at the new time: two cars where the driver drives one
// (QA 2026-10-16). An edit changes the route in place. The booked departure
// keeps what was agreed; every other one takes the new terms.
func TestEditingARouteKeepsTheBookedDepartureAsAgreed(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	driver, _, tripID, bookingID := bookedTrip(t, h, "+306900001101", "+306900001102")

	var templateID string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1::uuid`, &templateID, tripID)
	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		VALUES ($1::uuid, 'recurring', ARRAY[1,2,3,4,5,6,7], '15:00', '15:30')`, templateID)
	h.mustExec(`UPDATE route_schedules SET effective_from = '2026-07-01' WHERE template_id = $1::uuid`, templateID)
	var before string
	h.mustQuery(`SELECT to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI') FROM trip_occurrences WHERE id = $1::uuid`, &before, tripID)
	if err := jobs.NewMaterializer(h.pool, h.clock, testLocation()).Run(context.Background()); err != nil {
		t.Fatalf("materialising: %v", err)
	}

	rec := h.do(http.MethodPut, "/v1/me/routes/"+templateID, editBody("16:00", "16:30", 2), driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		KeptTrips int `json:"kept_trips"`
	}
	h.decode(rec, &got)
	if got.KeptTrips != 1 {
		t.Errorf("kept_trips %d, want 1", got.KeptTrips)
	}

	// The booked departure: same time, same seats, same passenger.
	var at string
	var seats int
	var status, booking string
	h.mustQuery(`
		SELECT to_char(o.depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI'), o.seats_total, o.status, b.status
		  FROM trip_occurrences o JOIN bookings b ON b.occurrence_id = o.id
		 WHERE o.id = $1::uuid AND b.id = $2::uuid`, &at, &seats, &status, &booking, tripID, bookingID)
	if at != before || seats != 3 || status != "scheduled" || booking != "confirmed" {
		t.Errorf("the booked departure changed: %s, %d seats, %s, booking %s", at, seats, status, booking)
	}

	// One departure that day, not two.
	var thatDay int
	h.mustQuery(`
		SELECT count(*) FROM trip_occurrences
		 WHERE template_id = $1::uuid AND status = 'scheduled'
		   AND service_date = (SELECT service_date FROM trip_occurrences WHERE id = $2::uuid)`,
		&thatDay, templateID, tripID)
	if thatDay != 1 {
		t.Errorf("%d departures on the booked day, want 1", thatDay)
	}

	// Every other day that has not left moved.
	var stale, moved int
	h.mustQuery(`
		SELECT count(*) FILTER (WHERE to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI') <> '16:00' OR seats_total <> 2),
		       count(*) FILTER (WHERE to_char(depart_earliest AT TIME ZONE 'Europe/Athens', 'HH24:MI') = '16:00' AND seats_total = 2)
		  FROM trip_occurrences
		 WHERE template_id = $1::uuid AND status = 'scheduled' AND id <> $2::uuid
		   AND depart_earliest > $3`,
		&stale, &moved, templateID, tripID, h.clock.Now())
	if stale != 0 || moved == 0 {
		t.Errorf("free departures: %d kept the old terms, %d took the new", stale, moved)
	}

	// Still one route, still offered, now saying 16:00 and 2 seats.
	rec = h.do(http.MethodGet, "/v1/me/routes", nil, driver)
	var list myRoutes
	h.decode(rec, &list)
	if len(list.Routes) != 1 || list.Routes[0].ID != templateID ||
		list.Routes[0].DepartEarliest != "16:00" || list.Routes[0].Seats != 2 {
		t.Errorf("routes after the edit: %+v", list.Routes)
	}
}

// Other ends are another journey. The departures already booked were agreed
// between these two places, so the app publishes a new route instead.
func TestAnEditCannotMoveTheEndsOfARoute(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	token, routeID := driverWithRoute(t, h, "+306900001103")

	body := editBody("09:00", "09:30", 2)
	body["destination_lon"], body["destination_lat"] = 26.4, 41.0
	rec := h.do(http.MethodPut, "/v1/me/routes/"+routeID, body, token)
	if rec.Code != http.StatusConflict || h.errorCode(rec) != "route_ends_changed" {
		t.Fatalf("moved end: %d %s", rec.Code, rec.Body.String())
	}

	var seats int
	h.mustQuery(`SELECT seats FROM route_templates WHERE id = $1::uuid`, &seats, routeID)
	if seats != 3 {
		t.Errorf("a refused edit changed the route: %d seats", seats)
	}
}

func TestOnlyTheDriverCanEditTheirRoute(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	_, routeID := driverWithRoute(t, h, "+306900001104")
	other, _ := driverWithRoute(t, h, "+306900001105")

	for name, id := range map[string]string{"another driver's": routeID, "unknown": "00000000-0000-0000-0000-000000000000"} {
		rec := h.do(http.MethodPut, "/v1/me/routes/"+id, editBody("09:00", "09:30", 2), other)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s route: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

// A retired route stays retired: an edit is not a way back.
func TestARetiredRouteCannotBeEdited(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	token, routeID := driverWithRoute(t, h, "+306900001106")
	if rec := h.do(http.MethodDelete, "/v1/me/routes/"+routeID, nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("retire: %d", rec.Code)
	}

	rec := h.do(http.MethodPut, "/v1/me/routes/"+routeID, editBody("09:00", "09:30", 2), token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("edit of a retired route: %d %s", rec.Code, rec.Body.String())
	}
}

// Changing one route into a copy of another is the duplicate the publish form
// refuses, reached the other way.
func TestAnEditCannotCopyAnotherRoute(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	token, routeID := driverWithRoute(t, h, "+306900001107")
	var otherID string
	h.mustQuery(`
		INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		                             route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version)
		SELECT driver_id, vehicle_id, origin, destination, origin_label, destination_label,
		       route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version
		  FROM route_templates WHERE id = $1::uuid
		RETURNING id::text`, &otherID, routeID)
	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		VALUES ($1::uuid, 'recurring', ARRAY[1,2,3,4,5,6,7], '09:00', '09:30')`, otherID)

	rec := h.do(http.MethodPut, "/v1/me/routes/"+routeID, editBody("09:00", "09:30", 2), token)
	if rec.Code != http.StatusConflict || h.errorCode(rec) != "duplicate_route" {
		t.Fatalf("edit into a copy: %d %s", rec.Code, rec.Body.String())
	}
	// Saving a route unchanged is not a copy of itself.
	rec = h.do(http.MethodPut, "/v1/me/routes/"+otherID, editBody("09:00", "09:30", 2), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("saving unchanged: %d %s", rec.Code, rec.Body.String())
	}
}

// A departure the driver called off is an exception in the schedule. The edit
// replaced the schedule with the form's, which has none, and the next run put
// the departure back.
func TestAnEditKeepsTheDaysTheDriverCalledOff(t *testing.T) {
	h := newHarnessDeps(t, false, nil, withMaterializer)
	driver, _, tripID, _ := bookedTrip(t, h, "+306900001108", "+306900001109")
	var templateID string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1::uuid`, &templateID, tripID)
	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest, effective_from)
		VALUES ($1::uuid, 'recurring', ARRAY[1,2,3,4,5,6,7], '03:00', '03:30', '2026-07-01')`, templateID)
	if rec := h.do(http.MethodDelete, "/v1/trips/"+tripID, nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}

	if rec := h.do(http.MethodPut, "/v1/me/routes/"+templateID, editBody("03:00", "03:30", 2), driver); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}

	var status string
	h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &status, tripID)
	if status != "cancelled" {
		t.Fatalf("the called-off departure is %q after an edit", status)
	}
}
