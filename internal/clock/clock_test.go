package clock

import (
	"testing"
	"time"
)

func TestFakeStartsAtItsSeedInstant(t *testing.T) {
	seed := time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)

	c := NewFake(seed)

	if got := c.Now(); !got.Equal(seed) {
		t.Fatalf("Now() = %v, want %v", got, seed)
	}
}

func TestFakeAdvanceMovesTimeForward(t *testing.T) {
	seed := time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)
	c := NewFake(seed)

	c.Advance(90 * time.Minute)

	want := seed.Add(90 * time.Minute)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance, Now() = %v, want %v", got, want)
	}
}

func TestRealReportsCurrentTime(t *testing.T) {
	before := time.Now()

	got := Real{}.Now()

	if got.Before(before) {
		t.Fatalf("Real.Now() = %v, which precedes the call site's time.Now() = %v", got, before)
	}
}

// Compile-time proof that both implementations satisfy the interface the rest of
// the codebase depends on.
var (
	_ Clock = Real{}
	_ Clock = (*Fake)(nil)
)
