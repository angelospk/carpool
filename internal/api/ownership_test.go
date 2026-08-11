package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Every route that names something in its path, tried by somebody with no
// connection to the thing named.
//
// Written as one sweep rather than a test per route because the failure this
// guards against is a route added without an ownership check, and a test per
// route is exactly the thing somebody adding a route forgets to write. The
// table below is meant to be walked next to router.go: a path parameter with no
// line here is a gap.
//
// Two assertions per route, and the second matters more than the first. A
// refusal that still wrote something is not a refusal, and a handler that
// checks ownership after it acts looks identical to one that checks before
// until you count the rows.

// stranger is a fully signed-up account with nothing to do with the fixture.
type ownedWorld struct {
	*harness
	driver, passenger, stranger string
	driverID, passengerID       uuid.UUID
	occurrence                  uuid.UUID
	template                    uuid.UUID
	vehicle                     uuid.UUID
	booking                     uuid.UUID
	pendingAsk                  uuid.UUID
	openRequest                 uuid.UUID
	arrangement                 uuid.UUID
}

func newOwnedWorld(t *testing.T) *ownedWorld {
	t.Helper()
	ctx := context.Background()

	h := newHarness(t)
	driver, passenger, occurrence := h.bookedTrip(t)
	stranger, _ := h.signUp("+306900000099", "Άγνωστος")

	w := &ownedWorld{harness: h, driver: driver, passenger: passenger, stranger: stranger, occurrence: occurrence}
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &w.driverID)
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000032'`, &w.passengerID)
	h.mustQuery(`SELECT template_id FROM trip_occurrences WHERE id = $1`, &w.template, occurrence)
	h.mustQuery(`SELECT id FROM vehicles WHERE owner_id = $1`, &w.vehicle, w.driverID)
	h.mustQuery(`SELECT id FROM bookings WHERE occurrence_id = $1`, &w.booking, occurrence)

	// A second passenger with a live ask, so accept and decline are exercised
	// against an ask that is genuinely answerable rather than one already spent.
	var otherPassenger uuid.UUID
	h.signUp("+306900000098", "Δεύτερη")
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000098'`, &otherPassenger)
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.83,40.85),4326), ST_SetSRID(ST_MakePoint(25.97,40.85),4326),
		        '2026-07-30T04:00:00Z','2026-07-30T06:00:00Z','open','asking')
		RETURNING id`, otherPassenger).Scan(&w.openRequest); err != nil {
		t.Fatalf("seeding the open request: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.83,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.97,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour')
		RETURNING id`, w.openRequest, occurrence).Scan(&w.pendingAsk); err != nil {
		t.Fatalf("seeding the pending ask: %v", err)
	}

	if err := h.pool.QueryRow(ctx, `
		INSERT INTO standing_arrangements (template_id, passenger_id, weekday, seats,
		        pickup_point, dropoff_point, price_eur, until_date)
		VALUES ($1, $2, 4, 1,
		        ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        2.50, '2026-12-31')
		RETURNING id`, w.template, w.passengerID).Scan(&w.arrangement); err != nil {
		t.Fatalf("seeding the arrangement: %v", err)
	}

	return w
}

// fingerprint is every piece of state a refused request must not have moved.
func (w *ownedWorld) fingerprint(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	rows, err := w.pool.Query(context.Background(), `
		SELECT 'ask:'         || id::text, status FROM asks
		UNION ALL SELECT 'booking:'     || id::text, status FROM bookings
		UNION ALL SELECT 'request:'     || id::text, status FROM ride_requests
		UNION ALL SELECT 'occurrence:'  || id::text, status FROM trip_occurrences
		UNION ALL SELECT 'arrangement:' || id::text, status FROM standing_arrangements
		UNION ALL SELECT 'template:'    || id::text, active::text FROM route_templates
		UNION ALL SELECT 'vehicle:'     || id::text, retired_at::text FROM vehicles
		UNION ALL SELECT 'messages',    count(*)::text FROM trip_messages
		UNION ALL SELECT 'ratings',     count(*)::text FROM ratings
		UNION ALL SELECT 'blocks',      count(*)::text FROM blocks
		UNION ALL SELECT 'reports',     count(*)::text FROM reports
		UNION ALL SELECT 'confirms',    count(*)::text FROM bookings
		                                WHERE driver_confirmed OR passenger_confirmed`)
	if err != nil {
		t.Fatalf("fingerprinting: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v *string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("scanning the fingerprint: %v", err)
		}
		if v == nil {
			out[k] = "null"
			continue
		}
		out[k] = *v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the fingerprint: %v", err)
	}
	return out
}

func TestAStrangerCannotTouchAnythingTheyDoNotOwn(t *testing.T) {
	w := newOwnedWorld(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"retire a route", http.MethodDelete, "/v1/me/routes/" + w.template.String(), nil},
		{"make a car the default", http.MethodPost, "/v1/me/vehicles/" + w.vehicle.String() + "/default", nil},
		{"retire a car", http.MethodDelete, "/v1/me/vehicles/" + w.vehicle.String(), nil},
		{"cancel a request", http.MethodDelete, "/v1/requests/" + w.openRequest.String(), nil},
		{"accept an ask", http.MethodPost, "/v1/asks/" + w.pendingAsk.String() + "/accept", nil},
		{"decline an ask", http.MethodPost, "/v1/asks/" + w.pendingAsk.String() + "/decline", nil},
		{"cancel a booking", http.MethodDelete, "/v1/bookings/" + w.booking.String(), nil},
		{"repeat a booking", http.MethodPost, "/v1/bookings/" + w.booking.String() + "/repeat",
			map[string]any{"weekday": 4, "until_date": "2026-12-31"}},
		{"ride again", http.MethodPost, "/v1/bookings/" + w.booking.String() + "/again", nil},
		{"approve an arrangement", http.MethodPost, "/v1/arrangements/" + w.arrangement.String() + "/approve", nil},
		{"decline an arrangement", http.MethodPost, "/v1/arrangements/" + w.arrangement.String() + "/decline", nil},
		{"cancel an arrangement", http.MethodDelete, "/v1/arrangements/" + w.arrangement.String(), nil},
		{"read a trip", http.MethodGet, "/v1/trips/" + w.occurrence.String() + "/", nil},
		{"confirm a trip", http.MethodPost, "/v1/trips/" + w.occurrence.String() + "/confirm", nil},
		{"cancel a trip", http.MethodDelete, "/v1/trips/" + w.occurrence.String() + "/", nil},
		{"rate a trip", http.MethodPost, "/v1/trips/" + w.occurrence.String() + "/rate",
			map[string]any{"stars": 5}},
		{"read the chat", http.MethodGet, "/v1/trips/" + w.occurrence.String() + "/messages", nil},
		{"post to the chat", http.MethodPost, "/v1/trips/" + w.occurrence.String() + "/messages",
			map[string]any{"body": "γεια"}},
	}

	before := w.fingerprint(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := w.do(tc.method, tc.path, tc.body, w.stranger)
			if rec.Code < 400 {
				t.Fatalf("a stranger got %d from %s %s: %s", rec.Code, tc.method, tc.path, rec.Body.String())
			}
			after := w.fingerprint(t)
			for k, want := range before {
				if got := after[k]; got != want {
					t.Fatalf("%s %s was refused with %d but moved %s from %q to %q",
						tc.method, tc.path, rec.Code, k, want, got)
				}
			}
			if len(after) != len(before) {
				t.Fatalf("%s %s was refused with %d but the row count changed: %d -> %d",
					tc.method, tc.path, rec.Code, len(before), len(after))
			}
		})
	}
}

// The same sweep for the moderation queue, which is not about owning a row but
// about holding a grant. A suspension carried out by somebody who is not a
// moderator is the worst of these to get wrong, because it is invisible: the
// person it lands on cannot tell the difference.
func TestAnOrdinaryAccountCannotModerate(t *testing.T) {
	w := newOwnedWorld(t)

	var reportID uuid.UUID
	if err := w.pool.QueryRow(context.Background(), `
		INSERT INTO reports (reporter_id, reported_id, reason)
		VALUES ($1, $2, 'κάτι έγινε') RETURNING id`, w.passengerID, w.driverID).Scan(&reportID); err != nil {
		t.Fatalf("seeding a report: %v", err)
	}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"read the queue", http.MethodGet, "/v1/moderation/reports"},
		{"dismiss a report", http.MethodPost, "/v1/moderation/reports/" + reportID.String() + "/dismiss"},
		{"suspend somebody", http.MethodPost, "/v1/moderation/reports/" + reportID.String() + "/suspend"},
		{"reinstate somebody", http.MethodPost, "/v1/moderation/users/" + w.driverID.String() + "/reinstate"},
		{"read pending meeting points", http.MethodGet, "/v1/moderation/meeting-points"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := w.do(tc.method, tc.path, nil, w.stranger)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s: status %d, want 403: %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			var status, reason string
			w.mustQuery(`SELECT status FROM users WHERE id = $1`, &status, w.driverID)
			w.mustQuery(`SELECT status FROM reports WHERE id = $1`, &reason, reportID)
			if status != "active" || reason != "open" {
				t.Fatalf("after a refused %s the account is %q and the report %q", tc.path, status, reason)
			}
		})
	}
}
