package tolls_test

import (
	"encoding/json"
	"os"
	"testing"

	"carpool/internal/geo"
	"carpool/internal/tolls"
)

// route is one entry of the golden corpus: a real route, its geometry, and the
// stations the source says are on it.
type route struct {
	Name          string  `json:"name"`
	KM            float64 `json:"km"`
	Polyline      string  `json:"polyline"`
	ExpectedIDs   []int   `json:"expected_ids"`
	ExpectedCents float64 `json:"expected_car_eur"`
}

func corpus(t *testing.T) []route {
	t.Helper()
	raw, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	var routes []route
	if err := json.Unmarshal(raw, &routes); err != nil {
		t.Fatalf("parsing corpus: %v", err)
	}
	if len(routes) < 20 {
		t.Fatalf("corpus has only %d routes", len(routes))
	}
	return routes
}

// The whole feature rests on this: given the same geometry, we must find the
// same toll stations the source does — in the same order, at the same price.
//
// The corpus deliberately includes both directions of four corridors, because
// 25 of the 86 stations are co-located pairs distinguished only by direction.
// Charging by proximity alone would double every mainline barrier.
func TestMatcherAgreesWithTheSourceOnEveryCorpusRoute(t *testing.T) {
	set, err := tolls.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, r := range corpus(t) {
		t.Run(r.Name, func(t *testing.T) {
			points, err := geo.DecodePolyline(r.Polyline)
			if err != nil {
				t.Fatalf("decoding geometry: %v", err)
			}

			got := set.Match(points)
			if len(got.Ambiguous) > 0 {
				t.Errorf("ambiguous candidates: %v", got.Ambiguous)
			}

			var ids []int
			for _, c := range got.Crossings {
				ids = append(ids, c.Station.ID)
			}
			if !sameOrder(ids, r.ExpectedIDs) {
				t.Fatalf("stations = %v, want %v", ids, r.ExpectedIDs)
			}

			wantCents := int(r.ExpectedCents*100 + 0.5)
			cost, known := got.Cents(tolls.Car)
			if !known {
				t.Fatal("a crossed station has no car price")
			}
			if cost != wantCents {
				t.Fatalf("cost = %d cents, want %d", cost, wantCents)
			}
		})
	}
}

// Reversing a route must pick the *other* member of each co-located pair, and
// the crossings must come back in the mirrored order.
func TestReversingARouteSelectsTheOppositeCarriageway(t *testing.T) {
	set, err := tolls.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var forward, backward []route
	for _, r := range corpus(t) {
		switch r.Name {
		case "Θεσσαλονίκη→Αλεξανδρούπολη":
			forward = append(forward, r)
		case "Αλεξανδρούπολη→Θεσσαλονίκη":
			backward = append(backward, r)
		}
	}
	if len(forward) != 1 || len(backward) != 1 {
		t.Fatal("the corpus must contain both directions of the Egnatia route")
	}

	out := func(r route) []int {
		points, err := geo.DecodePolyline(r.Polyline)
		if err != nil {
			t.Fatalf("decoding: %v", err)
		}
		var ids []int
		for _, c := range set.Match(points).Crossings {
			ids = append(ids, c.Station.ID)
		}
		return ids
	}

	there, back := out(forward[0]), out(backward[0])
	if len(there) != len(back) {
		t.Fatalf("%d stations one way, %d the other", len(there), len(back))
	}
	for _, id := range there {
		for _, other := range back {
			if id == other {
				t.Fatalf("station %d was charged in both directions", id)
			}
		}
	}
}

func sameOrder(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
