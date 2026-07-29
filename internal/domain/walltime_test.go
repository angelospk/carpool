package domain

import (
	"testing"
	"time"
)

// Athens follows EU DST rules: forward on the last Sunday of March at 03:00
// local (03:00 EET -> 04:00 EEST), back on the last Sunday of October at 04:00
// local (04:00 EEST -> 03:00 EET). In 2026 those dates are 29 March and 25
// October, so 03:30 local is the canonical nonexistent/repeated wall time.
func athens(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		t.Fatalf("loading Europe/Athens: %v", err)
	}
	return loc
}

func TestResolveWallTimeReturnsOneInstantForAnOrdinaryDay(t *testing.T) {
	loc := athens(t)

	got := ResolveWallTime(loc, 2026, time.July, 15, 7, 30)

	if len(got) != 1 {
		t.Fatalf("got %d instants, want exactly 1: %v", len(got), got)
	}
	// July is EEST (UTC+3), so 07:30 Athens is 04:30 UTC.
	want := time.Date(2026, time.July, 15, 4, 30, 0, 0, time.UTC)
	if !got[0].Equal(want) {
		t.Fatalf("got %v, want %v", got[0].UTC(), want)
	}
}

func TestResolveWallTimeReturnsNothingForASkippedSpringForwardTime(t *testing.T) {
	loc := athens(t)

	got := ResolveWallTime(loc, 2026, time.March, 29, 3, 30)

	if len(got) != 0 {
		t.Fatalf("03:30 on 2026-03-29 does not exist in Athens, got %d instants: %v", len(got), got)
	}
}

func TestResolveWallTimeReturnsTwoInstantsForARepeatedFallBackTime(t *testing.T) {
	loc := athens(t)

	got := ResolveWallTime(loc, 2026, time.October, 25, 3, 30)

	if len(got) != 2 {
		t.Fatalf("03:30 on 2026-10-25 happens twice in Athens, got %d instants: %v", len(got), got)
	}
	if !got[0].Before(got[1]) {
		t.Fatalf("results must be sorted earliest first, got %v then %v", got[0], got[1])
	}
	// The first pass is still EEST (UTC+3) -> 00:30 UTC; the second is EET
	// (UTC+2) -> 01:30 UTC.
	wantFirst := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	wantSecond := time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC)
	if !got[0].Equal(wantFirst) {
		t.Errorf("first instant = %v, want %v", got[0].UTC(), wantFirst)
	}
	if !got[1].Equal(wantSecond) {
		t.Errorf("second instant = %v, want %v", got[1].UTC(), wantSecond)
	}
}

func TestResolveWallTimeKeepsTheLocalHourAcrossADSTBoundary(t *testing.T) {
	loc := athens(t)

	// A 07:30 commute must stay 07:30 for the driver on both sides of the
	// October transition, even though its UTC offset changes.
	before := ResolveWallTime(loc, 2026, time.October, 23, 7, 30)
	after := ResolveWallTime(loc, 2026, time.October, 27, 7, 30)

	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("expected one instant each, got %d and %d", len(before), len(after))
	}
	if h := before[0].In(loc).Hour(); h != 7 {
		t.Errorf("pre-transition local hour = %d, want 7", h)
	}
	if h := after[0].In(loc).Hour(); h != 7 {
		t.Errorf("post-transition local hour = %d, want 7", h)
	}
	if before[0].UTC().Hour() == after[0].UTC().Hour() {
		t.Error("the UTC hour should shift across the transition; it did not, so the offset was not applied")
	}
}
