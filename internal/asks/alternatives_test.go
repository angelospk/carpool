package asks_test

import (
	"context"
	"testing"

	"carpool/internal/geo"
)

// The list said a car was on the road when only the gross filter had looked
// at it. The routed check then refused it, so the passenger was shown a car
// nobody would ever ask (QA 2026-10-16, B4).
func TestAlternativesListOnlyCarsTheRoutedCheckAccepts(t *testing.T) {
	cases := map[string]struct {
		router straightLineRouter
		want   int
	}{
		"a road the detour fits": {straightLineRouter{}, 1},
		"no road":                {straightLineRouter{err: geo.ErrNoRoute}, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mustExec(t, f, `DELETE FROM asks`)
			mustExec(t, f, `UPDATE ride_requests SET status = 'open'`)

			got, err := f.service().WithRouter(c.router).
				Alternatives(context.Background(), f.requestID, f.passengerID, 5)
			if err != nil {
				t.Fatalf("Alternatives: %v", err)
			}
			if len(got) != c.want {
				t.Fatalf("%d alternatives, want %d", len(got), c.want)
			}
		})
	}
}
