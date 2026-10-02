package api_test

import (
	"net/http"
	"testing"
	"time"
)

type pendingAsk struct {
	ID               string  `json:"id"`
	PassengerName    string  `json:"passenger_name"`
	OriginLabel      string  `json:"origin_label"`
	DestinationLabel string  `json:"destination_label"`
	DetourMin        float64 `json:"detour_min"`
	DetourKm         float64 `json:"detour_km"`
	PriceEUR         float64 `json:"price_eur"`
	Seats            int     `json:"seats"`
	DepartEarliest   string  `json:"depart_earliest"`
	ExpiresAt        string  `json:"expires_at"`
}

func (h *harness) pendingAsks(token string) []pendingAsk {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/asks/pending", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("pending asks: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Asks []pendingAsk `json:"asks"`
	}
	h.decode(rec, &body)
	return body.Asks
}

// A driver was asked by push and nowhere else: the notification opened the
// front page, and nothing in the app listed the question. Missing the push
// meant missing the passenger.
func TestDriverSeesTheAsksWaitingOnThem(t *testing.T) {
	h := newHarness(t)
	occurrence := h.occurrenceOf("+306900000301", "Οδηγός")
	driver, _ := h.signUp("+306900000301", "Οδηγός")
	passenger, _ := h.signUp("+306900000302", "Ελένη")
	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000302'`, &passengerID)

	live := h.openRequest(passengerID, "asking")
	h.ask(live, occurrence, "pending", 10*time.Minute)
	// One that lapsed and one already answered are not waiting on anybody.
	h.ask(h.openRequest(passengerID, "open"), occurrence, "pending", -time.Minute)
	h.ask(h.openRequest(passengerID, "open"), occurrence, "declined", 10*time.Minute)

	got := h.pendingAsks(driver)
	if len(got) != 1 {
		t.Fatalf("pending asks = %d, want only the live one: %+v", len(got), got)
	}
	a := got[0]
	if a.PassengerName != "Ελένη" || a.OriginLabel != "Αλεξανδρούπολη" || a.DestinationLabel != "Φέρες" {
		t.Errorf("ask = %+v, want who is asking and for which journey", a)
	}
	if a.DetourMin != 4 || a.DetourKm != 3 || a.PriceEUR != 2.5 || a.Seats != 1 {
		t.Errorf("ask numbers = %+v", a)
	}

	// The passenger is not the one being asked.
	if other := h.pendingAsks(passenger); len(other) != 0 {
		t.Errorf("passenger sees %d asks meant for the driver", len(other))
	}
}

// Opening one ask by id is what a notification tap does. Only the driver it
// was sent to may read it; anybody else gets the same 404 as a wrong id.
func TestOneAskOpensOnlyForItsDriver(t *testing.T) {
	h := newHarness(t)
	occurrence := h.occurrenceOf("+306900000311", "Οδηγός")
	driver, _ := h.signUp("+306900000311", "Οδηγός")
	stranger, _ := h.signUp("+306900000313", "Άλλος")
	var passengerID string
	h.signUp("+306900000312", "Ελένη")
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000312'`, &passengerID)
	h.ask(h.openRequest(passengerID, "asking"), occurrence, "pending", 10*time.Minute)

	var askID string
	h.mustQuery(`SELECT id::text FROM asks`, &askID)

	rec := h.do(http.MethodGet, "/v1/asks/"+askID, nil, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("driver GET ask: %d %s", rec.Code, rec.Body.String())
	}
	var a pendingAsk
	h.decode(rec, &a)
	if a.ID != askID || a.PassengerName != "Ελένη" {
		t.Errorf("ask = %+v", a)
	}

	if rec := h.do(http.MethodGet, "/v1/asks/"+askID, nil, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("stranger GET ask: %d, want 404", rec.Code)
	}
}

// A block made after the ask, either way, takes it off the driver's screen:
// accepting would be refused, and the passenger's name should not stay in
// front of somebody they blocked.
func TestABlockTakesTheAskAway(t *testing.T) {
	for _, driverBlocks := range []bool{true, false} {
		h := newHarness(t)
		occurrence := h.occurrenceOf("+306900000321", "Οδηγός")
		driver, _ := h.signUp("+306900000321", "Οδηγός")
		h.signUp("+306900000322", "Ελένη")
		var passengerID, driverID string
		h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000322'`, &passengerID)
		h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000321'`, &driverID)
		h.ask(h.openRequest(passengerID, "asking"), occurrence, "pending", 10*time.Minute)
		var askID string
		h.mustQuery(`SELECT id::text FROM asks`, &askID)

		blocker, blocked := passengerID, driverID
		if driverBlocks {
			blocker, blocked = driverID, passengerID
		}
		h.mustExec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1::uuid, $2::uuid)`, blocker, blocked)

		if got := h.pendingAsks(driver); len(got) != 0 {
			t.Errorf("driverBlocks=%v: still listed: %+v", driverBlocks, got)
		}
		if rec := h.do(http.MethodGet, "/v1/asks/"+askID, nil, driver); rec.Code != http.StatusNotFound {
			t.Errorf("driverBlocks=%v: GET ask = %d, want 404", driverBlocks, rec.Code)
		}
	}
}
