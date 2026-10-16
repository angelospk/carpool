package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"carpool/internal/match"
)

// days posts the same journey on day n, as another day of `of` when set.
type days struct {
	h     *harness
	token string
}

func (d days) post(n int, of string) (status int, id, code string) {
	b := requestBody(d.h, map[string]any{
		"window_start": d.h.clock.Now().Add(time.Duration(24*n+2) * time.Hour).Format(time.RFC3339),
		"window_end":   d.h.clock.Now().Add(time.Duration(24*n+4) * time.Hour).Format(time.RFC3339),
	})
	if of != "" {
		b["alternative_of"] = of
	}
	rec := d.h.do(http.MethodPost, "/v1/requests", b, d.token)
	var out struct {
		RequestID string `json:"request_id"`
	}
	code = d.h.errorCode(rec)
	d.h.decode(rec, &out)
	return rec.Code, out.RequestID, code
}

// The same journey on other days: each is its own request, pointed at the
// first one, so accepting any of them can withdraw the rest.
func TestAnotherDayIsPostedAsAnAlternativeOfTheFirst(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000831", "Επιβάτης")
	day := func(n int) map[string]any {
		return map[string]any{
			"window_start": h.clock.Now().Add(time.Duration(24*n+2) * time.Hour).Format(time.RFC3339),
			"window_end":   h.clock.Now().Add(time.Duration(24*n+4) * time.Hour).Format(time.RFC3339),
		}
	}
	post := func(n int, of string) (int, string, string) {
		b := requestBody(h, day(n))
		if of != "" {
			b["alternative_of"] = of
		}
		rec := h.do(http.MethodPost, "/v1/requests", b, token)
		var out struct {
			RequestID string `json:"request_id"`
		}
		code := h.errorCode(rec)
		h.decode(rec, &out)
		return rec.Code, out.RequestID, code
	}

	status, first, _ := post(0, "")
	if status != http.StatusCreated {
		t.Fatalf("first day: %d", status)
	}
	status, second, _ := post(1, first)
	if status != http.StatusCreated {
		t.Fatalf("second day: %d", status)
	}
	// Pointed at an extra day, it still joins the first: one group, one root.
	status, third, _ := post(2, second)
	if status != http.StatusCreated {
		t.Fatalf("third day: %d", status)
	}
	for _, id := range []string{second, third} {
		var of string
		h.mustQuery(`SELECT alternative_of::text FROM ride_requests WHERE id = $1`, &of, id)
		if of != first {
			t.Errorf("request %s is an alternative of %s, want %s", id, of, first)
		}
	}

	// Somebody else's request, or none at all: the same 404, so ids cannot be probed.
	stranger, _ := h.signUp("+306900000832", "Άλλος")
	b := requestBody(h, day(3))
	b["alternative_of"] = first
	if rec := h.do(http.MethodPost, "/v1/requests", b, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("an alternative of somebody else's request: %d, want 404", rec.Code)
	}
	b["alternative_of"] = "not-a-uuid"
	if rec := h.do(http.MethodPost, "/v1/requests", b, token); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed alternative_of: %d, want 400", rec.Code)
	}

	// The group closes after a week of days: more is a standing request in disguise.
	for n := 3; n < 7; n++ {
		if status, _, _ := post(n, first); status != http.StatusCreated {
			t.Fatalf("day %d: %d", n, status)
		}
	}
	if status, _, code := post(7, first); status != http.StatusConflict || code != "too_many_days" {
		t.Errorf("an eighth day: %d %s, want 409 too_many_days", status, code)
	}
	// A retry of a day already in the group is that day, full or not.
	if status, id, _ := post(2, first); status/100 != 2 || id != third {
		t.Errorf("a retried day at seven: %d %s, want the day already there (%s)", status, id, third)
	}

	// Once the journey is no longer being looked for, there is nothing to add to.
	h.mustExec(`UPDATE ride_requests SET status = 'cancelled' WHERE id = $1`, first)
	if status, _, code := post(8, first); status != http.StatusConflict || code != "request_closed" {
		t.Errorf("a day added to a closed request: %d %s, want 409 request_closed", status, code)
	}
}

// The day was already asked for on its own: it joins the group rather than
// staying a separate request that accepting the group would leave live.
func TestADayAlreadyAskedForJoinsTheGroup(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000833", "Επιβάτης")
	d := days{h, token}
	_, alone, _ := d.post(1, "")
	_, first, _ := d.post(0, "")

	status, id, _ := d.post(1, first)
	if status/100 != 2 || id != alone {
		t.Fatalf("posting the same day again: %d %s, want the one already there (%s)", status, id, alone)
	}
	var of string
	h.mustQuery(`SELECT COALESCE(alternative_of::text, '') FROM ride_requests WHERE id = $1`, &of, alone)
	if of != first {
		t.Errorf("the day asked for alone is an alternative of %q, want %s", of, first)
	}

	// Already a day of another journey's group: it cannot be in two.
	_, other, _ := d.post(2, "")
	if status, _, code := d.post(1, other); status != http.StatusConflict || code != "day_in_another_group" {
		t.Errorf("a day of another group: %d %s, want 409 day_in_another_group", status, code)
	}
}

// A day added while a driver is accepting another waits for the accept, and
// then finds the journey taken. Before, it read the group before the accept
// committed, was inserted after the accept withdrew the group, and stayed
// live: a second driver could be asked to carry the same passenger.
func TestADayAddedDuringAnAcceptFindsTheJourneyTaken(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000834", "Επιβάτης")
	var passenger string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000834'`, &passenger)
	d := days{h, token}
	_, first, _ := d.post(0, "")

	ctx := context.Background()
	accept, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accept.Rollback(ctx) }()
	if _, err := accept.Exec(ctx, match.PassengerLockSQL, passenger); err != nil {
		t.Fatal(err)
	}
	if _, err := accept.Exec(ctx, `UPDATE ride_requests SET status = 'matched' WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}

	type answer struct {
		status int
		code   string
	}
	done := make(chan answer, 1)
	go func() {
		status, _, code := d.post(1, first)
		done <- answer{status, code}
	}()
	select {
	case a := <-done:
		t.Fatalf("the day was answered (%d %s) while the accept still held the passenger", a.status, a.code)
	case <-time.After(300 * time.Millisecond):
	}
	if err := accept.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if a := <-done; a.status != http.StatusConflict || a.code != "request_closed" {
		t.Errorf("a day added during the accept: %d %s, want 409 request_closed", a.status, a.code)
	}
}
