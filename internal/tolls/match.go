package tolls

import (
	"math"
	"sort"

	"carpool/internal/geo"
)

// Matching thresholds.
//
// These are two-stage on purpose. The wide radius only nominates candidates;
// what actually charges money is crossing the gate line — a short segment laid
// across the carriageway at the station — in a direction that agrees with the
// station's own. Proximity alone would charge for frontage roads, for the
// carriageway on the other side of the barrier, and for overpasses.
const (
	// candidateRadiusM is generous: it only decides what is worth testing.
	candidateRadiusM = 400
	// maxApproachM is how close the route must actually come to the station.
	//
	// Measured, not guessed: across 162 traversals in the harvest — every
	// station, every direction it serves — the routed line passes within 3.2 m
	// of the station point, median 0.5 m, because the coordinates sit on the
	// road centreline. Ten metres leaves a threefold margin over the worst true
	// positive while excluding the case that motivated it: on Ναύπλιο→Αθήνα the
	// A8 runs 22 m from the Attiki Odos plaza, on the same heading, and is not
	// charged. Neither proximity nor bearing separates those two; the distance
	// the road actually keeps from the barrier does.
	maxApproachM = 10
	// gateHalfWidthM is half the gate line, laid perpendicular to the station's
	// direction of travel. It only has to be wide enough to be crossed by a road
	// that already passes within maxApproachM of the centre.
	gateHalfWidthM = 25
	// headingToleranceDeg accepts a crossing outright. An overpass crosses the
	// gate line at roughly a right angle and is refused here.
	headingToleranceDeg = 45
	// headingAmbiguousDeg is reported rather than charged.
	headingAmbiguousDeg = 70
	// headingChordM is the distance either side of the barrier used to measure
	// the route's heading. One segment is too short: OSRM geometry is dense in
	// bends, and a single 5 m segment inside a toll plaza points anywhere.
	headingChordM = 60
)

// Match returns the toll stations the route passes through, in the order they
// are met.
func (d *Dataset) Match(route []geo.Point) Result {
	res := Result{DatasetDate: d.HarvestedAt}
	if len(route) < 2 {
		return res
	}

	cumulative := cumulativeMetres(route)

	// Group members are the two carriageways of one physical plaza. Only the
	// best-agreeing member of a group may charge.
	bestInGroup := map[int]Crossing{}
	var ambiguous []Ambiguity

	for _, s := range d.Stations {
		hit, off, metres, ok := crossesGate(route, cumulative, s)
		if !ok {
			continue
		}
		if !hit {
			if off <= headingAmbiguousDeg {
				ambiguous = append(ambiguous, Ambiguity{Station: s, HeadingOff: off})
			}
			continue
		}

		candidate := Crossing{Station: s, MetresAlong: metres, HeadingOff: off}
		if existing, seen := bestInGroup[s.Group]; !seen || off < existing.HeadingOff {
			bestInGroup[s.Group] = candidate
		}
	}

	for _, c := range bestInGroup {
		res.Crossings = append(res.Crossings, c)
	}
	sort.Slice(res.Crossings, func(i, j int) bool {
		return res.Crossings[i].MetresAlong < res.Crossings[j].MetresAlong
	})
	res.Ambiguous = ambiguous
	return res
}

// crossesGate reports whether the route crosses the station's gate line, how
// far the route's heading was from the station's, and how far along the route
// the barrier is.
//
// ok is false when the route never came near the station at all, which is the
// overwhelmingly common case and worth leaving out of the ambiguity list.
func crossesGate(route []geo.Point, cumulative []float64, s Station) (hit bool, headingOff float64, metres float64, ok bool) {
	near := false
	bestOff := math.MaxFloat64
	var bestMetres float64
	closest := math.MaxFloat64

	for i := 0; i+1 < len(route); i++ {
		a, b := route[i], route[i+1]
		d := pointToSegmentM(s.Lat, s.Lon, a, b)
		if d < closest {
			closest = d
		}
		if d > candidateRadiusM {
			continue
		}
		near = true

		for _, bearing := range s.Bearings {
			p, q := gateLine(s, bearing)
			frac, crossed := segmentIntersection(a, b, p, q)
			if !crossed {
				continue
			}

			at := cumulative[i] + frac*(cumulative[i+1]-cumulative[i])
			off := angleBetween(headingAt(route, cumulative, at), bearing)
			if off < bestOff {
				bestOff, bestMetres = off, at
			}
		}
	}

	if !near {
		return false, 0, 0, false
	}
	if bestOff == math.MaxFloat64 {
		// Came close but never crossed the barrier — an exit taken before the
		// plaza, or the road on the other side of it.
		return false, 0, 0, false
	}
	if closest > maxApproachM {
		// Passing the plaza is not the same as going through it.
		return false, 0, 0, false
	}
	return bestOff <= headingToleranceDeg, bestOff, bestMetres, true
}

// gateLine returns the two ends of a segment laid across the carriageway at the
// station, perpendicular to the direction of travel it serves.
func gateLine(s Station, bearing float64) (geo.Point, geo.Point) {
	across := bearing + 90
	return offset(s.Lat, s.Lon, across, gateHalfWidthM),
		offset(s.Lat, s.Lon, across+180, gateHalfWidthM)
}

// headingAt measures the route's direction over a chord centred on a distance
// along the route, which is far steadier than the heading of one short segment.
func headingAt(route []geo.Point, cumulative []float64, at float64) float64 {
	before := pointAt(route, cumulative, at-headingChordM)
	after := pointAt(route, cumulative, at+headingChordM)
	return bearingBetween(before, after)
}

// pointAt interpolates the route at a distance along it, clamped to its ends.
func pointAt(route []geo.Point, cumulative []float64, at float64) geo.Point {
	if at <= 0 {
		return route[0]
	}
	last := len(route) - 1
	if at >= cumulative[last] {
		return route[last]
	}
	i := sort.SearchFloat64s(cumulative, at)
	if i == 0 {
		return route[0]
	}
	span := cumulative[i] - cumulative[i-1]
	if span == 0 {
		return route[i]
	}
	f := (at - cumulative[i-1]) / span
	return geo.Point{
		Lat: route[i-1].Lat + f*(route[i].Lat-route[i-1].Lat),
		Lon: route[i-1].Lon + f*(route[i].Lon-route[i-1].Lon),
	}
}

func cumulativeMetres(route []geo.Point) []float64 {
	out := make([]float64, len(route))
	for i := 1; i < len(route); i++ {
		out[i] = out[i-1] + haversineM(route[i-1], route[i])
	}
	return out
}
