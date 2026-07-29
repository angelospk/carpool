package match

import "sort"

// Scoring weights from MATCHING.md.
const (
	// walkMinutesPerKm converts a walk into a minute-equivalent so it can be
	// compared against the detour; 12 min/km is a normal walking pace.
	walkMinutesPerKm = 12
	// walkWeight halves that penalty: a passenger's own walk matters, but less
	// than the driver's detour, which is what gets the ride offered at all.
	walkWeight = 0.5
	// ratingWeight keeps the driver-quality nudge deliberately small. At most it
	// moves a candidate by ratingWeight × 5 minutes, so it can settle a close
	// call but never override a materially worse detour.
	ratingWeight = 2.0
	maxRating    = 5.0
)

// Scored is a candidate with everything the ranking needs.
type Scored struct {
	// ID identifies the candidate and doubles as the tie-break key.
	ID                string
	DetourMin         float64
	WalkToPickupKm    float64
	WalkFromDropoffKm float64
	// DriverRating is the driver's mean rating in [1,5], or 0 when unrated.
	DriverRating float64
}

// Score rates a candidate; lower is better.
func Score(c Scored) float64 {
	walk := (c.WalkToPickupKm + c.WalkFromDropoffKm) * walkMinutesPerKm * walkWeight

	// Normalised to [0,1] so the bonus stays inside its documented bound
	// regardless of the rating scale.
	var bonus float64
	if c.DriverRating > 0 {
		bonus = ratingWeight * (c.DriverRating / maxRating)
	}

	return c.DetourMin + walk - bonus
}

// Rank orders candidates best first.
//
// Ties break on ID, which makes the order total. That matters for more than
// tidiness: the sequential ask flow walks this list, so an unstable order would
// ask a different driver after a re-rank or a restart.
func Rank(candidates []Scored) []Scored {
	out := make([]Scored, len(candidates))
	copy(out, candidates)

	sort.Slice(out, func(i, j int) bool {
		si, sj := Score(out[i]), Score(out[j])
		if si != sj {
			return si < sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}
