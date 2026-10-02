package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type supplyBody struct {
	Status string `json:"status"`
}

// Αλεξανδρούπολη → Φέρες, which is the road occurrenceOf publishes.
func (h *harness) supply(token string, date string) (supplyBody, string) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/supply", map[string]any{
		"origin_lon": 25.8743, "origin_lat": 40.8476,
		"destination_lon": 26.1717, "destination_lat": 40.8975,
		"date": date,
	}, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("supply: %d %s", rec.Code, rec.Body.String())
	}
	var b supplyBody
	h.decode(rec, &b)
	return b, rec.Body.String()
}

func (h *harness) tomorrowInAthens() string {
	loc, _ := time.LoadLocation("Europe/Athens")
	return h.clock.Now().Add(24 * time.Hour).In(loc).Format("2006-01-02")
}

// What somebody searching sees before asking: whether at least three drivers
// go their way that day. Never which, when, whose, or exactly how many: an
// exact count moves by one when one driver is added to or taken out of it, and
// the caller controls things (a block, a group) that do exactly that.
func TestSupplySaysHowManyDriversOnlyAboveTheFloor(t *testing.T) {
	h := newHarness(t)
	passenger, _ := h.signUp("+306900000601", "Επιβάτης")
	day := h.tomorrowInAthens()

	for i := 0; i < 2; i++ {
		h.occurrenceOf(fmt.Sprintf("+30690000061%d", i), "Οδηγός")
	}
	got, _ := h.supply(passenger, day)
	if got.Status != "unknown" {
		t.Fatalf("two drivers: %+v, want nothing said", got)
	}

	h.occurrenceOf("+306900000619", "Οδηγός")
	got, raw := h.supply(passenger, day)
	if got.Status != "several" {
		t.Fatalf("three drivers: %+v", got)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 {
		t.Errorf("the answer carries more than a status: %s", raw)
	}
	for _, leak := range []string{"Οδηγός", "07:", "seats", "id"} {
		if strings.Contains(raw, leak) {
			t.Errorf("the answer carries %q: %s", leak, raw)
		}
	}
}

// One driver with many departures is one driver, and the filters that keep a
// passenger from being asked about somebody keep that somebody out of the
// count too: a block either way, the passenger's own routes, and a paused
// driver.
func TestSupplyCountsPeopleAndHonoursTheMatchersRules(t *testing.T) {
	h := newHarness(t)
	passenger, _ := h.signUp("+306900000701", "Επιβάτης")
	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000701'`, &passengerID)
	day := h.tomorrowInAthens()

	occ := h.occurrenceOf("+306900000710", "Α")
	// A second route of the same driver, departing the same day.
	var second string
	h.mustQuery(`INSERT INTO route_templates (driver_id, vehicle_id, origin, destination, origin_label,
	               destination_label, route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version)
	             SELECT driver_id, vehicle_id, origin, destination, origin_label, destination_label,
	                    route_geom, route_km, route_min, seats, pricing_mode, osrm_data_version
	             FROM route_templates
	             WHERE id = (SELECT template_id FROM trip_occurrences WHERE id = $1::uuid)
	             RETURNING id::text`, &second, occ)
	h.mustExec(`INSERT INTO trip_occurrences (template_id, vehicle_id, service_date, depart_earliest, depart_latest,
	              seats_total, seats_free, template_revision, route_geom, route_km, route_min, pricing_mode)
	            SELECT $2::uuid, vehicle_id, service_date, depart_earliest + interval '3 hours',
	                   depart_latest + interval '3 hours', seats_total, seats_free, template_revision,
	                   route_geom, route_km, route_min, pricing_mode
	            FROM trip_occurrences WHERE id = $1::uuid`, occ, second)
	h.occurrenceOf("+306900000711", "Β")
	h.occurrenceOf("+306900000712", "Γ")
	if got, _ := h.supply(passenger, day); got.Status != "several" {
		t.Fatalf("three people, four departures: %+v", got)
	}
	// Two departures of Α are one driver: without Β, Α's two do not make three.
	var bID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000711'`, &bID)

	// A driver who blocked the passenger is not theirs to count.
	h.mustExec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1::uuid, $2::uuid)`, bID, passengerID)
	if got, _ := h.supply(passenger, day); got.Status != "unknown" {
		t.Errorf("a driver who blocked the passenger still counted: %+v", got)
	}
	h.mustExec(`DELETE FROM blocks`)

	// A block the passenger made does not change the answer. If it did, block
	// one known driver and ask again would say whether they drive this road.
	h.mustExec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1::uuid, $2::uuid)`, passengerID, bID)
	if got, _ := h.supply(passenger, day); got.Status != "several" {
		t.Errorf("the passenger's own block moved the count: %+v", got)
	}
	h.mustExec(`DELETE FROM blocks`)

	// Seats spoken for by a live ask are not free: with Β's only seats asked
	// about, Β has no room to offer.
	h.mustExec(`UPDATE trip_occurrences SET seats_free = 1
	            WHERE template_id IN (SELECT id FROM route_templates WHERE driver_id = $1::uuid)`, bID)
	var bOcc string
	h.mustQuery(`SELECT o.id::text FROM trip_occurrences o JOIN route_templates t ON t.id = o.template_id
	             WHERE t.driver_id = $1::uuid`, &bOcc, bID)
	h.ask(h.openRequest(passengerID, "asking"), bOcc, "pending", 10*time.Minute)
	if got, _ := h.supply(passenger, day); got.Status != "unknown" {
		t.Errorf("a seat spoken for still counted: %+v", got)
	}
	h.mustExec(`DELETE FROM asks`)

	h.mustExec(`UPDATE driver_profiles SET notifications_muted = true
	            WHERE user_id = (SELECT id FROM users WHERE phone = '+306900000712')`)
	if got, _ := h.supply(passenger, day); got.Status != "unknown" {
		t.Errorf("a paused driver still counted: %+v", got)
	}
}

// The day asked about is a day somebody could search for: today or up to two
// weeks out, in Athens. Anything else is refused rather than answered, so the
// count cannot be walked through the calendar.
func TestSupplyOnlyAnswersForTheNextTwoWeeks(t *testing.T) {
	h := newHarness(t)
	passenger, _ := h.signUp("+306900000801", "Επιβάτης")
	for _, bad := range []string{"2020-01-01", "2099-01-01", "tomorrow", ""} {
		rec := h.do(http.MethodPost, "/v1/supply", map[string]any{
			"origin_lon": 25.8743, "origin_lat": 40.8476,
			"destination_lon": 26.1717, "destination_lat": 40.8975,
			"date": bad,
		}, passenger)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("date %q: %d, want 400", bad, rec.Code)
		}
	}
	if rec := h.do(http.MethodPost, "/v1/supply", map[string]any{"date": h.tomorrowInAthens()}, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d, want 401", rec.Code)
	}
}
