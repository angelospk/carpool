package match

import (
	"context"
	"errors"
	"fmt"

	"carpool/internal/geo"
)

// MaxConcurrentParties is the MVP cap from MATCHING.md. It keeps the insertion
// search trivially small; raising it grows the search and the OSRM bill, and is
// explicitly a post-MVP decision.
const MaxConcurrentParties = 2

// maxStops bounds the itinerary the insertion search will consider. The party
// cap limits distinct parties, not stops, so a malformed itinerary could
// otherwise turn into a quadratic fan-out of OSRM calls.
const maxStops = 8

var (
	// ErrPartyCapReached means the occurrence already carries the maximum number
	// of passenger parties.
	ErrPartyCapReached = errors.New("match: occurrence already has the maximum passenger parties")
	// ErrItineraryTooComplex means the itinerary has more stops than the
	// insertion search is willing to route.
	ErrItineraryTooComplex = errors.New("match: itinerary has too many stops to evaluate")
	// ErrNoViableInsertion means every valid ordering was unroutable.
	ErrNoViableInsertion = errors.New("match: no routable way to fit this passenger in")
)

// StopKind distinguishes a pickup from a dropoff.
type StopKind string

const (
	StopPickup  StopKind = "pickup"
	StopDropoff StopKind = "dropoff"
)

// Stop is one intermediate waypoint on a driver's itinerary.
type Stop struct {
	Point   geo.Point
	Kind    StopKind
	PartyID string
}

// Itinerary is a driver's current plan: their own endpoints plus the stops for
// passengers already aboard, in travel order.
type Itinerary struct {
	Origin      geo.Point
	Destination geo.Point
	Stops       []Stop
}

// Router computes a route through waypoints in order. *geo.OSRM satisfies it.
type Router interface {
	Route(ctx context.Context, waypoints []geo.Point) (geo.Route, error)
}

// Insertion is the cheapest way to fit a new passenger into an itinerary.
type Insertion struct {
	// DetourMin and DetourKm are the increments over the driver's current plan.
	DetourMin float64
	DetourKm  float64
	// Waypoints is the resulting itinerary, driver origin first.
	Waypoints []geo.Point
}

// BestInsertion finds the cheapest valid way to add a pickup and dropoff to an
// itinerary, measured as the increase over the driver's current journey.
//
// Every ordering that keeps each party's pickup ahead of their own dropoff is
// tried and routed. With the two-party cap the candidate set is a handful of
// orderings, so brute force is both simple and cheap; that is exactly why the
// cap exists.
func BestInsertion(ctx context.Context, router Router, itin Itinerary, pickup, dropoff geo.Point) (Insertion, error) {
	if parties(itin) >= MaxConcurrentParties {
		return Insertion{}, ErrPartyCapReached
	}
	if len(itin.Stops) > maxStops {
		return Insertion{}, fmt.Errorf("%w: %d stops", ErrItineraryTooComplex, len(itin.Stops))
	}

	baseline, err := router.Route(ctx, itin.waypoints())
	if err != nil {
		return Insertion{}, fmt.Errorf("match: routing the current itinerary: %w", err)
	}

	newStops := []Stop{
		{Point: pickup, Kind: StopPickup, PartyID: "new"},
		{Point: dropoff, Kind: StopDropoff, PartyID: "new"},
	}

	var best *Insertion
	// Insert the pickup at position i and the dropoff at position j >= i, which
	// enumerates exactly the orderings that keep the new party's stops in order
	// while preserving the relative order of everyone already aboard.
	for i := 0; i <= len(itin.Stops); i++ {
		for j := i; j <= len(itin.Stops); j++ {
			candidate := spliceStops(itin.Stops, i, j, newStops[0], newStops[1])
			trial := Itinerary{Origin: itin.Origin, Destination: itin.Destination, Stops: candidate}

			routed, err := router.Route(ctx, trial.waypoints())
			if errors.Is(err, geo.ErrNoRoute) {
				// This particular ordering has no path; another may. Giving up
				// here would silently drop a driver who could take the ride.
				continue
			}
			if err != nil {
				// Anything else is the routing service failing, which must not be
				// mistaken for "this driver cannot take them".
				return Insertion{}, fmt.Errorf("match: routing a candidate itinerary: %w", err)
			}

			ins := Insertion{
				DetourMin: (routed.Duration - baseline.Duration).Minutes(),
				DetourKm:  (routed.DistanceMeters - baseline.DistanceMeters) / 1000,
				Waypoints: trial.waypoints(),
			}
			if best == nil || ins.DetourMin < best.DetourMin {
				cp := ins
				best = &cp
			}
		}
	}

	if best == nil {
		return Insertion{}, ErrNoViableInsertion
	}
	// A negative detour is not meaningful; clamp so callers and stored values
	// stay non-negative (the schema enforces this too).
	if best.DetourMin < 0 {
		best.DetourMin = 0
	}
	if best.DetourKm < 0 {
		best.DetourKm = 0
	}
	return *best, nil
}

// WithinBudget reports whether a detour fits the driver's stated budget.
func (i Insertion) WithinBudget(budgetMin int) bool {
	return i.DetourMin <= float64(budgetMin)
}

// waypoints renders an itinerary as an ordered coordinate list.
func (it Itinerary) waypoints() []geo.Point {
	out := make([]geo.Point, 0, len(it.Stops)+2)
	out = append(out, it.Origin)
	for _, s := range it.Stops {
		out = append(out, s.Point)
	}
	return append(out, it.Destination)
}

// parties counts the distinct passenger parties on an itinerary.
//
// This counts parties per occurrence rather than parties concurrently aboard.
// The stricter reading is deliberate for the MVP: it keeps the cap a single
// integer instead of a sweep over pickup/dropoff events, and it can only refuse
// rides that a concurrency-aware cap would allow — never admit ones it would
// not. Revisit alongside raising the cap itself (MATCHING.md lists that as a
// post-MVP decision).
func parties(it Itinerary) int {
	seen := make(map[string]struct{}, len(it.Stops))
	for _, s := range it.Stops {
		seen[s.PartyID] = struct{}{}
	}
	return len(seen)
}

// spliceStops returns stops with pickup inserted at index i and dropoff at
// index j, counted against the original slice.
func spliceStops(stops []Stop, i, j int, pickup, dropoff Stop) []Stop {
	out := make([]Stop, 0, len(stops)+2)
	for idx := 0; idx <= len(stops); idx++ {
		if idx == i {
			out = append(out, pickup)
		}
		if idx == j {
			out = append(out, dropoff)
		}
		if idx < len(stops) {
			out = append(out, stops[idx])
		}
	}
	return out
}
