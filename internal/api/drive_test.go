package api_test

import (
	"net/http"
	"testing"
	"time"
)

// The driver's trip screen pins the departure, and the trip then says so to
// the driver and to each passenger: the minute, and for a passenger when the
// car reaches their own pickup.
func TestADriverPinsTheMinuteAndTheTripShowsIt(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000501", "Οδηγός")
	driver, _ := h.signUp("+306900000501", "Οδηγός")
	opens := h.clock.Now().Add(10 * time.Minute)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1`,
		occ, opens, opens.Add(30*time.Minute))
	departs := opens.Add(-5 * time.Minute)

	rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/drive", map[string]any{
		"departs_at": departs.Format(time.RFC3339), "detour_min": 3,
	}, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("drive: %d %s", rec.Code, rec.Body.String())
	}

	var trip struct {
		DepartEarliest    time.Time  `json:"depart_earliest"`
		DriveSetAt        *time.Time `json:"drive_set_at"`
		ScheduledEarliest *time.Time `json:"scheduled_earliest"`
		DetourMin         int        `json:"detour_min"`
	}
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ, nil, driver), &trip)
	if !trip.DepartEarliest.Equal(departs) || trip.DriveSetAt == nil ||
		trip.ScheduledEarliest == nil || !trip.ScheduledEarliest.Equal(opens) || trip.DetourMin != 3 {
		t.Fatalf("trip after the drive: %+v", trip)
	}
}

func TestDriveRefusalsSayWhy(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000502", "Οδηγός")
	driver, _ := h.signUp("+306900000502", "Οδηγός")
	stranger, _ := h.signUp("+306900000503", "Κάποιος")
	opens := h.clock.Now().Add(10 * time.Minute)
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2, depart_latest = $3 WHERE id = $1`,
		occ, opens, opens.Add(30*time.Minute))

	cases := []struct {
		name   string
		token  string
		body   map[string]any
		status int
		code   string
	}{
		{"a stranger", stranger, map[string]any{"departs_at": opens.Format(time.RFC3339)}, 404, "not_found"},
		{"no time", driver, map[string]any{}, 400, "invalid_request"},
		{"an hour from now", driver, map[string]any{"departs_at": opens.Add(time.Hour).Format(time.RFC3339)}, 422, "bad_departure"},
		{"a long detour", driver, map[string]any{"departs_at": opens.Format(time.RFC3339), "detour_min": 45}, 422, "bad_detour"},
	}
	for _, c := range cases {
		rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/drive", c.body, c.token)
		if rec.Code != c.status || h.errorCode(rec) != c.code {
			t.Errorf("%s: %d %q, want %d %q", c.name, rec.Code, h.errorCode(rec), c.status, c.code)
		}
	}
}

func TestADriverTurnsTheNudgeOnForARoute(t *testing.T) {
	h := newHarness(t)
	occ := h.occurrenceOf("+306900000504", "Οδηγός")
	driver, _ := h.signUp("+306900000504", "Οδηγός")
	other, _ := h.signUp("+306900000505", "Κάποιος")
	var route string
	h.mustQuery(`SELECT template_id::text FROM trip_occurrences WHERE id = $1`, &route, occ)

	if rec := h.do(http.MethodPut, "/v1/me/routes/"+route+"/nudge", map[string]any{"on": true}, other); rec.Code != http.StatusNotFound {
		t.Errorf("somebody else's route: %d", rec.Code)
	}
	if rec := h.do(http.MethodPut, "/v1/me/routes/"+route+"/nudge", map[string]any{"on": true}, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("nudge on: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Routes []struct {
			ID    string `json:"id"`
			Nudge bool   `json:"nudge_when_leaving"`
		} `json:"routes"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me/routes", nil, driver), &list)
	if len(list.Routes) != 1 || !list.Routes[0].Nudge {
		t.Fatalf("routes: %+v", list.Routes)
	}
}
