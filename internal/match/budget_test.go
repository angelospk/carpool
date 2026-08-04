package match_test

import (
	"context"
	"testing"
	"time"

	"carpool/internal/geo"
	"carpool/internal/match"
)

// A router whose answers are a function of how many waypoints it is given, so a
// test can say "each extra stop costs five kilometres" without inventing a map.
type stepRouter struct {
	baseKm  float64
	baseMin float64
	perStop float64 // extra km for each waypoint beyond origin and destination
	calls   int
}

func (r *stepRouter) Route(_ context.Context, waypoints []geo.Point) (geo.Route, error) {
	r.calls++
	extra := float64(len(waypoints) - 2)
	km := r.baseKm + extra*r.perStop
	return geo.Route{
		DistanceMeters: km * 1000,
		// A kilometre a minute, so the two limits can be reasoned about
		// together without a second knob.
		Duration: time.Duration(km*r.baseMin/r.baseKm) * time.Minute,
	}, nil
}

func plainItinerary() match.Itinerary {
	return match.Itinerary{
		Origin:      geo.Point{Lon: 25.87, Lat: 40.84},
		Destination: geo.Point{Lon: 22.94, Lat: 40.64},
	}
}

// The detour a budget is judged against is the one measured from the driver
// travelling alone, not from whatever the itinerary happens to look like now.
//
// This is the bug the change exists for: judged marginally, three passengers
// each costing eight minutes all pass an eight-minute budget, and the driver
// arrives twenty-four minutes late having agreed to eight.
func TestTheDetourIsMeasuredFromTheDriverTravellingAlone(t *testing.T) {
	r := &stepRouter{baseKm: 300, baseMin: 300, perStop: 5}

	// One party already aboard: the itinerary is already two stops longer than
	// the driver's own plan, and ten kilometres longer with it.
	itin := plainItinerary()
	itin.Stops = []match.Stop{
		{Point: geo.Point{Lon: 25.0, Lat: 40.8}, Kind: match.StopPickup, PartyID: "a"},
		{Point: geo.Point{Lon: 24.0, Lat: 40.7}, Kind: match.StopDropoff, PartyID: "a"},
	}

	got, err := match.BestInsertion(context.Background(), r, itin,
		geo.Point{Lon: 24.5, Lat: 40.75}, geo.Point{Lon: 23.5, Lat: 40.7})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	// Four stops against the driver's none: twenty kilometres.
	if got.TotalDetourKm != 20 {
		t.Errorf("TotalDetourKm = %v, want 20 measured from the driver alone", got.TotalDetourKm)
	}
	// The marginal figure is still reported, because it is what the driver is
	// being asked to accept right now.
	if got.DetourKm != 10 {
		t.Errorf("DetourKm = %v, want the 10 this passenger adds", got.DetourKm)
	}
}

func TestABudgetInKilometresIsHonoured(t *testing.T) {
	r := &stepRouter{baseKm: 100, baseMin: 100, perStop: 8}
	got, err := match.BestInsertion(context.Background(), r, plainItinerary(),
		geo.Point{Lon: 25.0, Lat: 40.8}, geo.Point{Lon: 24.0, Lat: 40.7})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}
	if got.TotalDetourKm != 16 {
		t.Fatalf("the fixture is wrong: TotalDetourKm = %v, want 16", got.TotalDetourKm)
	}

	generous := match.Budget{AllowStops: true, Km: 16, Min: 60}
	if !got.WithinBudget(generous) {
		t.Error("a detour exactly at the limit was refused; the limit is what was agreed to")
	}

	tight := match.Budget{AllowStops: true, Km: 15.999, Min: 60}
	if got.WithinBudget(tight) {
		t.Error("a detour past the stated kilometres was accepted")
	}
}

// The reason both limits exist. Fifteen kilometres on the Egnatia is nine
// minutes; fifteen kilometres up a mountain road is thirty-five. A driver who
// says fifteen kilometres has not agreed to thirty-five minutes.
func TestATimeCapCatchesASlowDetourThatFitsTheKilometres(t *testing.T) {
	// Half a kilometre a minute: mountain roads.
	r := &stepRouter{baseKm: 100, baseMin: 200, perStop: 8}
	got, err := match.BestInsertion(context.Background(), r, plainItinerary(),
		geo.Point{Lon: 25.0, Lat: 40.8}, geo.Point{Lon: 24.0, Lat: 40.7})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	within := match.Budget{AllowStops: true, Km: 20, Min: 60}
	if !got.WithinBudget(within) {
		t.Fatalf("the fixture is wrong: %+v refused by %+v", got, within)
	}

	capped := match.Budget{AllowStops: true, Km: 20, Min: 20}
	if got.WithinBudget(capped) {
		t.Errorf("a detour of %.0f minutes passed a twenty-minute cap", got.TotalDetourMin)
	}
}

// Saying no to intermediate stops has to mean no, not "none that cost
// anything". Stopping costs time even where the map says the detour is zero,
// which is what the dwell allowance is for.
func TestADriverWhoWantsNoStopsGetsNone(t *testing.T) {
	// perStop 0: the pickup is exactly on the route and adds no distance.
	r := &stepRouter{baseKm: 100, baseMin: 100, perStop: 0}
	got, err := match.BestInsertion(context.Background(), r, plainItinerary(),
		geo.Point{Lon: 25.0, Lat: 40.8}, geo.Point{Lon: 24.0, Lat: 40.7})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	if got.NewStops != 2 {
		t.Fatalf("NewStops = %d, want the pickup and the dropoff", got.NewStops)
	}
	if got.WithinBudget(match.Budget{AllowStops: false, Km: 100, Min: 100}) {
		t.Error("a driver who refused intermediate stops was offered one anyway")
	}
	if !got.WithinBudget(match.Budget{AllowStops: true, Km: 100, Min: 100}) {
		t.Error("a driver who accepts stops was refused one that costs nothing")
	}
}

// Pulling over, waiting and pulling out again is time, and a route that is
// geometrically unchanged is not free.
func TestEveryStopCostsTimeEvenWhenTheRouteDoesNotChange(t *testing.T) {
	r := &stepRouter{baseKm: 100, baseMin: 100, perStop: 0}
	got, err := match.BestInsertion(context.Background(), r, plainItinerary(),
		geo.Point{Lon: 25.0, Lat: 40.8}, geo.Point{Lon: 24.0, Lat: 40.7})
	if err != nil {
		t.Fatalf("BestInsertion: %v", err)
	}

	want := 2 * match.StopDwell.Minutes()
	if got.TotalDetourMin != want {
		t.Errorf("TotalDetourMin = %v for two stops on the route, want %v of dwell", got.TotalDetourMin, want)
	}
	if got.WithinBudget(match.Budget{AllowStops: true, Km: 100, Min: 3}) {
		t.Error("two stops fitted a three-minute budget; the dwell is not being counted")
	}
}
