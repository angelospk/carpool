package match_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"carpool/internal/geo"
	"carpool/internal/match"
)

// straightLineRouter is a deterministic stand-in for OSRM: it charges the
// great-circle distance between consecutive waypoints at a fixed 60 km/h, so
// every expected detour below is arithmetic rather than a recorded fixture.
type straightLineRouter struct {
	calls int
	err   error
}

func (r *straightLineRouter) Route(_ context.Context, waypoints []geo.Point) (geo.Route, error) {
	r.calls++
	if r.err != nil {
		return geo.Route{}, r.err
	}
	var metres float64
	for i := 1; i < len(waypoints); i++ {
		metres += haversine(waypoints[i-1], waypoints[i])
	}
	return geo.Route{
		DistanceMeters: metres,
		Duration:       time.Duration(metres / 1000 / 60 * float64(time.Hour)),
	}, nil
}

func haversine(a, b geo.Point) float64 {
	const r = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(b.Lat - a.Lat)
	dLon := toRad(b.Lon - a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(a.Lat))*math.Cos(toRad(b.Lat))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// The corridor runs west→east along latitude 40.85.
var (
	routeStart = geo.Point{Lon: 25.80, Lat: 40.85}
	routeEnd   = geo.Point{Lon: 26.00, Lat: 40.85}
)

func emptyItinerary() match.Itinerary {
	return match.Itinerary{Origin: routeStart, Destination: routeEnd}
}

func TestBestInsertionCostsNothingForAPickupExactlyOnTheRoute(t *testing.T) {
	router := &straightLineRouter{}

	got, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.85}, geo.Point{Lon: 25.95, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	// Both stops sit on the straight line, so the journey length is unchanged.
	if got.DetourMin > 0.01 {
		t.Fatalf("detour = %v min, want ~0 for stops directly on the route", got.DetourMin)
	}
}

func TestBestInsertionChargesForAPickupOffTheRoute(t *testing.T) {
	router := &straightLineRouter{}

	got, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.88}, geo.Point{Lon: 25.95, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	if got.DetourMin <= 0 {
		t.Fatalf("detour = %v min, want a positive detour for a pickup off the route", got.DetourMin)
	}
	if got.DetourKm <= 0 {
		t.Fatalf("detour = %v km, want a positive distance", got.DetourKm)
	}
}

func TestBestInsertionKeepsPickupBeforeDropoff(t *testing.T) {
	router := &straightLineRouter{}

	got, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.85}, geo.Point{Lon: 25.95, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	var pickupAt, dropoffAt = -1, -1
	for i, w := range got.Waypoints {
		if w == (geo.Point{Lon: 25.85, Lat: 40.85}) {
			pickupAt = i
		}
		if w == (geo.Point{Lon: 25.95, Lat: 40.85}) {
			dropoffAt = i
		}
	}
	if pickupAt < 0 || dropoffAt < 0 {
		t.Fatalf("both stops should appear in the itinerary, got %+v", got.Waypoints)
	}
	if pickupAt >= dropoffAt {
		t.Fatalf("pickup at %d must come before dropoff at %d", pickupAt, dropoffAt)
	}
}

func TestBestInsertionStartsAndEndsAtTheDriversOwnEndpoints(t *testing.T) {
	router := &straightLineRouter{}

	got, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.86}, geo.Point{Lon: 25.95, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	if got.Waypoints[0] != routeStart {
		t.Errorf("itinerary starts at %+v, want the driver's origin %+v", got.Waypoints[0], routeStart)
	}
	if last := got.Waypoints[len(got.Waypoints)-1]; last != routeEnd {
		t.Errorf("itinerary ends at %+v, want the driver's destination %+v", last, routeEnd)
	}
}

// With one party already aboard the search must consider every ordering that
// keeps each passenger's pickup before their own dropoff.
func TestBestInsertionRespectsAnExistingPassengersStopOrder(t *testing.T) {
	router := &straightLineRouter{}
	existingPickup := geo.Point{Lon: 25.84, Lat: 40.85}
	existingDropoff := geo.Point{Lon: 25.96, Lat: 40.85}

	itin := match.Itinerary{
		Origin:      routeStart,
		Destination: routeEnd,
		Stops: []match.Stop{
			{Point: existingPickup, Kind: match.StopPickup, PartyID: "a"},
			{Point: existingDropoff, Kind: match.StopDropoff, PartyID: "a"},
		},
	}

	got, err := match.BestInsertion(context.Background(), router, itin,
		geo.Point{Lon: 25.88, Lat: 40.85}, geo.Point{Lon: 25.92, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	idx := func(p geo.Point) int {
		for i, w := range got.Waypoints {
			if w == p {
				return i
			}
		}
		return -1
	}
	if idx(existingPickup) < 0 || idx(existingDropoff) < 0 {
		t.Fatalf("the existing passenger's stops were dropped: %+v", got.Waypoints)
	}
	if idx(existingPickup) >= idx(existingDropoff) {
		t.Fatal("the existing passenger's pickup must still precede their dropoff")
	}
}

func TestBestInsertionRejectsAThirdConcurrentParty(t *testing.T) {
	router := &straightLineRouter{}
	itin := match.Itinerary{
		Origin:      routeStart,
		Destination: routeEnd,
		Stops: []match.Stop{
			{Point: geo.Point{Lon: 25.84, Lat: 40.85}, Kind: match.StopPickup, PartyID: "a"},
			{Point: geo.Point{Lon: 25.96, Lat: 40.85}, Kind: match.StopDropoff, PartyID: "a"},
			{Point: geo.Point{Lon: 25.86, Lat: 40.85}, Kind: match.StopPickup, PartyID: "b"},
			{Point: geo.Point{Lon: 25.94, Lat: 40.85}, Kind: match.StopDropoff, PartyID: "b"},
		},
	}

	_, err := match.BestInsertion(context.Background(), router, itin,
		geo.Point{Lon: 25.88, Lat: 40.85}, geo.Point{Lon: 25.92, Lat: 40.85})

	// MVP caps an occurrence at two concurrent passenger parties.
	if !errors.Is(err, match.ErrPartyCapReached) {
		t.Fatalf("error = %v, want ErrPartyCapReached", err)
	}
}

func TestBestInsertionPropagatesRouterFailure(t *testing.T) {
	router := &straightLineRouter{err: errors.New("osrm exploded")}

	_, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.85}, geo.Point{Lon: 25.95, Lat: 40.85})

	if err == nil {
		t.Fatal("a routing failure must not be silently treated as a zero detour")
	}
}

func TestBestInsertionPicksTheCheapestOrdering(t *testing.T) {
	router := &straightLineRouter{}
	// An existing party is dropped off early in the west; the new party travels
	// entirely in the east. The cheapest plan keeps each pair together rather
	// than interleaving them.
	itin := match.Itinerary{
		Origin:      routeStart,
		Destination: routeEnd,
		Stops: []match.Stop{
			{Point: geo.Point{Lon: 25.82, Lat: 40.85}, Kind: match.StopPickup, PartyID: "a"},
			{Point: geo.Point{Lon: 25.85, Lat: 40.85}, Kind: match.StopDropoff, PartyID: "a"},
		},
	}

	got, err := match.BestInsertion(context.Background(), router, itin,
		geo.Point{Lon: 25.95, Lat: 40.85}, geo.Point{Lon: 25.98, Lat: 40.85})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	// Everything is collinear and in travel order, so the optimum adds nothing.
	if got.DetourMin > 0.01 {
		t.Fatalf("detour = %v min; the cheapest ordering along a straight line costs nothing", got.DetourMin)
	}
}

func TestBestInsertionBoundsTheNumberOfRoutingCalls(t *testing.T) {
	router := &straightLineRouter{}
	itin := match.Itinerary{
		Origin:      routeStart,
		Destination: routeEnd,
		Stops: []match.Stop{
			{Point: geo.Point{Lon: 25.84, Lat: 40.85}, Kind: match.StopPickup, PartyID: "a"},
			{Point: geo.Point{Lon: 25.96, Lat: 40.85}, Kind: match.StopDropoff, PartyID: "a"},
		},
	}

	if _, err := match.BestInsertion(context.Background(), router, itin,
		geo.Point{Lon: 25.88, Lat: 40.85}, geo.Point{Lon: 25.92, Lat: 40.85}); err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	// OSRM calls are the expensive part of matching. With one party aboard the
	// brute-force search is small; this pins that it stays small.
	if router.calls > 20 {
		t.Fatalf("made %d routing calls; the insertion search should stay far smaller", router.calls)
	}
}

// --- regression tests for issues found in adversarial review ---

// One unroutable ordering must not abandon the search: a different ordering may
// route perfectly well, and giving up would silently drop a valid driver.
func TestBestInsertionSkipsAnUnroutableOrderingAndUsesAnother(t *testing.T) {
	existing := geo.Point{Lon: 25.90, Lat: 40.85}
	pickup := geo.Point{Lon: 25.95, Lat: 40.85}
	dropoff := geo.Point{Lon: 25.98, Lat: 40.85}

	// Refuse exactly the orderings that put the new party ahead of the existing
	// stop; every other ordering routes normally.
	router := &pickyRouter{refuseBefore: existing, newStops: []geo.Point{pickup, dropoff}}

	itin := match.Itinerary{
		Origin:      routeStart,
		Destination: routeEnd,
		Stops: []match.Stop{
			{Point: existing, Kind: match.StopPickup, PartyID: "a"},
			{Point: geo.Point{Lon: 25.99, Lat: 40.85}, Kind: match.StopDropoff, PartyID: "a"},
		},
	}

	got, err := match.BestInsertion(context.Background(), router, itin, pickup, dropoff)
	if err != nil {
		t.Fatalf("BestInsertion gave up instead of trying another ordering: %v", err)
	}
	if len(got.Waypoints) == 0 {
		t.Fatal("no itinerary returned")
	}
}

// A routing outage is not the same as an unroutable ordering, and must not be
// mistaken for "this driver cannot take them".
func TestBestInsertionStillFailsWhenTheRouterIsBroken(t *testing.T) {
	router := &straightLineRouter{err: errors.New("connection refused")}

	_, err := match.BestInsertion(context.Background(), router, emptyItinerary(),
		geo.Point{Lon: 25.85, Lat: 40.85}, geo.Point{Lon: 25.95, Lat: 40.85})

	if err == nil {
		t.Fatal("a routing outage must surface, not be treated as no-route")
	}
	if errors.Is(err, geo.ErrNoRoute) {
		t.Fatal("an infrastructure failure was reported as an unroutable candidate")
	}
}

// A malformed itinerary must not turn into an unbounded pile of OSRM calls.
func TestBestInsertionRefusesAnAbsurdlyLongItinerary(t *testing.T) {
	router := &straightLineRouter{}
	itin := match.Itinerary{Origin: routeStart, Destination: routeEnd}
	for i := range 500 {
		itin.Stops = append(itin.Stops, match.Stop{
			Point:   geo.Point{Lon: 25.85, Lat: 40.85},
			Kind:    match.StopPickup,
			PartyID: "a", // one party, so the cap does not catch it
		})
		_ = i
	}

	_, err := match.BestInsertion(context.Background(), router, itin,
		geo.Point{Lon: 25.9, Lat: 40.85}, geo.Point{Lon: 25.95, Lat: 40.85})

	if err == nil {
		t.Fatal("expected a refusal rather than a quadratic fan-out of routing calls")
	}
	if router.calls > 50 {
		t.Fatalf("made %d routing calls before refusing", router.calls)
	}
}

// pickyRouter returns NoRoute for any ordering where a new stop precedes the
// given point, and routes everything else.
type pickyRouter struct {
	refuseBefore geo.Point
	newStops     []geo.Point
	straightLineRouter
}

func (r *pickyRouter) Route(ctx context.Context, waypoints []geo.Point) (geo.Route, error) {
	idxOf := func(p geo.Point) int {
		for i, w := range waypoints {
			if w == p {
				return i
			}
		}
		return -1
	}
	anchor := idxOf(r.refuseBefore)
	if anchor >= 0 {
		for _, s := range r.newStops {
			if i := idxOf(s); i >= 0 && i < anchor {
				return geo.Route{}, fmt.Errorf("%w: no path", geo.ErrNoRoute)
			}
		}
	}
	return r.straightLineRouter.Route(ctx, waypoints)
}

// A drive's off-road limit leaves out the time stops take: "my road only"
// still takes somebody standing on the road, two stops' dwell and all.
func TestAnOffRoadLimitDoesNotCountPullingOver(t *testing.T) {
	zero := 0
	b := match.Budget{AllowStops: true, Km: 5, Min: 1, OffRoadMin: &zero}
	onRoad := match.Insertion{TotalDetourMin: 4, NewStops: 2}
	if !onRoad.WithinBudget(b) {
		t.Errorf("a pickup on the road was refused under 'my road only'")
	}
	offRoad := match.Insertion{TotalDetourMin: 6, NewStops: 2}
	if offRoad.WithinBudget(b) {
		t.Errorf("two minutes off the road were allowed under 'my road only'")
	}
}
