package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func weekendBody() map[string]any {
	return map[string]any{
		"origin_lon": 25.82, "origin_lat": 40.85,
		"destination_lon": 25.98, "destination_lat": 40.95,
		"origin_label": "Αλεξανδρούπολη", "destination_label": "Θεσσαλονίκη",
		"depart_dow": 5, "depart_time": "15:00",
		"return_dow": 0, "return_time": "18:00",
		"seats": 2, "luggage": "suitcase", "luggage_count": 1,
		"every_week": true,
	}
}

type planJSON struct {
	ID            uuid.UUID `json:"id"`
	OriginLabel   string    `json:"origin_label"`
	DepartDow     int       `json:"depart_dow"`
	DepartTime    string    `json:"depart_time"`
	ReturnDow     *int      `json:"return_dow"`
	ReturnTime    *string   `json:"return_time"`
	Seats         int       `json:"seats"`
	Luggage       string    `json:"luggage"`
	LuggageCount  int       `json:"luggage_count"`
	EveryWeek     bool      `json:"every_week"`
	ThisWeek      string    `json:"this_week"`
	AskToStop     bool      `json:"ask_to_stop"`
	DeclinesInRow int       `json:"declines_in_row"`
}

func TestCreatingAndListingAWeekendPlan(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000051", "Επιβάτης")

	rec := h.do(http.MethodPost, "/v1/weekend-plans", weekendBody(), token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body.String())
	}
	var created planJSON
	h.decode(rec, &created)
	if created.ID == uuid.Nil || created.DepartDow != 5 || created.DepartTime != "15:00" ||
		created.ReturnDow == nil || *created.ReturnDow != 0 || created.ReturnTime == nil || *created.ReturnTime != "18:00" ||
		created.Seats != 2 || created.Luggage != "suitcase" || created.LuggageCount != 1 || !created.EveryWeek {
		t.Fatalf("created = %+v", created)
	}

	rec = h.do(http.MethodGet, "/v1/weekend-plans", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Plans []planJSON `json:"plans"`
	}
	h.decode(rec, &list)
	if len(list.Plans) != 1 || list.Plans[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}
	if list.Plans[0].ThisWeek != "open" || list.Plans[0].AskToStop || list.Plans[0].OriginLabel != "Αλεξανδρούπολη" {
		t.Errorf("plan = %+v", list.Plans[0])
	}

	// Four requests for the coming weekend: three outbound days and a return.
	var n int
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE weekend_plan_id = $1`, &n, created.ID)
	if n != 4 {
		t.Errorf("%d requests made for the plan, want 4", n)
	}
}

func TestAWeekendPlanWithoutAReturnOrLuggageIsFine(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000052", "Επιβάτης")
	body := weekendBody()
	for _, k := range []string{"return_dow", "return_time", "luggage", "luggage_count", "seats", "every_week"} {
		delete(body, k)
	}
	rec := h.do(http.MethodPost, "/v1/weekend-plans", body, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got planJSON
	h.decode(rec, &got)
	if got.ReturnDow != nil || got.ReturnTime != nil || got.Seats != 1 || got.Luggage != "none" ||
		got.LuggageCount != 0 || !got.EveryWeek {
		t.Errorf("defaults = %+v", got)
	}
}

func TestAWeekendPlanThatCannotBeIsRefused(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000053", "Επιβάτης")
	cases := map[string]struct {
		mutate func(map[string]any)
		code   string
	}{
		"a Wednesday":         {func(b map[string]any) { b["depart_dow"] = 3 }, "invalid_day"},
		"no day at all":       {func(b map[string]any) { delete(b, "depart_dow") }, "invalid_day"},
		"no time at all":      {func(b map[string]any) { delete(b, "depart_time") }, "invalid_time"},
		"a return before":     {func(b map[string]any) { b["depart_dow"], b["return_dow"] = 6, 5 }, "invalid_return"},
		"a return half given": {func(b map[string]any) { delete(b, "return_time") }, "invalid_return"},
		"bad luggage":         {func(b map[string]any) { b["luggage"] = "piano" }, "invalid_luggage"},
		"a suitcase of none":  {func(b map[string]any) { b["luggage_count"] = 0 }, "invalid_luggage"},
		"nine seats":          {func(b map[string]any) { b["seats"] = 9 }, "invalid_seats"},
		"both ends the same": {func(b map[string]any) {
			b["destination_lon"], b["destination_lat"] = b["origin_lon"], b["origin_lat"]
		}, "too_short"},
		"outside Greece":   {func(b map[string]any) { b["destination_lon"], b["destination_lat"] = 10.0, 50.0 }, "outside_country"},
		"an unknown field": {func(b map[string]any) { b["window_start"] = "now" }, "invalid_request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body := weekendBody()
			tc.mutate(body)
			rec := h.do(http.MethodPost, "/v1/weekend-plans", body, token)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if got := h.errorCode(rec); got != tc.code {
				t.Errorf("error code %q, want %q", got, tc.code)
			}
		})
	}
	var n int
	h.mustQuery(`SELECT count(*) FROM weekend_plans`, &n)
	if n != 0 {
		t.Errorf("%d plans stored after refusals", n)
	}
}

func TestWeekendPlansNeedASignedInCaller(t *testing.T) {
	h := newHarness(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/v1/weekend-plans"},
		{http.MethodGet, "/v1/weekend-plans"},
		{http.MethodDelete, "/v1/weekend-plans/" + uuid.NewString()},
		{http.MethodPost, "/v1/weekend-plans/" + uuid.NewString() + "/this-week"},
	} {
		if rec := h.do(call.method, call.path, map[string]any{}, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", call.method, call.path, rec.Code)
		}
	}
}

func TestStoppingAWeekendPlanCancelsItsRequests(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000054", "Επιβάτης")
	var plan planJSON
	h.decode(h.do(http.MethodPost, "/v1/weekend-plans", weekendBody(), token), &plan)

	rec := h.do(http.MethodDelete, "/v1/weekend-plans/"+plan.ID.String(), nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var live int
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE weekend_plan_id = $1 AND status <> 'cancelled'`, &live, plan.ID)
	if live != 0 {
		t.Errorf("%d requests still looking after the plan stopped", live)
	}
	var list struct {
		Plans []planJSON `json:"plans"`
	}
	h.decode(h.do(http.MethodGet, "/v1/weekend-plans", nil, token), &list)
	if len(list.Plans) != 0 {
		t.Errorf("a stopped plan is still listed")
	}
}

func TestSomebodyElsesWeekendPlanIsNotFound(t *testing.T) {
	h := newHarness(t)
	owner, _ := h.signUp("+306900000055", "Ιδιοκτήτης")
	stranger, _ := h.signUp("+306900000056", "Άγνωστος")
	var plan planJSON
	h.decode(h.do(http.MethodPost, "/v1/weekend-plans", weekendBody(), owner), &plan)

	paths := []struct{ method, path string }{
		{http.MethodDelete, "/v1/weekend-plans/" + plan.ID.String()},
		{http.MethodPost, "/v1/weekend-plans/" + plan.ID.String() + "/this-week"},
	}
	missing := []struct{ method, path string }{
		{http.MethodDelete, "/v1/weekend-plans/" + uuid.NewString()},
		{http.MethodPost, "/v1/weekend-plans/" + uuid.NewString() + "/this-week"},
	}
	for i, call := range paths {
		theirs := h.do(call.method, call.path, map[string]any{"going": false}, stranger)
		nothing := h.do(missing[i].method, missing[i].path, map[string]any{"going": false}, stranger)
		if theirs.Code != http.StatusNotFound || nothing.Code != http.StatusNotFound {
			t.Fatalf("%s: somebody else's plan %d, a missing one %d; both want 404", call.method, theirs.Code, nothing.Code)
		}
		if theirs.Body.String() != nothing.Body.String() {
			t.Errorf("%s: the two answers differ:\n%s\n%s", call.method, theirs.Body.String(), nothing.Body.String())
		}
	}
	var active bool
	var live int
	h.mustQuery(`SELECT active FROM weekend_plans WHERE id = $1`, &active, plan.ID)
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE weekend_plan_id = $1 AND status = 'open'`, &live, plan.ID)
	if !active || live != 4 {
		t.Errorf("a stranger changed the plan: active %v, %d open requests", active, live)
	}
}

func TestThisWeekAnswers(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000057", "Επιβάτης")
	var plan planJSON
	h.decode(h.do(http.MethodPost, "/v1/weekend-plans", weekendBody(), token), &plan)
	path := "/v1/weekend-plans/" + plan.ID.String() + "/this-week"

	rec := h.do(http.MethodPost, path, map[string]any{"going": false}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("no: status %d: %s", rec.Code, rec.Body.String())
	}
	var got planJSON
	h.decode(rec, &got)
	if got.ThisWeek != "cancelled" || got.DeclinesInRow != 1 || got.AskToStop {
		t.Errorf("after no: %+v", got)
	}
	var live int
	h.mustQuery(`SELECT count(*) FROM ride_requests WHERE weekend_plan_id = $1 AND status = 'open'`, &live, plan.ID)
	if live != 0 {
		t.Errorf("%d requests still open after no", live)
	}

	// The third no in a row. The two before it are set directly: the token
	// would not outlive the weeks it takes to give them.
	if _, err := h.pool.Exec(context.Background(), `UPDATE weekend_plans SET declines_in_row = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE ride_requests SET status = 'open'`); err != nil {
		t.Fatal(err)
	}
	rec = h.do(http.MethodPost, path, map[string]any{"going": false}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("third no: %d: %s", rec.Code, rec.Body.String())
	}
	h.decode(rec, &got)
	if got.DeclinesInRow != 3 || !got.AskToStop {
		t.Errorf("after three: %+v", got)
	}

	rec = h.do(http.MethodPost, path, map[string]any{"going": true}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("yes: status %d: %s", rec.Code, rec.Body.String())
	}
	h.decode(rec, &got)
	if got.DeclinesInRow != 0 || got.AskToStop {
		t.Errorf("after yes: %+v", got)
	}
}

func TestThisWeekNeedsAnAnswer(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000058", "Επιβάτης")
	var plan planJSON
	h.decode(h.do(http.MethodPost, "/v1/weekend-plans", weekendBody(), token), &plan)
	path := "/v1/weekend-plans/" + plan.ID.String() + "/this-week"

	for name, body := range map[string]map[string]any{
		"nothing": {}, "a string": {"going": "yes"}, "an unknown field": {"going": true, "maybe": 1},
	} {
		if rec := h.do(http.MethodPost, path, body, token); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if rec := h.do(http.MethodPost, "/v1/weekend-plans/not-a-uuid/this-week", map[string]any{"going": true}, token); rec.Code != http.StatusNotFound {
		t.Errorf("a malformed id: status %d, want 404", rec.Code)
	}
}
