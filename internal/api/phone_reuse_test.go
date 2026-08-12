package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/account"
)

// A recycled phone number is a new person.
//
// Greek numbers are reassigned, so the number that reached somebody last year
// can reach somebody else this year, and an account that has been closed leaves
// its number free. Both directions of getting this wrong are bad: inheriting
// the previous holder's ratings and trips hands a stranger somebody's history,
// and inheriting their blocks or reports punishes a stranger for it.
//
// Erasure rewrites the phone to an unusable form, so the new signup finds
// nothing to join to. This is that claim tested rather than assumed, because it
// is the sort of thing a later "let people recover a deleted account" feature
// would quietly reverse.

func TestANumberThatComesBackBelongsToNobodyWhoUsedItBefore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	const number = "+306900000077"
	h.signUp(number, "Πρώτος")

	var firstID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = $1`, &firstID, number)

	// A history worth inheriting, on both sides: what they did, and what was
	// decided about them.
	var otherID uuid.UUID
	h.signUp("+306900000078", "Άλλος")
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000078'`, &otherID)
	h.mustExec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, otherID, firstID)
	h.mustExec(`INSERT INTO reports (reporter_id, reported_id, reason) VALUES ($1, $2, 'κάτι')`,
		otherID, firstID)

	if err := account.NewService(h.pool, h.clock).Delete(ctx, firstID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The number is free again, and signing up with it works.
	h.signUp(number, "Δεύτερος")
	var secondID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = $1`, &secondID, number)

	if secondID == firstID {
		t.Fatal("the number came back to the same account rather than a new one")
	}
	for what, sql := range map[string]string{
		"a block against them": `SELECT count(*) FROM blocks WHERE blocked_id = $1 OR blocker_id = $1`,
		"a complaint about them": `SELECT count(*) FROM reports
		                            WHERE reported_id = $1 OR reporter_id = $1`,
		"somebody else's rides": `SELECT count(*) FROM bookings WHERE passenger_id = $1`,
		"somebody else's rating": `SELECT count(*) FROM ratings
		                            WHERE ratee_id = $1 OR rater_id = $1`,
	} {
		var n int
		h.mustQuery(sql, &n, secondID)
		if n != 0 {
			t.Errorf("the new holder of the number inherited %s (%d rows)", what, n)
		}
	}
}

// And the person who left cannot get back in with it. Signing up again is a new
// account, not the old one returning: their trips, their conversations and
// their ratings stay unreachable.
func TestSigningUpAgainDoesNotReopenAClosedAccount(t *testing.T) {
	h := newHarness(t)
	const number = "+306900000079"
	token, _ := h.signUp(number, "Κάποιος")

	var firstID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = $1`, &firstID, number)
	if err := account.NewService(h.pool, h.clock).Delete(context.Background(), firstID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The old session is dead the moment the account is, and it is told the
	// truth about why. An access token outlives a closure by up to its
	// lifetime, so somebody's own last tap lands here — and being told they
	// were suspended, when what happened is that they asked to be erased, is a
	// different and worse thing to say.
	rec := h.do(http.MethodGet, "/v1/me", nil, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a token from before the closure gets %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if got := h.errorCode(rec); got != "account_closed" {
		t.Fatalf("somebody who closed their account is told %q", got)
	}

	h.signUp(number, "Κάποιος")
	var secondID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = $1`, &secondID, number)
	if secondID == firstID {
		t.Fatal("signing up with the old number reopened the closed account")
	}

	var status string
	h.mustQuery(`SELECT status FROM users WHERE id = $1`, &status, firstID)
	if status != "deleted" {
		t.Fatalf("the closed account is %q again", status)
	}
}
