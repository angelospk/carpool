package api_test

import (
	"net/http"
	"testing"
)

type blockedPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *harness) myBlocks(token string) []blockedPerson {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/me/blocks", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("GET /me/blocks: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Blocked []blockedPerson `json:"blocked"`
	}
	h.decode(rec, &body)
	return body.Blocked
}

// Unblocking was an endpoint with no way to reach it: a block could only be
// undone by somebody who still had the other person's profile open. The list
// is who you blocked, and only that — never who blocked you.
func TestBlockedListShowsOnlyWhoYouBlocked(t *testing.T) {
	h := newHarness(t)
	me, _ := h.signUp("+306900000501", "Εγώ")
	other, _ := h.signUp("+306900000502", "Άλλος")
	h.signUp("+306900000503", "Τρίτος")
	var otherID, thirdID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000502'`, &otherID)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000503'`, &thirdID)

	h.do(http.MethodPost, "/v1/users/"+otherID+"/block", nil, me)
	// A block the other way round is theirs, not mine to see or undo.
	var myID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000501'`, &myID)
	h.do(http.MethodPost, "/v1/users/"+myID+"/block", nil, other)

	got := h.myBlocks(me)
	if len(got) != 1 || got[0].ID != otherID || got[0].Name != "Άλλος" {
		t.Fatalf("blocked = %+v, want only Άλλος", got)
	}

	h.do(http.MethodDelete, "/v1/users/"+otherID+"/block", nil, me)
	if got := h.myBlocks(me); len(got) != 0 {
		t.Errorf("after unblocking: %+v", got)
	}
	// Unblocking mine did not lift theirs.
	if got := h.myBlocks(other); len(got) != 1 {
		t.Errorf("the other person's block was lifted too: %+v", got)
	}
}
