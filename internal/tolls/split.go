package tolls

import (
	"fmt"
	"math"
	"sort"
)

// Gate is one barrier on the driver's route and what it costs.
type Gate struct {
	MetresAlong float64
	Cents       int
}

// Rider is a passenger's occupancy of the driver's route, measured in metres
// from its start. The interval is half-open: someone who boards at a barrier was
// in the car for it, someone who alights at one was not.
type Rider struct {
	ID         string
	FromMetres float64
	ToMetres   float64
}

// Shares is who owes what, in cents.
type Shares struct {
	Driver int
	Riders map[string]int
}

// Split divides each barrier among the people in the car when it is paid.
//
// The rule is deliberately the one that can be explained at the roadside: every
// toll is shared by whoever was aboard for it. A passenger carried for a short
// stretch of a long motorway run pays for the barriers on that stretch and no
// others; a passenger whose whole ride is on a toll-free road pays nothing.
//
// This is not marginal-cost pricing. The driver was making the trip anyway, so
// a passenger's true marginal toll is usually zero — but so is their marginal
// fuel, and nobody considers that the fair answer either. Sharing what was
// actually spent, among those it was spent on, is the rule people recognise.
//
// Money is integer cents throughout. Exact shares rarely divide, so the
// remainder is allocated by largest remainder and the tie is broken by the
// barrier's own position rather than by name: over a season, always handing the
// odd cent to whoever sorts first is a real transfer.
func Split(gates []Gate, riders []Rider) (Shares, error) {
	seen := map[string]bool{}
	for _, r := range riders {
		// The driver rides under the empty id, so a rider carrying it would be
		// merged into the driver rather than split against them.
		if r.ID == "" {
			return Shares{}, fmt.Errorf("tolls: a rider has no id; the empty id is the driver's")
		}
		if r.ToMetres < r.FromMetres {
			return Shares{}, fmt.Errorf("tolls: rider %s alights before boarding", r.ID)
		}
		if seen[r.ID] {
			return Shares{}, fmt.Errorf("tolls: rider %s appears twice", r.ID)
		}
		seen[r.ID] = true
	}

	shares := Shares{Riders: map[string]int{}}
	for _, r := range riders {
		shares.Riders[r.ID] = 0
	}

	for _, g := range gates {
		// The driver is aboard for every barrier, by definition.
		aboard := []string{""}
		for _, r := range riders {
			if r.FromMetres <= g.MetresAlong && g.MetresAlong < r.ToMetres {
				aboard = append(aboard, r.ID)
			}
		}
		sort.Strings(aboard)

		base := g.Cents / len(aboard)
		remainder := g.Cents % len(aboard)

		// Rotate the starting point with the barrier's position, so the odd
		// cents of a multi-gate trip do not all land on the same person.
		start := 0
		if len(aboard) > 0 {
			start = int(math.Abs(g.MetresAlong)) % len(aboard)
		}

		for i, id := range aboard {
			share := base
			// The first `remainder` people, counting from the rotated start.
			if (i-start+len(aboard))%len(aboard) < remainder {
				share++
			}
			if id == "" {
				shares.Driver += share
			} else {
				shares.Riders[id] += share
			}
		}
	}

	return shares, nil
}

// Gates converts matched crossings into the barriers a split works on.
//
// The second return is false when a crossed station has no price for the class:
// a split of an incomplete total would look authoritative and be wrong.
func (r Result) Gates(c Class) ([]Gate, bool) {
	out := make([]Gate, 0, len(r.Crossings))
	for _, x := range r.Crossings {
		cents, ok := x.Station.Price(c)
		if !ok {
			return nil, false
		}
		out = append(out, Gate{MetresAlong: x.MetresAlong, Cents: cents})
	}
	return out, true
}
