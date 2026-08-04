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
