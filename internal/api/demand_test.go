package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"carpool/internal/store"
)

// A driver with nothing published has no reason to publish, and the app had no
// answer to "would anybody even ride with me". The rows were already there.
//
// What leaves the server is a corridor and a count. These tests are mostly
// about what does *not* leave it: this is the one endpoint where one person's
// stated journey is shown to somebody who is not part of it.

type corridor struct {
	OriginLabel      string `json:"origin_label"`
	DestinationLabel string `json:"destination_label"`
	People           int    `json:"people"`
}

func (h *harness) demand(token string) ([]corridor, int) {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/demand", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("GET /v1/demand = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Corridors  []corridor `json:"corridors"`
		PeriodDays int        `json:"period_days"`
	}
	h.decode(rec, &body)
	return body.Corridors, body.PeriodDays
}

// wanting inserts an open request from a new person along the given corridor.
func (h *harness) wanting(phone, from, to string) {
	h.t.Helper()
	h.signUp(phone, "Επιβάτης")
	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, phone)
	h.mustExec(`
		INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
		                           window_start, window_end, seats, kind, status)
		VALUES ($1::uuid,
		        ST_SetSRID(ST_MakePoint(25.8743, 40.8476), 4326),
		        ST_SetSRID(ST_MakePoint(26.1717, 40.8975), 4326),
		        $2, $3, $4, $5, 1, 'open', 'open')`,
		passengerID, from, to, h.clock.Now().Add(time.Hour), h.clock.Now().Add(3*time.Hour))
}

// The sentence the endpoint exists to make possible.
func TestDemandNamesTheCorridorAndCountsThePeople(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000301", "Οδηγός")

	// store.DemandFloor different people, because a corridor below the privacy
	// floor is not reported at all.
	for i := 0; i < store.DemandFloor; i++ {
		h.wanting(fmt.Sprintf("+30690000031%d", i), "Αλεξανδρούπολη", "Ορεστιάδα")
	}

	got, period := h.demand(token)
	if len(got) != 1 {
		t.Fatalf("got %d corridors, want 1: %+v", len(got), got)
	}
	if got[0].OriginLabel != "Αλεξανδρούπολη" || got[0].DestinationLabel != "Ορεστιάδα" {
		t.Errorf("corridor = %q → %q", got[0].OriginLabel, got[0].DestinationLabel)
	}
	if got[0].People != store.DemandFloor {
		t.Errorf("people = %d, want %d", got[0].People, store.DemandFloor)
	}
	// The screen says "this week", so the server has to be the one that decides
	// what a week is rather than the app guessing.
	if period != 7 {
		t.Errorf("period_days = %d, want 7", period)
	}
}

// Nothing waiting is an ordinary answer and not an error. The screen renders
// nothing at all, which it cannot do if this is a 404.
func TestDemandWithNobodyWaitingIsAnEmptyList(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000304", "Οδηγός")

	got, _ := h.demand(token)
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// Demand is other people's journeys. It is not public.
func TestDemandNeedsAToken(t *testing.T) {
	h := newHarness(t)
	h.wanting("+306900000305", "Αλεξανδρούπολη", "Ορεστιάδα")

	rec := h.do(http.MethodGet, "/v1/demand", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/demand without a token = %d, want 401", rec.Code)
	}
}

// The privacy contract as it appears on the wire. A field added to the handler
// reaches every signed-in user, so the test reads the raw JSON rather than a
// struct that would silently drop anything extra.
func TestDemandPutsNothingIdentifyingOnTheWire(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000306", "Οδηγός")
	var phones []string
	for i := 0; i < store.DemandFloor; i++ {
		phone := fmt.Sprintf("+30690000032%d", i)
		phones = append(phones, phone)
		h.wanting(phone, "Αλεξανδρούπολη", "Ορεστιάδα")
	}

	rec := h.do(http.MethodGet, "/v1/demand", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/demand = %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Corridors []map[string]json.RawMessage `json:"corridors"`
	}
	h.decode(rec, &body)
	if len(body.Corridors) != 1 {
		t.Fatalf("got %d corridors, want 1", len(body.Corridors))
	}
	allowed := map[string]bool{"origin_label": true, "destination_label": true, "people": true}
	for k := range body.Corridors[0] {
		if !allowed[k] {
			t.Errorf("demand exposes %q: a corridor and a count is the whole of what may leave here", k)
		}
	}

	// Belt and braces against a field being renamed into something that looks
	// innocent: neither the passenger's name nor their phone may appear
	// anywhere in the response at all.
	// Every phone that was seeded, not the first couple: a leak of the third
	// person is a leak.
	for _, secret := range append([]string{"Επιβάτης"}, phones...) {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("demand response contains %q", secret)
		}
	}
}

// The other half of the same gap, on the passenger's side: whether anybody
// drives this road at all, told apart from whether they have been asked yet.
func TestMyRequestsSaysWhetherAnybodyDrivesTheRoad(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000308", "Επιβάτης")

	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &passengerID, "+306900000308")
	h.openRequest(passengerID, "open")

	onCorridor := func() int {
		h.t.Helper()
		rec := h.do(http.MethodGet, "/v1/requests/mine", nil, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/requests/mine = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Requests []struct {
				DriversOnCorridor int `json:"drivers_on_corridor"`
			} `json:"requests"`
		}
		h.decode(rec, &body)
		if len(body.Requests) != 1 {
			t.Fatalf("got %d requests, want 1", len(body.Requests))
		}
		return body.Requests[0].DriversOnCorridor
	}

	if got := onCorridor(); got != 0 {
		t.Fatalf("with nobody publishing, drivers_on_corridor = %d, want 0", got)
	}

	// occurrenceOf publishes a route along exactly this corridor.
	h.occurrenceOf("+306900000309", "Οδηγός")

	if got := onCorridor(); got != 1 {
		t.Fatalf("with one driver publishing the road, drivers_on_corridor = %d, want 1", got)
	}
}

// The privacy floor at the boundary that matters. The store test covers the
// threshold itself; this one is here because the promise is made to users in
// docs/legal/privacy-el.md and it is the endpoint that keeps it.
func TestDemandDoesNotReportACorridorBelowTheFloor(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000330", "Οδηγός")

	for i := 0; i < store.DemandFloor-1; i++ {
		h.wanting(fmt.Sprintf("+30690000033%d", i+1), "Πέπλος", "Ορεστιάδα")
	}

	got, _ := h.demand(token)
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing below a floor of %d", got, store.DemandFloor)
	}
}
