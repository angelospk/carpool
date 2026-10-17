package api_test

import (
	"net/http"
	"testing"
)

type groupBody struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Role    string  `json:"role"`
	Members int     `json:"members"`
	Code    *string `json:"code"`
}

func (h *harness) createGroup(token, name string) groupBody {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/groups", map[string]any{"name": name}, token)
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create group: status %d, body %s", rec.Code, rec.Body.String())
	}
	var g groupBody
	h.decode(rec, &g)
	return g
}

func (h *harness) joinGroup(token, code string) groupBody {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/groups/join", map[string]any{"code": code}, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("join group: status %d, body %s", rec.Code, rec.Body.String())
	}
	var g groupBody
	h.decode(rec, &g)
	return g
}

func TestAGroupIsMadeJoinedAndLeftOverTheAPI(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.signUp("+306900000701", "Διαχειρίστρια")
	member, _ := h.signUp("+306900000702", "Μέλος")

	g := h.createGroup(admin, "ΔΠΘ Ξάνθη")
	if g.Role != "admin" || g.Code == nil {
		t.Fatalf("the creator should be admin and see the code: %+v", g)
	}

	joined := h.joinGroup(member, *g.Code)
	if joined.ID != g.ID || joined.Members != 2 || joined.Role != "member" {
		t.Fatalf("joined = %+v", joined)
	}
	// Not an empty string: absent. A member may not see it at all.
	if joined.Code != nil {
		t.Fatalf("a member was shown the code")
	}

	rec := h.do(http.MethodPost, "/v1/groups/"+g.ID+"/code", nil, member)
	if rec.Code != http.StatusForbidden || h.errorCode(rec) != "not_admin" {
		t.Fatalf("a member replacing the code: %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(http.MethodDelete, "/v1/groups/"+g.ID+"/membership", nil, member)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("leave: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(http.MethodGet, "/v1/groups/mine", nil, member)
	var mine struct {
		Groups []groupBody `json:"groups"`
	}
	h.decode(rec, &mine)
	if len(mine.Groups) != 0 {
		t.Fatalf("still listed after leaving: %+v", mine.Groups)
	}
}

// Somebody outside a group gets the same answer about it whether it exists or
// not, so group ids cannot be probed.
func TestAGroupYouAreNotInLooksLikeNoGroup(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.signUp("+306900000711", "Διαχειρίστρια")
	outsider, _ := h.signUp("+306900000712", "Άσχετος")
	g := h.createGroup(admin, "Νοσοκομείο")

	for _, path := range []string{
		"/v1/groups/" + g.ID + "/code",
		"/v1/groups/00000000-0000-0000-0000-000000000000/code",
	} {
		rec := h.do(http.MethodPost, path, nil, outsider)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}

	rec := h.do(http.MethodPost, "/v1/groups/join", map[string]any{"code": "NOTACODE"}, outsider)
	if rec.Code != http.StatusBadRequest || h.errorCode(rec) != "code_not_usable" {
		t.Fatalf("a wrong code: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGroupsNeedASession(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/groups/mine"},
		{http.MethodPost, "/v1/groups"},
		{http.MethodPost, "/v1/groups/join"},
	} {
		if rec := h.do(c.method, c.path, map[string]any{}, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", c.method, c.path, rec.Code)
		}
	}
}

// The person you are about to share a car with is somebody from your school:
// that is what the trip screen says, and only about the groups both of you
// are still in.
func TestATripSaysWhichGroupsTheCallerSharesWithTheOthers(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)

	g := h.createGroup(driver, "ΔΠΘ")
	h.joinGroup(passenger, *g.Code)
	h.createGroup(driver, "Μόνο του οδηγού")

	var trip struct {
		Driver struct {
			SharedGroups []string `json:"shared_groups"`
		} `json:"driver"`
		Passengers []struct {
			SharedGroups []string `json:"shared_groups"`
		} `json:"passengers"`
	}

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("passenger GET trip: %d %s", rec.Code, rec.Body.String())
	}
	h.decode(rec, &trip)
	if len(trip.Driver.SharedGroups) != 1 || trip.Driver.SharedGroups[0] != "ΔΠΘ" {
		t.Fatalf("the passenger sees shared groups %v with the driver, want [ΔΠΘ]", trip.Driver.SharedGroups)
	}

	rec = h.do(http.MethodGet, "/v1/trips/"+occurrenceID.String(), nil, driver)
	h.decode(rec, &trip)
	if len(trip.Passengers) != 1 || len(trip.Passengers[0].SharedGroups) != 1 {
		t.Fatalf("the driver sees %+v", trip.Passengers)
	}
}

// Offering a route to a group you are not in would be a way to reach its
// members without them having let you in.
func TestARouteCanBeOfferedOnlyToAGroupTheDriverIsIn(t *testing.T) {
	// Refused before any routing happens, so the router never needs to answer.
	h := newHarnessRoutedBy(t, "http://127.0.0.1:1")
	owner, _ := h.signUp("+306900000721", "Διαχειρίστρια")
	driver, _ := h.signUp("+306900000722", "Οδηγός")
	g := h.createGroup(owner, "Νοσοκομείο")

	for _, groupID := range []string{g.ID, "not-a-uuid"} {
		rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
			"origin_lon": 25.87, "origin_lat": 40.85, "destination_lon": 26.1, "destination_lat": 41.0,
			"origin_label": "Αλεξανδρούπολη", "destination_label": "Φέρες",
			"kind": "oneoff", "oneoff_date": "2026-07-30", "depart_earliest": "07:00", "depart_latest": "07:30",
			"group_id": groupID,
		}, driver)
		if rec.Code != http.StatusForbidden || h.errorCode(rec) != "not_in_group" {
			t.Fatalf("group %s: %d %s", groupID, rec.Code, rec.Body.String())
		}
	}
}

// Deleting an account takes the person out of every group, and a group whose
// admin that was is handed on rather than left with nobody to run it.
func TestDeletingAnAccountLeavesItsGroupsInOtherHands(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.signUp("+306900000731", "Διαχειρίστρια")
	member, _ := h.signUp("+306900000732", "Μέλος")
	g := h.createGroup(admin, "ΔΠΘ")
	h.joinGroup(member, *g.Code)

	h.do(http.MethodPost, "/v1/me/delete-code", nil, admin)
	if rec := h.do(http.MethodDelete, "/v1/me", map[string]any{"code": h.sms.LastCode()}, admin); rec.Code/100 != 2 {
		t.Fatalf("delete account: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(http.MethodGet, "/v1/groups/mine", nil, member)
	var mine struct {
		Groups []groupBody `json:"groups"`
	}
	h.decode(rec, &mine)
	if len(mine.Groups) != 1 || mine.Groups[0].Members != 1 || mine.Groups[0].Role != "admin" {
		t.Fatalf("after the admin's account went: %+v", mine.Groups)
	}
}
