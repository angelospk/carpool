package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// noDatabaseText fails when an error message leaks driver or SQL detail.
func (h *harness) noDatabaseText(t *testing.T, body string) {
	t.Helper()
	for _, leak := range []string{"ERROR:", "SQLSTATE", "pgx", "violates", "store:"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks %q: %s", leak, body)
		}
	}
}

func TestChatNulByteIsRejectedWithoutDatabaseText(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)

	rec := h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/messages",
		map[string]any{"body": "nul:\u0000:end"}, passenger)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	h.noDatabaseText(t, rec.Body.String())
}

func TestBlockingAMissingUserIs404WithoutDatabaseText(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)

	rec := h.do(http.MethodPost, "/v1/users/00000000-0000-4000-8000-000000000000/block", nil, passenger)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
	h.noDatabaseText(t, rec.Body.String())
}

func TestBlockingYourselfIs400WithoutDatabaseText(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)
	var id string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000032'`, &id)

	rec := h.do(http.MethodPost, "/v1/users/"+id+"/block", nil, passenger)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	h.noDatabaseText(t, rec.Body.String())
}

func TestRatingTagIsBounded(t *testing.T) {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	h.departed()
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, driver)
	h.do(http.MethodPost, "/v1/trips/"+occurrenceID.String()+"/confirm", nil, passenger)
	path := "/v1/trips/" + occurrenceID.String() + "/rate"

	for name, tag := range map[string]string{
		"too long":        strings.Repeat("x", 41),
		"too long, greek": strings.Repeat("ω", 41),
		"control char":    "bad\x00tag",
		"newline":         "a\nb",
		"bidi override":   "a\u202eb",
	} {
		rec := h.do(http.MethodPost, path, map[string]any{"stars": 4, "tag": tag}, passenger)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
		if c := h.errorCode(rec); c != "invalid_request" {
			t.Errorf("%s: code = %q, want invalid_request", name, c)
		}
	}

	// A tag at the limit, in runes, still goes through.
	rec := h.do(http.MethodPost, path, map[string]any{"stars": 4, "tag": strings.Repeat("ω", 40)}, passenger)
	if rec.Code != http.StatusCreated {
		t.Fatalf("40-rune tag: status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
}

func TestChatBodyIsNormalisedAndCountedInRunes(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)
	path := "/v1/trips/" + occurrenceID.String() + "/messages"

	// 1500 Greek letters are 3000 bytes: fine by the rune limit.
	long := strings.Repeat("ω", 1500)
	if rec := h.do(http.MethodPost, path, map[string]any{"body": long}, passenger); rec.Code != http.StatusCreated {
		t.Fatalf("1500 runes: status = %d, want 201; body %.200s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodPost, path, map[string]any{"body": strings.Repeat("ω", 2001)}, passenger); rec.Code != http.StatusBadRequest {
		t.Fatalf("2001 runes: status = %d, want 400", rec.Code)
	}

	rec := h.do(http.MethodPost, path, map[string]any{"body": "a\u202eb\u200bc"}, passenger)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	var stored string
	h.mustQuery(`SELECT body FROM trip_messages ORDER BY created_at DESC LIMIT 1`, &stored)
	if stored != "abc" {
		t.Errorf("stored %q, want %q", stored, "abc")
	}
}
