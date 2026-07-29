package match_test

import (
	"context"
	"errors"
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
