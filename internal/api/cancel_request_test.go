package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// seedRequest puts one ride request in a given state and returns its id.
func (h *harness) seedRequest(t *testing.T, passenger uuid.UUID, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint(25.82,40.85),4326), ST_SetSRID(ST_MakePoint(25.98,40.85),4326),
		        '2026-07-30T04:00:00Z','2026-07-30T06:00:00Z','open',$2)
		RETURNING id`, passenger, status).Scan(&id); err != nil {
		t.Fatalf("seeding a %s request: %v", status, err)
	}
	return id
}

// Cancelling a request that is not yours, or that never existed, used to answer
// 204 — the statement updated nothing and the handler discarded the row count.
//
// Nothing was written either way, so this is not somebody cancelling another
// person's ride. It is worse in a quieter way: the caller is told the thing they
// asked for happened. The app takes 204 as done and navigates home.
func TestCancellingSomebodyElsesRequestSaysSo(t *testing.T) {
	h := newHarness(t)
	h.signUp("+306900000041", "Επιβάτης")
	stranger, _ := h.signUp("+306900000042", "Άγνωστος")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000041'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "open")

	rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, stranger)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body.String())
	}

	var status string
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1`, &status, requestID)
	if status != "open" {
		t.Fatalf("the request is now %q; a stranger must not move it", status)
	}
}

func TestCancellingARequestThatIsNotThereSaysSo(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000043", "Επιβάτης")

	rec := h.do(http.MethodDelete, "/v1/requests/"+uuid.NewString(), nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// The one that matters. A passenger sits on the searching screen while the
// asking runs; a driver accepts; the passenger presses cancel a moment later.
// Answering 204 sends them home believing they are not going, while a driver is
// expecting them at a meeting point and a seat is held in their name.
func TestCancellingAMatchedRequestIsRefusedLoudly(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000044", "Επιβάτης")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000044'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "matched")

	rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "already_matched" {
		t.Fatalf("error code %q, want already_matched: the app has to send them to the booking instead", code)
	}

	var status string
	h.mustQuery(`SELECT status FROM ride_requests WHERE id = $1`, &status, requestID)
	if status != "matched" {
		t.Fatalf("the request is now %q, want matched", status)
	}
}

// Cancelling twice is not a failure. The second press of a button, and a retry
// after a dropped response, both arrive here and both mean the same thing.
func TestCancellingTwiceIsStillCancelled(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000045", "Επιβάτης")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000045'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "open")

	for i := range 2 {
		rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("attempt %d: status %d, want 204: %s", i+1, rec.Code, rec.Body.String())
		}
	}
}

// An expired request is a dead one, and pressing cancel on a screen that has
// been open too long should not be an error.
func TestCancellingAnExpiredRequestIsAccepted(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000046", "Επιβάτης")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000046'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "expired")

	rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

// The live ask goes with the request, which is what stops a driver accepting a
// ride the passenger has withdrawn.
func TestCancellingARequestKillsItsLiveAsk(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000047", "Επιβάτης")
	driver, _, occurrence := h.bookedTrip(t)
	_ = driver

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000047'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "asking")

	var askID uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour')
		RETURNING id`, requestID, occurrence).Scan(&askID); err != nil {
		t.Fatalf("seeding the ask: %v", err)
	}

	if rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
	}

	var status string
	h.mustQuery(`SELECT status FROM asks WHERE id = $1`, &status, askID)
	if status != "cancelled" {
		t.Fatalf("the ask is %q, want cancelled: a driver must not be able to accept a withdrawn ride", status)
	}
}

// The seats a pending ask held go back when the passenger withdraws it, and
// whoever was waiting behind them is woken now rather than at the next hourly
// reconcile. A one-seat trip otherwise sat unoffered for up to an hour
// (QA 2026-10-16, B1).
func TestCancellingARequestWakesWhoeverWaitsForItsTrip(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000048", "Επιβάτης")
	_, _, occurrence := h.bookedTrip(t)

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000048'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "asking")
	if _, err := h.pool.Exec(context.Background(), `
		INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                  suggested_price_eur, itinerary_version, status, expires_at)
		VALUES ($1, $2, 5, 2, ST_SetSRID(ST_MakePoint(25.82,40.85),4326),
		        ST_SetSRID(ST_MakePoint(25.98,40.85),4326), 2.50, 0, 'pending', now() + interval '1 hour')`,
		requestID, occurrence); err != nil {
		t.Fatalf("seeding the ask: %v", err)
	}
	var before int
	h.mustQuery(`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'
	              AND payload->>'occurrence_id' = $1::text`, &before, occurrence)

	if rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
	}

	var after int
	h.mustQuery(`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'
	              AND payload->>'occurrence_id' = $1::text`, &after, occurrence)
	if after != before+1 {
		t.Fatalf("wakes for the trip went %d -> %d, want one more", before, after)
	}
}

// A request that was never put to anybody frees nothing, and wakes nothing.
func TestCancellingAnOpenRequestWakesNothing(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000049", "Επιβάτης")

	var passengerID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000049'`, &passengerID)
	requestID := h.seedRequest(t, passengerID, "open")
	var before int
	h.mustQuery(`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`, &before)

	if rec := h.do(http.MethodDelete, "/v1/requests/"+requestID.String(), nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
	}

	var after int
	h.mustQuery(`SELECT count(*) FROM outbox WHERE kind = 'wake_waiting'`, &after)
	if after != before {
		t.Fatalf("cancelling an open request queued %d wakes", after-before)
	}
}
