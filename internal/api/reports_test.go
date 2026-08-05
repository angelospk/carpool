package api_test

import (
	"net/http"
	"testing"
)

// makeModerator grants the flag directly, the way cmd/moderator does. There is
// deliberately no way to do it through the API.
func (h *harness) makeModerator(phone string) {
	h.t.Helper()
	h.mustExec(`UPDATE users SET is_moderator = true WHERE phone = $1`, phone)
}

func TestAReportReachesTheQueueAndCanBeDecided(t *testing.T) {
	h := newHarness(t)
	reporter, _ := h.signUp("+306900000901", "Καταγγέλλων")
	h.signUp("+306900000902", "Καταγγελλόμενος")

	var badID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &badID, "+306900000902")

	if rec := h.do(http.MethodPost, "/v1/users/"+badID+"/report",
		map[string]any{"reason": "άφησε τον κόσμο στον δρόμο"}, reporter); rec.Code != http.StatusNoContent {
		t.Fatalf("filing: %d %s", rec.Code, rec.Body.String())
	}

	// A moderator sees it; an ordinary member does not.
	if rec := h.do(http.MethodGet, "/v1/moderation/reports", nil, reporter); rec.Code != http.StatusForbidden {
		t.Fatalf("an ordinary member read the queue: %d", rec.Code)
	}

	h.makeModerator("+306900000901")
	rec := h.do(http.MethodGet, "/v1/moderation/reports", nil, reporter)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue: %d %s", rec.Code, rec.Body.String())
	}
	var queue struct {
		Reports []struct {
			ID           string `json:"id"`
			ReportedName string `json:"reported_name"`
			PriorReports int    `json:"prior_reports"`
		} `json:"reports"`
	}
	h.decode(rec, &queue)
	if len(queue.Reports) != 1 {
		t.Fatalf("%d reports in the queue, want 1", len(queue.Reports))
	}
	if queue.Reports[0].ReportedName != "Καταγγελλόμενος" {
		t.Errorf("reported name = %q", queue.Reports[0].ReportedName)
	}

	// Deciding to act suspends, and the suspension is enforced immediately.
	if rec := h.do(http.MethodPost, "/v1/moderation/reports/"+queue.Reports[0].ID+"/suspend",
		map[string]any{"note": "confirmed by two other passengers"}, reporter); rec.Code != http.StatusNoContent {
		t.Fatalf("deciding: %d %s", rec.Code, rec.Body.String())
	}

	var status string
	h.mustQuery(`SELECT status FROM users WHERE phone = $1`, &status, "+306900000902")
	if status != "suspended" {
		t.Errorf("account status = %q, want suspended", status)
	}

	// And the queue is empty again.
	rec = h.do(http.MethodGet, "/v1/moderation/reports", nil, reporter)
	h.decode(rec, &queue)
	if len(queue.Reports) != 0 {
		t.Errorf("%d reports still open after deciding the only one", len(queue.Reports))
	}
}

// The second moderator to press must not silently overwrite the first one's
// judgement, and must be told which of the two things happened.
func TestDecidingAnAlreadyDecidedReportIsAConflict(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000903", "Επόπτης")
	h.signUp("+306900000904", "Άλλος")
	h.makeModerator("+306900000903")

	var otherID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &otherID, "+306900000904")
	h.do(http.MethodPost, "/v1/users/"+otherID+"/report", map[string]any{"reason": "κάτι"}, token)

	rec := h.do(http.MethodGet, "/v1/moderation/reports", nil, token)
	var queue struct {
		Reports []struct {
			ID string `json:"id"`
		} `json:"reports"`
	}
	h.decode(rec, &queue)
	id := queue.Reports[0].ID

	if rec := h.do(http.MethodPost, "/v1/moderation/reports/"+id+"/dismiss",
		map[string]any{"note": "μίλησα και με τους δύο"}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("first decision: %d", rec.Code)
	}

	rec = h.do(http.MethodPost, "/v1/moderation/reports/"+id+"/suspend",
		map[string]any{"note": "άλλαξα γνώμη"}, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second decision: status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if code := h.errorCode(rec); code != "already_decided" {
		t.Errorf("error code = %q, want already_decided", code)
	}
}

// Somebody who filed a complaint is shown that it went somewhere. What they are
// not shown is what happened to the other account.
func TestAMemberSeesTheStateOfTheirOwnReports(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000905", "Καταγγέλλων")
	h.signUp("+306900000906", "Άλλος")

	var otherID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = $1`, &otherID, "+306900000906")
	h.do(http.MethodPost, "/v1/users/"+otherID+"/report", map[string]any{"reason": "κάτι"}, token)

	rec := h.do(http.MethodGet, "/v1/me/reports", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body.String())
	}
	var mine struct {
		Reports []map[string]any `json:"reports"`
	}
	h.decode(rec, &mine)
	if len(mine.Reports) != 1 {
		t.Fatalf("%d reports, want 1", len(mine.Reports))
	}
	if mine.Reports[0]["status"] != "open" {
		t.Errorf("status = %v, want open", mine.Reports[0]["status"])
	}
	if _, leaked := mine.Reports[0]["reported_status"]; leaked {
		t.Error("the reporter was told the state of somebody else's account")
	}
}
