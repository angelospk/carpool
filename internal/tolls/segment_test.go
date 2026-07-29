package tolls_test

import (
	"math"
	"testing"

	"carpool/internal/geo"
	"carpool/internal/tolls"
)

// A straight run east along the 41st parallel, one vertex per 0.01 degree.
func straightRoute() []geo.Point {
	var pts []geo.Point
	for i := 0; i <= 10; i++ {
		pts = append(pts, geo.Point{Lat: 41, Lon: 25 + float64(i)*0.01})
	}
	return pts
}

// Board and alight points are wherever the passenger stands, not on the route.
// They have to be projected onto it before they can be compared with a barrier.
func TestProjectsPickupAndDropoffOntoTheRoute(t *testing.T) {
	route := straightRoute()

	from, to, err := tolls.Segment(route,
		geo.Point{Lat: 41.001, Lon: 25.02}, // 111 m north of the road
		geo.Point{Lat: 40.999, Lon: 25.07},
	)
	if err != nil {
		t.Fatalf("Segment: %v", err)
	}

	// 0.02 and 0.07 degrees of longitude at 41°N.
	wantFrom := 0.02 * 111320 * math.Cos(41*math.Pi/180)
	wantTo := 0.07 * 111320 * math.Cos(41*math.Pi/180)
	if math.Abs(from-wantFrom) > 50 || math.Abs(to-wantTo) > 50 {
		t.Fatalf("segment = %.0f..%.0f m, want about %.0f..%.0f", from, to, wantFrom, wantTo)
	}
}

// A passenger travelling against the driver is not a passenger for this trip.
// Silently swapping the ends would quote them for barriers they never see.
func TestRejectsASegmentThatRunsBackwards(t *testing.T) {
	route := straightRoute()

	_, _, err := tolls.Segment(route,
		geo.Point{Lat: 41, Lon: 25.08},
		geo.Point{Lat: 41, Lon: 25.02},
	)
	if err == nil {
		t.Fatal("a backwards segment was accepted")
	}
}

func TestRejectsARouteWithNothingToProjectOnto(t *testing.T) {
	_, _, err := tolls.Segment([]geo.Point{{Lat: 41, Lon: 25}},
		geo.Point{Lat: 41, Lon: 25}, geo.Point{Lat: 41, Lon: 25.1})
	if err == nil {
		t.Fatal("a one-point route was accepted")
	}
}
