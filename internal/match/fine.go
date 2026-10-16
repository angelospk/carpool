package match

import (
	"context"
	"errors"
	"fmt"
	"math"

	"time"

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

// StopDwell is what pulling over, waiting and pulling out again costs, per
// stop. A pickup exactly on the route changes the road distance by nothing and
// still takes real minutes out of the driver's day, so the total detour counts
// it. The driver's minute limit does not: it is minutes of driving, and a
// driver who wants no stops at all says so with AllowStops.
const StopDwell = 2 * time.Minute

// Insertion is the cheapest way to fit a new passenger into an itinerary.
type Insertion struct {
	// DetourMin and DetourKm are what this passenger adds to the plan as it
	// stands. This is what the driver is being asked to accept right now.
	DetourMin float64
	DetourKm  float64

	// TotalDetourMin and TotalDetourKm are measured from the driver travelling
	// alone, and are what a budget is judged against.
	//
	// Judging the marginal figure instead is the bug this exists for: three
	// passengers each costing eight minutes all pass an eight-minute budget,
	// and the driver arrives twenty-four minutes late having agreed to eight.
	TotalDetourMin float64
	TotalDetourKm  float64

	// NewStops counts the waypoints this insertion adds beyond the driver's own
	// endpoints, which is what a driver refusing intermediate stops is refusing.
	NewStops int

	// Waypoints is the resulting itinerary, driver origin first.
	Waypoints []geo.Point
}

// Budget is what a driver agreed to for one trip.
type Budget struct {
	// AllowStops is whether they want intermediate stops at all. It is not the
	// same as a zero budget: stopping costs time even where the map says the
	// detour is nothing.
	AllowStops bool
	// Km is the extra road distance they will drive, which is the figure they
	// actually chose. Km is the increase in road distance over their own
	// journey, not how far off the line the pickup sits.
	Km float64
	// Min is the time limit over the same journey. It exists because the two do
	// not track each other: fifteen kilometres on the Egnatia is nine minutes
	// and fifteen up a mountain road is thirty-five, and a driver who said
	// fifteen kilometres did not agree to thirty-five minutes. It counts
	// driving only, not the time stops take (see StopDwell).
	Min int
	// OffRoadMin, when set, replaces Min for a drive: the minutes the driver
	// will spend off their road, not counting the time stops take. A driver
	// who said "my road only" still stops for somebody standing on it.
	OffRoadMin *int
}

// BestInsertion finds the cheapest valid way to add a pickup and dropoff to an
// itinerary, measured as the increase over the driver's current journey.
//
// Every ordering that keeps each party's pickup ahead of their own dropoff is
// tried and routed. With the two-party cap the candidate set is a handful of
// orderings, so brute force is both simple and cheap; that is exactly why the
// cap exists.
func BestInsertion(ctx context.Context, router Router, itin Itinerary, pickup, dropoff geo.Point) (Insertion, error) {
	// An unbounded budget: every ordering is eligible and the cheapest wins.
	return BestInsertionWithin(ctx, router, itin, pickup, dropoff,
		Budget{AllowStops: true, Km: math.Inf(1), Min: math.MaxInt32})
}

// BestInsertionWithin is BestInsertion confined to what the driver agreed to.
//
// The budget shapes the choice rather than judging it afterwards. Picking the
// cheapest ordering and then asking whether it fits throws away an ordering
// that would have worked: the passenger is told no by a driver who could have
// taken them, and nothing anywhere says why.
func BestInsertionWithin(ctx context.Context, router Router, itin Itinerary, pickup, dropoff geo.Point, budget Budget) (Insertion, error) {
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

	// The driver's own journey, with nobody aboard. Every budget is judged
	// against this rather than against the itinerary as it stands, so that
	// passengers cannot each spend the whole allowance.
	alone := baseline
	if len(itin.Stops) > 0 {
		alone, err = router.Route(ctx, []geo.Point{itin.Origin, itin.Destination})
		if err != nil {
			return Insertion{}, fmt.Errorf("match: routing the driver's own journey: %w", err)
		}
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

			newStops := len(candidate)
			ins := Insertion{
				DetourMin: (routed.Duration - baseline.Duration).Minutes(),
				DetourKm:  (routed.DistanceMeters - baseline.DistanceMeters) / 1000,
				TotalDetourMin: (routed.Duration - alone.Duration).Minutes() +
					float64(newStops)*StopDwell.Minutes(),
				TotalDetourKm: (routed.DistanceMeters - alone.DistanceMeters) / 1000,
				NewStops:      newStops,
				Waypoints:     trial.waypoints(),
			}
			if !ins.WithinBudget(budget) {
				continue
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
	if best.TotalDetourKm < 0 {
		best.TotalDetourKm = 0
	}
	if best.TotalDetourMin < 0 {
		best.TotalDetourMin = 0
	}
	return *best, nil
}

// WithinBudget reports whether an insertion fits what the driver agreed to.
//
// Both limits are checked, and both against the total rather than the
// increment, for the reasons on Budget and Insertion above.
func (i Insertion) WithinBudget(b Budget) bool {
	if !b.AllowStops && i.NewStops > 0 {
		return false
	}
	if i.TotalDetourKm > b.Km {
		return false
	}
	// Minutes of driving off the road. The time stops take is in the total,
	// and not in what the driver agreed to: "10 λεπτά" left six for the road
	// once two stops had taken theirs, while the driver was shown only the
	// driving (QA 2026-10-16).
	driving := i.TotalDetourMin - float64(i.NewStops)*StopDwell.Minutes()
	if b.OffRoadMin != nil {
		return driving <= float64(*b.OffRoadMin)
	}
	return driving <= float64(b.Min)
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
