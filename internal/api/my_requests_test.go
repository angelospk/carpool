package api_test

import (
	"net/http"
	"testing"
	"time"
)

// myRequest is what GET /v1/requests/mine hands back for one open request.
type myRequest struct {
	ID               string    `json:"id"`
	OriginLabel      string    `json:"origin_label"`
	DestinationLabel string    `json:"destination_label"`
	WindowStart      time.Time `json:"window_start"`
	WindowEnd        time.Time `json:"window_end"`
	Seats            int       `json:"seats"`
	Status           string    `json:"status"`
	DriversAsked     int       `json:"drivers_asked"`
	DriversDeclined  int       `json:"drivers_declined"`
	AskingNow        bool      `json:"asking_now"`
}

func (h *harness) myRequests(token string) []myRequest {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/requests/mine", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("GET /v1/requests/mine = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Requests []myRequest `json:"requests"`
	}
	h.decode(rec, &body)
	return body.Requests
}

// openRequest inserts a request the passenger is waiting on. Written straight
// to the table rather than posted: POST /v1/requests also starts the matching,
// and these tests are about what the waiting looks like, not how it began.
func (h *harness) openRequest(passengerID, status string) string {
	h.t.Helper()
	var id string
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        'Αλεξανδρούπολη', 'Φέρες', $2, $3, 1, 'open', $4)
		RETURNING id::text`, &id, passengerID,
		h.clock.Now().Add(time.Hour), h.clock.Now().Add(3*time.Hour), status)
	return id
}

// occurrenceOf builds a driver with a car, a route and one departure, and
// returns the departure. It is the thing an ask points at.
func (h *harness) occurrenceOf(phone, name string) string {
	h.t.Helper()
	driver, _ := h.signUp(phone, name)
	if rec := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, driver); rec.Code != http.StatusOK {
		h.t.Fatalf("driver profile: %d %s", rec.Code, rec.Body.String())
	}

	var driverID, templateID, occurrenceID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &driverID, phone)
	h.mustQuery(`
		INSERT INTO route_templates (
			driver_id, vehicle_id, origin, destination, origin_label, destination_label,
			route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version)
		VALUES ($1::uuid, (SELECT id FROM vehicles WHERE owner_id = $1 AND is_default),
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        'Αλεξανδρούπολη', 'Φέρες',
		        ST_SetSRID(ST_MakeLine(ST_MakePoint(25.8743, 40.8476), ST_MakePoint(26.1717, 40.8975)), 4326),
		        31.2, 28, 3, 'formula', 'test')
		RETURNING id::text`, &templateID, driverID)

	// A schedule as well as a departure. A published route has one, and the
	// corridor count reads it to tell a route that can still run from one that
	// merely was never retired.
	h.mustExec(`
		INSERT INTO route_schedules
		(template_id, kind, weekdays, depart_earliest, depart_latest, effective_from)
		VALUES ($1::uuid, 'recurring', '{1,2,3,4,5}', '07:30', '08:00', current_date)`,
		templateID)

	depart := h.clock.Now().Add(24 * time.Hour)
	h.mustQuery(`
		INSERT INTO trip_occurrences (
			template_id, vehicle_id, service_date, depart_earliest, depart_latest,
			seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
		VALUES ($1::uuid, (SELECT vehicle_id FROM route_templates WHERE id = $1), $2::date, $2, $3, 3, 3, 1,
		        ST_SetSRID(ST_MakeLine(ST_MakePoint(25.8743, 40.8476), ST_MakePoint(26.1717, 40.8975)), 4326),
		        31.2, 28, 'formula')
		RETURNING id::text`, &occurrenceID, templateID, depart, depart.Add(30*time.Minute))
	return occurrenceID
}

func (h *harness) ask(requestID, occurrenceID, status string, expiresIn time.Duration) {
	h.t.Helper()
	h.mustExec(`
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km,
		                  pickup_point, dropoff_point, suggested_price_eur,
		                  itinerary_version, status, expires_at)
		VALUES ($1::uuid, $2::uuid, 4, 3.0,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        2.50, 1, $3, $4)`,
		requestID, occurrenceID, status, h.clock.Now().Add(expiresIn))
}

// A request nobody has been asked about yet is the first thing a passenger
// sees, and the honest answer is zero — not a spinner.
func TestMyRequestsCountsNothingBeforeAnybodyIsAsked(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000201", "Επιβάτης")

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, "+306900000201")
	h.openRequest(passengerID, "open")

	got := h.myRequests(token)
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	if got[0].DriversAsked != 0 || got[0].DriversDeclined != 0 || got[0].AskingNow {
		t.Fatalf("a request nobody was asked about reads as %+v", got[0])
	}
	if got[0].OriginLabel != "Αλεξανδρούπολη" || got[0].Seats != 1 {
		t.Fatalf("the request does not describe itself: %+v", got[0])
	}
}

// Two drivers, one of whom said no and one of whom is still looking at it.
func TestMyRequestsCountsDriversAndLiveAsks(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000202", "Επιβάτης")

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, "+306900000202")
	requestID := h.openRequest(passengerID, "asking")

	first := h.occurrenceOf("+306900000203", "Πρώτος")
	second := h.occurrenceOf("+306900000204", "Δεύτερος")
	h.ask(requestID, first, "declined", -time.Hour)
	h.ask(requestID, second, "pending", time.Hour)

	got := h.myRequests(token)[0]
	if got.DriversAsked != 2 {
		t.Fatalf("drivers_asked = %d, want 2", got.DriversAsked)
	}
	if got.DriversDeclined != 1 {
		t.Fatalf("drivers_declined = %d, want 1", got.DriversDeclined)
	}
	if !got.AskingNow {
		t.Fatalf("a pending ask that has not expired is somebody reading it now")
	}
}

// A pending ask whose expiry has passed is nobody reading it. Saying "we are
// asking a driver" about a notification that timed out is the one thing this
// screen exists to stop.
func TestMyRequestsDoesNotCallAnExpiredAskLive(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000205", "Επιβάτης")

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, "+306900000205")
	requestID := h.openRequest(passengerID, "asking")
	h.ask(requestID, h.occurrenceOf("+306900000206", "Οδηγός"), "pending", -time.Minute)

	if got := h.myRequests(token)[0]; got.AskingNow {
		t.Fatalf("an ask that expired is still being reported as live")
	}
}

// Everything that is no longer in flight belongs somewhere else — a trip, or
// nothing at all — and none of it belongs in a list of things being waited on.
func TestMyRequestsLeavesOutWhatIsNoLongerWaiting(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000207", "Επιβάτης")

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, "+306900000207")
	for _, status := range []string{"matched", "cancelled", "expired"} {
		h.openRequest(passengerID, status)
	}

	// One whose window has already closed, which is open and yet over.
	var stale string
	h.mustQuery(`
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                           seats, kind, status)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        $2, $3, 1, 'open', 'open')
		RETURNING id::text`, &stale, passengerID,
		h.clock.Now().Add(-3*time.Hour), h.clock.Now().Add(-time.Hour))

	if got := h.myRequests(token); len(got) != 0 {
		t.Fatalf("got %d requests, want none: %+v", len(got), got)
	}
}

// Somebody else's request is somebody else's business.
func TestMyRequestsShowsOnlyTheCallersOwn(t *testing.T) {
	h := newHarness(t)
	mine, _ := h.signUp("+306900000208", "Εγώ")
	h.signUp("+306900000209", "Άλλος")

	var otherID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &otherID, "+306900000209")
	h.openRequest(otherID, "open")

	if got := h.myRequests(mine); len(got) != 0 {
		t.Fatalf("got somebody else's %d requests", len(got))
	}
}

func TestMyRequestsNeedsAToken(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodGet, "/v1/requests/mine", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}
