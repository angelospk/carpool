package tolls_test

import (
	"testing"

	"carpool/internal/geo"
	"carpool/internal/tolls"
)

// Two records at the same place are the two carriageways of one plaza. The
// group is what ties them together, and what stops a route being charged for
// the barrier on the other side of the motorway as well as its own.
func TestBothCarriagewaysOfAPlazaShareAGroup(t *testing.T) {
	set, err := tolls.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	stations := set.Stations

	// Comfortably wider than a plaza (the widest pair here is 82 m apart) and
	// far narrower than the gap between two plazas, which is kilometres.
	const plazaM = 200

	for i := range stations {
		for j := i + 1; j < len(stations); j++ {
			a, b := stations[i], stations[j]
			d := tolls.DistanceM(
				geo.Point{Lat: a.Lat, Lon: a.Lon}, geo.Point{Lat: b.Lat, Lon: b.Lon})
			if d < plazaM && a.Group != b.Group {
				t.Errorf("%s and %s are %.0f m apart but in groups %d and %d: "+
					"a route crossing both gate lines is charged for the plaza twice",
					a.ShortEL, b.ShortEL, d, a.Group, b.Group)
			}
			if d > plazaM && a.Group == b.Group {
				t.Errorf("%s and %s share group %d but are %.0f m apart: "+
					"one of them silently suppresses the other",
					a.ShortEL, b.ShortEL, a.Group, d)
			}
		}
	}
}
