package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Retiring a route used to leave up to three weeks of departures standing, and
// the driver was told to cancel them one at a time: a doubled route stayed
// doubled in everybody's search until each morning was called off by hand.
// Retiring now takes the empty ones with it. A departure somebody has a seat
// on, or is being asked about, stays: calling that off is the driver's
// separate decision, and the passenger is told when it happens.
func TestRetiringARouteCallsOffItsEmptyDepartures(t *testing.T) {
	h := newHarness(t)
	driver, _, bookedID, _ := bookedTrip(t, h, "+306900001101", "+306900001102")

	var templateID string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1::uuid`, &templateID, bookedID)

	departure := func(day int) string {
		var id string
		at := h.clock.Now().Add(time.Duration(day) * 24 * time.Hour)
		h.mustQuery(`
			INSERT INTO trip_occurrences (
				template_id, vehicle_id, service_date, depart_earliest, depart_latest,
				seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
			SELECT $1::uuid, vehicle_id, $2::date, $2, $3, 3, 3, 1, route_geom, route_km, route_min, pricing_mode
			  FROM route_templates WHERE id = $1::uuid
			RETURNING id::text`, &id, templateID, at, at.Add(30*time.Minute))
		return id
	}
	empty := departure(2)
	asked := departure(3)
	past := departure(-2)

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900001102'`, &passengerID)
	var requestID string
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1::uuid, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        now(), now() + interval '1 hour', 'open', 'asking')
		RETURNING id::text`, &requestID, passengerID)
	h.mustExec(`
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1::uuid, $2::uuid, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', $3)`,
		requestID, asked, h.clock.Now().Add(time.Hour))

	if rec := h.do(http.MethodDelete, "/v1/me/routes/"+templateID, nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("retiring: %d %s", rec.Code, rec.Body.String())
	}

	for id, want := range map[string]string{
		empty:    "cancelled",
		bookedID: "scheduled",
		asked:    "scheduled",
		past:     "scheduled",
	} {
		var got string
		h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &got, id)
		if got != want {
			t.Errorf("departure %s: status %q, want %q", id, got, want)
		}
	}
}

// The confirmation before retiring says how many departures will stay, so the
// list has to know which those are.
func TestMyRoutesCountsTheDeparturesRetiringWouldKeep(t *testing.T) {
	h := newHarness(t)
	driver, _, tripID, _ := bookedTrip(t, h, "+306900001103", "+306900001104")
	h.mustExec(`
		INSERT INTO route_schedules (template_id, kind, weekdays, depart_earliest, depart_latest)
		SELECT template_id, 'recurring', ARRAY[1,2,3], '07:30', '08:00'
		  FROM trip_occurrences WHERE id = $1::uuid`, tripID)

	rec := h.do(http.MethodGet, "/v1/me/routes", nil, driver)
	var got struct {
		Routes []struct {
			Upcoming int `json:"upcoming_trips"`
			Kept     int `json:"kept_trips"`
		} `json:"routes"`
	}
	h.decode(rec, &got)
	if len(got.Routes) != 1 || got.Routes[0].Kept != 1 {
		t.Fatalf("got %+v, want one route keeping its one booked departure", got.Routes)
	}
}

// «Αλλαγή» opens the publish form filled in with the route, so the list has
// to carry where the route goes, not only what its ends are called.
func TestMyRoutesCarriesTheEndsAsCoordinates(t *testing.T) {
	h := newHarness(t)
	token, _ := driverWithRoute(t, h, "+306900001105")

	rec := h.do(http.MethodGet, "/v1/me/routes", nil, token)
	var got struct {
		Routes []struct {
			OriginLon      float64 `json:"origin_lon"`
			OriginLat      float64 `json:"origin_lat"`
			DestinationLon float64 `json:"destination_lon"`
			DestinationLat float64 `json:"destination_lat"`
		} `json:"routes"`
	}
	h.decode(rec, &got)
	if len(got.Routes) != 1 {
		t.Fatalf("got %+v", got.Routes)
	}
	r := got.Routes[0]
	if r.OriginLon != 25.8743 || r.OriginLat != 40.8476 || r.DestinationLon != 26.1717 || r.DestinationLat != 40.8975 {
		t.Errorf("ends: %+v", r)
	}
}

// «Αλλαγή» publishes a replacement and retires this route, so everything the
// driver chose has to come back with it. Without the group, a route offered
// only to a group was republished to everybody.
func TestMyRoutesCarriesEveryChoiceAChangeMustKeep(t *testing.T) {
	h := newHarness(t)
	token, routeID := driverWithRoute(t, h, "+306900001106")
	h.mustExec(`INSERT INTO groups (name, join_code, created_by)
		SELECT 'Νοσοκομείο', 'KEEPGRP01', driver_id FROM route_templates WHERE id = $1::uuid`, routeID)
	h.mustExec(`UPDATE route_templates
		   SET group_id = (SELECT id FROM groups WHERE join_code = 'KEEPGRP01'),
		       luggage = 'cabin', luggage_capacity = 2, contribution_share = 0.5,
		       allow_stops = false, detour_km = 7
		 WHERE id = $1::uuid`, routeID)

	rec := h.do(http.MethodGet, "/v1/me/routes", nil, token)
	var got struct {
		Routes []struct {
			GroupID           *string `json:"group_id"`
			VehicleID         *string `json:"vehicle_id"`
			Luggage           string  `json:"luggage"`
			LuggageCapacity   int     `json:"luggage_capacity"`
			ContributionShare float64 `json:"contribution_share"`
			AllowStops        bool    `json:"allow_stops"`
			DetourKm          float64 `json:"detour_km"`
		} `json:"routes"`
	}
	h.decode(rec, &got)
	if len(got.Routes) != 1 {
		t.Fatalf("got %+v", got.Routes)
	}
	r := got.Routes[0]
	if r.GroupID == nil || r.VehicleID == nil || r.Luggage != "cabin" || r.LuggageCapacity != 2 ||
		r.ContributionShare != 0.5 || r.AllowStops || r.DetourKm != 7 {
		t.Errorf("choices not carried: %+v", r)
	}
}

// Review found this race: a driver being asked holds the departure's lock while
// the ask is written. A retire that read "nobody is being asked" before the ask
// committed, then waited for the lock, cancelled a departure somebody was being
// offered. The retire has to look again once it holds the lock.
func TestRetiringWaitsForAnAskBeingWrittenAndKeepsItsDeparture(t *testing.T) {
	h := newHarness(t)
	driver, _, _, _ := bookedTrip(t, h, "+306900001107", "+306900001108")

	var templateID, departure, passengerID, requestID string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences LIMIT 1`, &templateID)
	at := h.clock.Now().Add(72 * time.Hour)
	h.mustQuery(`
		INSERT INTO trip_occurrences (
			template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		SELECT $1::uuid, vehicle_id, $2::date, $2, $3, 3, 3, 1, route_geom, route_km, route_min, pricing_mode
		  FROM route_templates WHERE id = $1::uuid
		RETURNING id::text`, &departure, templateID, at, at.Add(30*time.Minute))
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900001108'`, &passengerID)
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1::uuid, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        now(), now() + interval '1 hour', 'open', 'asking')
		RETURNING id::text`, &requestID, passengerID)

	// The matcher, mid-ask: the departure is locked and the ask written, but
	// not yet committed.
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM trip_occurrences WHERE id = $1::uuid FOR UPDATE`, departure); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1::uuid, $2::uuid, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour')`,
		requestID, departure); err != nil {
		t.Fatal(err)
	}

	done := make(chan int)
	go func() {
		done <- h.do(http.MethodDelete, "/v1/me/routes/"+templateID, nil, driver).Code
	}()

	// The retire must be waiting on the lock, not finished.
	select {
	case code := <-done:
		t.Fatalf("the retire finished (%d) while the departure was locked by an ask being written", code)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != http.StatusNoContent {
		t.Fatalf("retire: %d", code)
	}

	var status string
	h.mustQuery(`SELECT status FROM trip_occurrences WHERE id = $1::uuid`, &status, departure)
	if status != "scheduled" {
		t.Errorf("a departure somebody was being asked about was %s", status)
	}
}
