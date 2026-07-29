package tolls_test

import (
	"testing"

	"carpool/internal/geo"
	"carpool/internal/tolls"
)

// Μέστης sits on the Egnatia between Komotini and Alexandroupoli, facing east.
const (
	mestiLat = 41.017236887
	mestiLon = 25.528747737
)

func loadSet(t *testing.T) *tolls.Dataset {
	t.Helper()
	set, err := tolls.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return set
}

// Found by adversarial review: the approach distance was accumulated over the
// whole route, so a road that brushed past the plaza *somewhere else* satisfied
// the 10 m rule for a crossing 20 m away — the exact false positive the rule
// exists to stop.
func TestABrushPastThePlazaDoesNotLicenceADistantCrossing(t *testing.T) {
	set := loadSet(t)

	// The motorway here runs north-west to south-east. This route brushes
	// within metres of the barrier on its way in, then crosses the gate line
	// about 20 m to the side of it — a parallel road, not the plaza.
	var route []geo.Point
	for i := -60; i <= -2; i++ {
		route = append(route, geo.Point{
			Lat: mestiLat - float64(i)*0.0007,
			Lon: mestiLon + float64(i)*0.0008,
		})
	}
	for i := 0; i <= 60; i++ {
		route = append(route, geo.Point{
			Lat: mestiLat - float64(i)*0.0007 + 0.00014,
			Lon: mestiLon + float64(i)*0.0008 + 0.00016,
		})
	}

	for _, c := range set.Match(route).Crossings {
		if c.Station.ShortEL == "Μέστης" {
			t.Fatalf("charged Μέστης for a crossing %.0f m from the barrier", c.HeadingOff)
		}
	}
}

// Also from review: both carriageways of a plaza share a group, and the group
// was deduplicated across the whole route — so a car that came back through the
// other side was charged once instead of twice.
func TestAPlazaCrossedBothWaysIsChargedTwice(t *testing.T) {
	set := loadSet(t)

	// The pair faces 137° and 317°, so the motorway runs north-west to
	// south-east here, and the two records sit on their own carriageways about
	// 18 m apart. Drive down one through the plaza, a long way on, then back up
	// the other — which is what a return trip on the same day is.
	const (
		backLat = 41.0174028 // the north-west-facing record
		backLon = 25.5287746
	)
	var route []geo.Point
	for i := -60; i <= 60; i++ {
		route = append(route, geo.Point{
			Lat: mestiLat - float64(i)*0.0007,
			Lon: mestiLon + float64(i)*0.0008,
		})
	}
	for i := 60; i >= -60; i-- {
		route = append(route, geo.Point{
			Lat: backLat - float64(i)*0.0007,
			Lon: backLon + float64(i)*0.0008,
		})
	}

	var mesti int
	for _, c := range set.Match(route).Crossings {
		// The pair share a plaza; either record counts as paying it.
		if c.Station.ShortEL == "Μέστης" {
			mesti++
		}
	}
	if mesti != 2 {
		t.Fatalf("charged the plaza %d time(s) for a there-and-back route, want 2", mesti)
	}
}
