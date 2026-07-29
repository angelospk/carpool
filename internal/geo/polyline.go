package geo

import (
	"fmt"
	"math"
)

// DecodePolyline turns an encoded polyline into points.
//
// Route geometry arrives encoded in several places — OSRM's default output and
// the toll dataset's source among them — and the toll matcher needs the vertices
// rather than a string.
func DecodePolyline(encoded string) ([]Point, error) {
	return DecodePolylinePrecision(encoded, 5)
}

// DecodePolylinePrecision decodes at a given precision. The app stores geometry
// at precision 6, which is a different scale, not a different format.
func DecodePolylinePrecision(encoded string, precision int) ([]Point, error) {
	scale := math.Pow(10, float64(precision))
	var (
		points []Point
		lat    int
		lon    int
		i      int
	)

	for i < len(encoded) {
		dLat, next, err := decodeValue(encoded, i)
		if err != nil {
			return nil, err
		}
		dLon, next, err := decodeValue(encoded, next)
		if err != nil {
			return nil, err
		}
		i = next

		lat += dLat
		lon += dLon
		points = append(points, Point{
			Lat: float64(lat) / scale,
			Lon: float64(lon) / scale,
		})
	}
	return points, nil
}

// decodeValue reads one zig-zag encoded varint starting at i and returns it
// with the index just past it.
func decodeValue(encoded string, i int) (value int, next int, err error) {
	var shift, result int
	for {
		if i >= len(encoded) {
			return 0, 0, fmt.Errorf("geo: polyline ends mid-value")
		}
		b := int(encoded[i]) - 63
		i++
		if b < 0 || b > 63 {
			return 0, 0, fmt.Errorf("geo: polyline contains a character outside the alphabet")
		}
		result |= (b & 0x1f) << shift
		shift += 5
		if b < 0x20 {
			break
		}
		if shift > 30 {
			return 0, 0, fmt.Errorf("geo: polyline value is too long")
		}
	}

	// The low bit is the sign, and the rest is the magnitude.
	if result&1 != 0 {
		return ^(result >> 1), i, nil
	}
	return result >> 1, i, nil
}
