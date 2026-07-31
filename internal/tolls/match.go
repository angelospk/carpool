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
	// samePassageM is how far apart two crossings of one plaza have to be before
	// they count as two separate passages through it. Dense geometry can cross
	// one gate line twice; a car that loops back through the same plaza an hour
	// later has genuinely paid twice.
	samePassageM = 500
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

	var accepted []Crossing
	var ambiguous []Ambiguity

	for _, s := range d.Stations {
		passes, near := gatePasses(route, cumulative, s)
		if !near {
			continue
		}
		for _, p := range passes {
			if p.headingOff > headingToleranceDeg {
				if p.headingOff <= headingAmbiguousDeg {
					ambiguous = append(ambiguous, Ambiguity{Station: s, HeadingOff: p.headingOff})
				}
				continue
			}
			accepted = append(accepted, Crossing{
				Station: s, MetresAlong: p.metres, HeadingOff: p.headingOff,
			})
		}
	}

	sort.Slice(accepted, func(i, j int) bool {
		return accepted[i].MetresAlong < accepted[j].MetresAlong
	})

	// Both carriageways of one plaza sit at the same place, so a route can
	// appear to cross both. Only the better-agreeing one is real — but only
	// within a single passage: a route that comes back the other way later has
	// genuinely paid the plaza twice, and must be charged twice.
	for _, c := range accepted {
		if i := lastOfGroupNear(res.Crossings, c); i >= 0 {
			if c.HeadingOff < res.Crossings[i].HeadingOff {
				res.Crossings[i] = c
			}
			continue
		}
		res.Crossings = append(res.Crossings, c)
	}
	// Replacing a crossing can seat a later one at an earlier index, so the
	// route order callers rely on is restored rather than assumed.
	sort.Slice(res.Crossings, func(i, j int) bool {
		return res.Crossings[i].MetresAlong < res.Crossings[j].MetresAlong
	})

	res.Ambiguous = ambiguous
	return res
}

// pass is one crossing of one station's gate line.
type pass struct {
	metres     float64
	headingOff float64
}

// gatePasses returns every place the route crosses this station's gate line.
//
// near is false when the route never came within the candidate radius, which is
// the overwhelmingly common case and worth leaving out of the ambiguity list.
//
// Consecutive segments crossing the same gate are one passage: a plaza is wide
// enough that dense geometry can cross the line more than once while the car
// goes through it exactly once.
func gatePasses(route []geo.Point, cumulative []float64, s Station) (passes []pass, near bool) {
	for i := 0; i+1 < len(route); i++ {
		a, b := route[i], route[i+1]
		// The approach distance is judged on the segment that crosses the
		// barrier, never on the route as a whole: a road that brushes past the
		// plaza somewhere else says nothing about whether this crossing is real.
		d := pointToSegmentM(s.Lat, s.Lon, a, b)
		if d > candidateRadiusM {
			continue
		}
		near = true
		if d > maxApproachM {
			continue
		}

		best := math.MaxFloat64
		var at float64
		for _, bearing := range s.Bearings {
			p, q := gateLine(s, bearing)
			frac, crossed := segmentIntersection(a, b, p, q)
			if !crossed {
				continue
			}
			metres := cumulative[i] + frac*(cumulative[i+1]-cumulative[i])
			off := angleBetween(headingAt(route, cumulative, metres), bearing)
			if off < best {
				best, at = off, metres
			}
		}
		if best == math.MaxFloat64 {
			continue
		}

		if n := len(passes); n > 0 && at-passes[n-1].metres < samePassageM {
			if best < passes[n-1].headingOff {
				passes[n-1] = pass{metres: at, headingOff: best}
			}
			continue
		}
		passes = append(passes, pass{metres: at, headingOff: best})
	}
	return passes, near
}

// lastOfGroupNear finds an already-accepted crossing of the same physical plaza
// close enough to be the same passage through it.
func lastOfGroupNear(accepted []Crossing, c Crossing) int {
	for i := len(accepted) - 1; i >= 0; i-- {
		if accepted[i].MetresAlong < c.MetresAlong-samePassageM {
			return -1
		}
		if accepted[i].Station.Group == c.Station.Group {
			return i
		}
	}
	return -1
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
