package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Αλεξανδρούπολη, and somewhere well inside it.
const (
	alexandroupoli = "node/139260064"
	spotLon        = 25.8600
	spotLat        = 40.8600
)

func propose(h *harness, token string, body map[string]any) *httptest.ResponseRecorder {
	if body["place_id"] == nil {
		body["place_id"] = alexandroupoli
	}
	return h.do(http.MethodPost, "/v1/places/points", body, token)
}

// The queue is not something an ordinary account discovers by trying.
func TestModerationIsRefusedToOrdinaryAccounts(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000901", "Νίκος")

	for _, path := range []string{
		"/v1/moderation/meeting-points",
	} {
		rec := h.do(http.MethodGet, path, nil, token)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as an ordinary user: %d, want 403", path, rec.Code)
		}
	}
}

func TestModerationIsRefusedWithoutAToken(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/v1/moderation/meeting-points", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", rec.Code)
	}
}

// Revoking has to take effect at once. The flag is read on every request rather
// than carried in the token, so the same token stops working immediately.
func TestARevokedModeratorIsRefusedOnTheirNextRequest(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000902", "Μαρία")
	makeModerator(t, h, "+306900000902", true)
	if rec := h.do(http.MethodGet, "/v1/moderation/meeting-points", nil, token); rec.Code != http.StatusOK {
		t.Fatalf("a moderator was refused: %d %s", rec.Code, rec.Body.String())
	}

	makeModerator(t, h, "+306900000902", false)

	// The same token, which has not expired and never will within this test.
	if rec := h.do(http.MethodGet, "/v1/moderation/meeting-points", nil, token); rec.Code != http.StatusForbidden {
		t.Errorf("status %d after revocation, want 403 on the very next request", rec.Code)
	}
}

// The whole flow: somebody suggests a spot, nobody else sees it, a moderator
// looks, and then everybody sees it.
func TestASuggestionIsInvisibleUntilSomebodyHasLookedAtIt(t *testing.T) {
	h := newHarness(t)
	author, _ := h.signUp("+306900000903", "Γιώργος")
	mod, _ := h.signUp("+306900000904", "Ελένη")
	makeModerator(t, h, "+306900000904", true)

	before := meetingPoints(t, h, author)

	rec := propose(h, author, map[string]any{
		"name": "Πλατεία Κύπρου", "kind": "other", "lon": spotLon, "lat": spotLat,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("proposing: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	h.decode(rec, &created)
	if created.Status != "pending" {
		t.Errorf("status = %q, want pending", created.Status)
	}

	if got := meetingPoints(t, h, author); len(got) != len(before) {
		t.Fatalf("a pending suggestion is already being offered")
	}

	queue := h.do(http.MethodGet, "/v1/moderation/meeting-points", nil, mod)
	if queue.Code != http.StatusOK {
		t.Fatalf("reading the queue: %d", queue.Code)
	}
	var pending struct {
		Requests []struct {
			ID        string `json:"id"`
			PlaceName string `json:"place_name"`
		} `json:"requests"`
	}
	h.decode(queue, &pending)
	if len(pending.Requests) != 1 || pending.Requests[0].ID != created.ID {
		t.Fatalf("queue = %+v, want the one suggestion", pending.Requests)
	}
	if pending.Requests[0].PlaceName == "" {
		t.Error("the queue shows OSM ids rather than place names")
	}

	if rec := h.do(http.MethodPost, "/v1/moderation/meeting-points/"+created.ID+"/approve",
		nil, mod); rec.Code != http.StatusNoContent {
		t.Fatalf("approving: %d %s", rec.Code, rec.Body.String())
	}

	after := meetingPoints(t, h, author)
	var found bool
	for _, name := range after {
		if name == "Πλατεία Κύπρου" {
			found = true
		}
	}
	if !found {
		t.Errorf("after approval the point is still not offered: %v", after)
	}
}

// Deciding twice is a conflict rather than a second decision.
func TestASuggestionCanOnlyBeDecidedOnce(t *testing.T) {
	h := newHarness(t)
	author, _ := h.signUp("+306900000905", "Κώστας")
	mod, _ := h.signUp("+306900000906", "Άννα")
	makeModerator(t, h, "+306900000906", true)

	rec := propose(h, author, map[string]any{
		"name": "Πλατεία Κύπρου", "kind": "other", "lon": spotLon, "lat": spotLat,
	})
	var created struct {
		ID string `json:"id"`
	}
	h.decode(rec, &created)

	first := h.do(http.MethodPost, "/v1/moderation/meeting-points/"+created.ID+"/approve", nil, mod)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first decision: %d", first.Code)
	}
	second := h.do(http.MethodPost, "/v1/moderation/meeting-points/"+created.ID+"/reject",
		map[string]any{"reason": "on second thoughts"}, mod)
	if second.Code != http.StatusConflict {
		t.Errorf("second decision: %d, want 409", second.Code)
	}
}

// Somebody's own suggestions are theirs; the queue is nobody's to browse.
func TestAnAuthorSeesTheirOwnSuggestionAndNobodyElsesThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	mine, _ := h.signUp("+306900000907", "Δημήτρης")
	theirs, _ := h.signUp("+306900000908", "Σοφία")

	propose(h, mine, map[string]any{"name": "Δικό μου", "kind": "other", "lon": spotLon, "lat": spotLat})
	propose(h, theirs, map[string]any{"name": "Δικό τους", "kind": "other", "lon": spotLon + 0.01, "lat": spotLat})

	rec := h.do(http.MethodGet, "/v1/me/meeting-points", nil, mine)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Proposals []struct {
			Name string `json:"name"`
		} `json:"proposals"`
	}
	h.decode(rec, &out)
	if len(out.Proposals) != 1 || out.Proposals[0].Name != "Δικό μου" {
		t.Errorf("got %+v, want only my own", out.Proposals)
	}
}

func TestABadSuggestionIsRefusedWithACodeTheAppCanActOn(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000909", "Παύλος")

	for _, tc := range []struct {
		name string
		body map[string]any
		want int
		code string
	}{
		{"a point in Paris",
			map[string]any{"name": "Κάπου", "kind": "other", "lon": 2.35, "lat": 48.85},
			http.StatusBadRequest, "too_far"},
		{"a kind nobody offers",
			map[string]any{"name": "Κάπου", "kind": "helipad", "lon": spotLon, "lat": spotLat},
			http.StatusBadRequest, "invalid_kind"},
		{"a settlement that does not exist",
			map[string]any{"place_id": "node/999999999999", "name": "Κάπου", "kind": "other", "lon": spotLon, "lat": spotLat},
			http.StatusBadRequest, "unknown_place"},
		{"a name of only spaces",
			map[string]any{"name": "   ", "kind": "other", "lon": spotLon, "lat": spotLat},
			http.StatusBadRequest, "invalid_name"},
	} {
		rec := propose(h, token, tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s: body %s, want the code %q", tc.name, rec.Body.String(), tc.code)
		}
	}
}

// makeModerator sets the flag the way the operator command does, by the person
// rather than by whatever the sign-up handed back.
func makeModerator(t *testing.T, h *harness, phone string, granted bool) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(),
		`UPDATE users SET is_moderator = $2 WHERE phone = $1`, phone, granted); err != nil {
		t.Fatalf("setting the moderator flag: %v", err)
	}
}

// meetingPoints returns the names a settlement currently offers.
func meetingPoints(t *testing.T, h *harness, token string) []string {
	t.Helper()
	rec := h.do(http.MethodGet, "/v1/places/points?place=node%2F139260064", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("reading points: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Points []struct {
			Name string `json:"name"`
		} `json:"points"`
	}
	h.decode(rec, &out)
	names := make([]string, 0, len(out.Points))
	for _, p := range out.Points {
		names = append(names, p.Name)
	}
	return names
}

// What the automatic list actually needs: taking away a ticket agency tagged as
// a bus station, and putting the real one above what is left.
func TestAModeratorCanCorrectTheAutomaticList(t *testing.T) {
	h := newHarness(t)
	reader, _ := h.signUp("+306900000910", "Αναγνώστης")
	mod, _ := h.signUp("+306900000911", "Ελένη")
	makeModerator(t, h, "+306900000911", true)

	before := meetingPoints(t, h, reader)
	if len(before) == 0 {
		t.Fatal("nothing harvested to correct")
	}

	// The id of the first harvested point, which is the one to take away.
	rec := h.do(http.MethodGet, "/v1/places/points?place=node%2F139260064", nil, reader)
	var listed struct {
		Points []struct {
			ID string `json:"id"`
		} `json:"points"`
	}
	h.decode(rec, &listed)

	hide := h.do(http.MethodPost, "/v1/moderation/meeting-points/edit", map[string]any{
		"place_id": alexandroupoli, "action": "hide", "target_id": listed.Points[0].ID,
	}, mod)
	if hide.Code != http.StatusNoContent {
		t.Fatalf("hiding: %d %s", hide.Code, hide.Body.String())
	}

	add := h.do(http.MethodPost, "/v1/moderation/meeting-points/edit", map[string]any{
		"place_id": alexandroupoli, "action": "add", "name": "ΚΤΕΛ Έβρου",
		"kind": "bus_station", "lon": spotLon, "lat": spotLat, "priority": -1,
	}, mod)
	if add.Code != http.StatusNoContent {
		t.Fatalf("adding: %d %s", add.Code, add.Body.String())
	}

	after := meetingPoints(t, h, reader)
	if len(after) == 0 || after[0] != "ΚΤΕΛ Έβρου" {
		t.Errorf("list = %v, want the curated point first", after)
	}
	for _, name := range after {
		if name == before[0] {
			t.Errorf("%q is still listed after being hidden", name)
		}
	}
}

// A moderator's own corrections leave a trace, or the question "who removed
// this, and when" has no answer.
func TestAModeratorsOwnCorrectionIsRecorded(t *testing.T) {
	h := newHarness(t)
	mod, _ := h.signUp("+306900000912", "Ελένη")
	makeModerator(t, h, "+306900000912", true)

	if rec := h.do(http.MethodPost, "/v1/moderation/meeting-points/edit", map[string]any{
		"place_id": alexandroupoli, "action": "add", "name": "ΚΤΕΛ Έβρου",
		"kind": "bus_station", "lon": spotLon, "lat": spotLat,
	}, mod); rec.Code != http.StatusNoContent {
		t.Fatalf("adding: %d %s", rec.Code, rec.Body.String())
	}

	var n int
	if err := h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM meeting_point_requests r
		  JOIN users u ON u.id = r.reviewed_by
		 WHERE u.phone = '+306900000912' AND r.status = 'approved'`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("%d recorded corrections, want the one that was made", n)
	}
}

// The correction endpoint is moderation, so it is refused like the rest of it.
func TestCorrectingIsRefusedToOrdinaryAccounts(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000913", "Νίκος")

	rec := h.do(http.MethodPost, "/v1/moderation/meeting-points/edit", map[string]any{
		"place_id": alexandroupoli, "action": "hide", "target_id": "node/1",
	}, token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", rec.Code)
	}
}

// A moderator is trusted to judge a place, not to invent a taxonomy or put a
// point in another country. The checks are the same ones an ordinary
// suggestion gets.
func TestAModeratorsCorrectionIsCheckedToo(t *testing.T) {
	h := newHarness(t)
	mod, _ := h.signUp("+306900000914", "Ελένη")
	makeModerator(t, h, "+306900000914", true)

	for _, tc := range []struct {
		name string
		body map[string]any
		code string
	}{
		{"a kind nobody offers", map[string]any{
			"place_id": alexandroupoli, "action": "add", "name": "Κάπου",
			"kind": "helipad", "lon": spotLon, "lat": spotLat}, "invalid_kind"},
		{"a point in Paris", map[string]any{
			"place_id": alexandroupoli, "action": "add", "name": "Κάπου",
			"kind": "other", "lon": 2.35, "lat": 48.85}, "too_far"},
		{"a name of only spaces", map[string]any{
			"place_id": alexandroupoli, "action": "add", "name": "   ",
			"kind": "other", "lon": spotLon, "lat": spotLat}, "invalid_name"},
		{"hiding without saying which", map[string]any{
			"place_id": alexandroupoli, "action": "hide"}, "invalid_request"},
	} {
		rec := h.do(http.MethodPost, "/v1/moderation/meeting-points/edit", tc.body, mod)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s: %d %s, want 400 and %q", tc.name, rec.Code, rec.Body.String(), tc.code)
		}
	}
}

// A database that cannot be read is not an answer about who somebody is.
// Reporting 403 during an outage tells a moderator they have been demoted, and
// sends them looking for the wrong problem.
func TestADatabaseFailureIsNotReportedAsNotBeingAModerator(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000915", "Ελένη")
	makeModerator(t, h, "+306900000915", true)

	ctx := context.Background()
	// The bluntest possible outage, and a real one: the query the check makes
	// stops working.
	if _, err := h.pool.Exec(ctx, `ALTER TABLE users RENAME TO users_away`); err != nil {
		t.Fatalf("breaking the read: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `ALTER TABLE users_away RENAME TO users`)
	})

	rec := h.do(http.MethodGet, "/v1/moderation/meeting-points", nil, token)
	if rec.Code == http.StatusForbidden {
		t.Fatal("an outage was reported as the caller not being a moderator")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", rec.Code)
	}
}
