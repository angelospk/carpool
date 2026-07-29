package tolls

import (
	"math"

	"carpool/internal/geo"
)

const earthRadiusM = 6371008.8

// Distances here are small — a few hundred metres at most — so the geometry
// works on a local flat projection around the point of interest. That keeps the
// segment maths ordinary two-dimensional algebra without the error a global
// projection would introduce at Greek latitudes.

func haversineM(a, b geo.Point) float64 {
	φ1, φ2 := rad(a.Lat), rad(b.Lat)
	dφ, dλ := rad(b.Lat-a.Lat), rad(b.Lon-a.Lon)
	h := math.Sin(dφ/2)*math.Sin(dφ/2) + math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// bearingBetween is the compass bearing from a to b, in degrees from north.
func bearingBetween(a, b geo.Point) float64 {
	φ1, φ2 := rad(a.Lat), rad(b.Lat)
	dλ := rad(b.Lon - a.Lon)
	y := math.Sin(dλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(dλ)
	return math.Mod(deg(math.Atan2(y, x))+360, 360)
}

// angleBetween is the smaller angle between two bearings, so 359° and 1° are
// two degrees apart rather than 358.
func angleBetween(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		return 360 - d
	}
	return d
}

// offset returns the point a given distance from another along a bearing.
func offset(lat, lon, bearing, metres float64) geo.Point {
	δ := metres / earthRadiusM
	θ := rad(bearing)
	φ1, λ1 := rad(lat), rad(lon)

	φ2 := math.Asin(math.Sin(φ1)*math.Cos(δ) + math.Cos(φ1)*math.Sin(δ)*math.Cos(θ))
	λ2 := λ1 + math.Atan2(math.Sin(θ)*math.Sin(δ)*math.Cos(φ1), math.Cos(δ)-math.Sin(φ1)*math.Sin(φ2))
	return geo.Point{Lat: deg(φ2), Lon: deg(λ2)}
}

// pointToSegmentM is the distance from a point to a segment, not merely to its
// ends: OSRM geometry can put kilometres between vertices on a straight
// motorway, and a vertex-only test would miss a station between two of them.
func pointToSegmentM(lat, lon float64, a, b geo.Point) float64 {
	// Flatten around the point, metres per degree.
	mx := 111320 * math.Cos(rad(lat))
	my := 110540.0

	px, py := 0.0, 0.0
	ax, ay := (a.Lon-lon)*mx, (a.Lat-lat)*my
	bx, by := (b.Lon-lon)*mx, (b.Lat-lat)*my

	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(ax-px, ay-py)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(ax+t*dx-px, ay+t*dy-py)
}

// segmentIntersection reports whether ab crosses pq, and how far along ab it
// happens. Segments that merely touch at an endpoint count: a route vertex
// landing exactly on a barrier is a crossing, not a near miss.
func segmentIntersection(a, b, p, q geo.Point) (fracAlongAB float64, crossed bool) {
	// Flatten around a; all four points are within a few hundred metres.
	mx := 111320 * math.Cos(rad(a.Lat))
	my := 110540.0
	x := func(pt geo.Point) float64 { return (pt.Lon - a.Lon) * mx }
	y := func(pt geo.Point) float64 { return (pt.Lat - a.Lat) * my }

	ax, ay := 0.0, 0.0
	bx, by := x(b), y(b)
	px, py := x(p), y(p)
	qx, qy := x(q), y(q)

	rx, ry := bx-ax, by-ay
	sx, sy := qx-px, qy-py

	denom := rx*sy - ry*sx
	if denom == 0 {
		return 0, false // parallel, including collinear
	}
	t := ((px-ax)*sy - (py-ay)*sx) / denom
	u := ((px-ax)*ry - (py-ay)*rx) / denom
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// DistanceM is the great-circle distance between two points, in metres. It is
// exported for callers that need the same measure the matcher uses.
func DistanceM(a, b geo.Point) float64 { return haversineM(a, b) }
