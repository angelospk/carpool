package places_test

import (
	"testing"

	"carpool/internal/places"
)

// «Από εδώ»: the phone knows a coordinate and the form wants a settlement.
// These run against the embedded dataset, like the search tests beside them.
func TestNearestNamesTheTownSomebodyIsStandingIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lon, lat float64
		want     string
	}{
		// A few hundred metres off each centre, as a phone in the street is.
		{"Σουφλί, by the square", 26.3051, 41.1958, "Σουφλί"},
		{"Φέρες, by the station", 26.1695, 40.8925, "Φέρες"},
		// Inside a city, where suburbs and hamlets sit closer to the phone than
		// the city's own node does, the city is still the answer.
		{"Αλεξανδρούπολη, at the port", 25.8790, 40.8440, "Αλεξανδρούπολη"},
		{"Αλεξανδρούπολη, at the hospital", 25.8960, 40.8640, "Αλεξανδρούπολη"},
	} {
		got, ok := places.Nearest(tc.lon, tc.lat)
		if !ok || got.Name != tc.want {
			t.Errorf("%s: got %q (found %v), want %q", tc.name, got.Name, ok, tc.want)
		}
	}
}

// The middle of the Aegean is near nothing anybody can be picked up in.
func TestNearestFindsNothingFarFromAnyPlace(t *testing.T) {
	if got, ok := places.Nearest(25.0, 37.0); ok && got.Name == "" {
		t.Fatalf("found a place with no name")
	}
	if _, ok := places.Nearest(20.0, 34.0); ok {
		t.Fatal("a point far out at sea found a place")
	}
}
