package tolls

import (
	"fmt"
	"math"

	"carpool/internal/geo"
)

// Segment projects a pickup and a drop-off onto a route and returns how far
// along it each lies, in metres.
//
// Everything about the split — where the barriers are, where a passenger gets
// in and out — has to be measured the same way, or a passenger can end up on
// the wrong side of a barrier by arithmetic alone. That measure is metres along
// the geometry, computed here, and never a fraction borrowed from elsewhere:
// PostGIS's ST_LineLocatePoint measures a fraction of the line's length in
// coordinate space, which is not the same quantity at Greek latitudes.
func Segment(route []geo.Point, pickup, dropoff geo.Point) (fromM, toM float64, err error) {
	if len(route) < 2 {
		return 0, 0, fmt.Errorf("tolls: a route needs at least two points")
	}
	cumulative := cumulativeMetres(route)

	fromM = project(route, cumulative, pickup)
	toM = project(route, cumulative, dropoff)
	if toM < fromM {
		return 0, 0, fmt.Errorf("tolls: the drop-off lies before the pickup on this route")
	}
	return fromM, toM, nil
}

// project returns the distance along the route of the closest point to p,
// measuring to the segments rather than to the vertices: motorway geometry can
// run kilometres between vertices, and snapping to the nearer end of such a
// segment would move a passenger past a barrier.
func project(route []geo.Point, cumulative []float64, p geo.Point) float64 {
	best := math.MaxFloat64
	var at float64

	for i := 0; i+1 < len(route); i++ {
		a, b := route[i], route[i+1]
		d := pointToSegmentM(p.Lat, p.Lon, a, b)
		if d >= best {
			continue
		}
		best = d
		at = cumulative[i] + fractionAlong(a, b, p)*(cumulative[i+1]-cumulative[i])
	}
	return at
}

// fractionAlong is how far between a and b the closest point to p lies.
func fractionAlong(a, b, p geo.Point) float64 {
	mx := 111320 * math.Cos(rad(a.Lat))
	my := 110540.0
	ax, ay := 0.0, 0.0
	bx, by := (b.Lon-a.Lon)*mx, (b.Lat-a.Lat)*my
	px, py := (p.Lon-a.Lon)*mx, (p.Lat-a.Lat)*my

	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return 0
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	return math.Max(0, math.Min(1, t))
}
