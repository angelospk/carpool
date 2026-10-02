package places

import "math"

// nearestReach is how far from a phone a settlement can still be the answer
// to "where am I". Further than this is the sea, or a border, and naming a
// town forty kilometres away would be a wrong answer stated confidently.
const nearestReach = 25_000.0

// Nearest names the settlement a coordinate is in, for «Από εδώ».
//
// Plain nearest is wrong inside a city: its node sits on one square, and the
// suburbs and hamlets around it are each closer to most of the streets than
// that square is. So distance is divided by a reach that grows with the kind
// of place, and a city wins anywhere it plausibly covers.
func Nearest(lon, lat float64) (Place, bool) {
	once.Do(load)
	var (
		best      Place
		bestScore = math.Inf(1)
	)
	for _, e := range index {
		d := metres(lon, lat, e.Lon, e.Lat)
		if d > nearestReach {
			continue
		}
		if s := d / reach(e.Kind); s < bestScore {
			best, bestScore = e.Place, s
		}
	}
	return best, !math.IsInf(bestScore, 1)
}

func reach(kind string) float64 {
	switch kind {
	case "city":
		return 5
	case "town":
		return 2.5
	case "village":
		return 1
	default:
		return 0.8
	}
}

// metres between two points, flat-earth: exact enough at the scale of a town,
// and this runs over twelve thousand rows per call.
func metres(lon1, lat1, lon2, lat2 float64) float64 {
	const r = 6_371_000.0
	rad := math.Pi / 180
	x := (lon2 - lon1) * rad * math.Cos((lat1+lat2)/2*rad)
	y := (lat2 - lat1) * rad
	return r * math.Hypot(x, y)
}
