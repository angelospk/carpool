package asks_test

import (
	"context"
	"encoding/json"
	"testing"

	"carpool/internal/domain"
)

// What a passenger is asked to contribute towards is the road, not the map.
//
// The formula's trip_km is defined in MATCHING.md as the distance along the
// driver's route between where this passenger gets in and where they get out.
// It was the straight line between their own two points instead — which is
// never the distance anybody drives, and on the roads this app is for is not
// close to it either. Έβρος is a river with few bridges and a delta with fewer:
// two villages four hundred metres apart across the water are eleven kilometres
// apart by car, and the driver burns the eleven.
//
// The number was already there. The gross filter computes the shared distance
// along the route to decide whether an overlap is worth an offer at all, so
// every candidate arrives carrying it; pricing was the one place that went and
// measured something else.

// A road that goes the long way round is what the driver actually drives.
func TestTheContributionIsForTheRoadDrivenNotTheLineBetweenTheStops(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)

	// East along the coast, then north inland: two legs of about 16.8 and 22.2
	// km. Somebody joining near the start and leaving near the end is carried
	// around the corner, which is a third further than the line across it.
	const road = `LINESTRING(25.80 40.85, 26.00 40.85, 26.00 41.05)`
	mustExec(t, f, `
		UPDATE route_templates SET route_geom = ST_GeomFromText($1, 4326), route_km = 39.1,
		       destination = ST_SetSRID(ST_MakePoint(26.00, 41.05), 4326)`, road)
	mustExec(t, f, `
		UPDATE trip_occurrences SET route_geom = ST_GeomFromText($1, 4326), route_km = 39.1`, road)
	mustExec(t, f, `
		UPDATE ride_requests SET status = 'open',
		       origin = ST_SetSRID(ST_MakePoint(25.82, 40.85), 4326),
		       destination = ST_SetSRID(ST_MakePoint(26.00, 41.03), 4326)`)

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}

	basis := f.basis(t, askID)

	// Along the road, about 35 km. Across the corner, about 25. The tolerance
	// is a kilometre either way, which is projection noise; the ten-kilometre
	// gap between the two answers is the point.
	const alongTheRoad, acrossTheCorner = 35.2, 25.1
	if basis.TripKm < alongTheRoad-1 || basis.TripKm > alongTheRoad+1 {
		t.Errorf("the contribution is for %.1f km; the road they are carried along is %.1f km",
			basis.TripKm, alongTheRoad)
	}
	if basis.TripKm < acrossTheCorner+2 {
		t.Errorf("%.1f km is the line between their stops, not the road between them", basis.TripKm)
	}
	if basis.TripKmSource != domain.TripKmDriverRoute {
		t.Errorf("the receipt says the distance came from %q", basis.TripKmSource)
	}
}

// The threshold under which a driver asks for nothing is a distance driven.
//
// This is where the old measure did real harm rather than merely quoting low. A
// driver's free_below_km is five kilometres, and two stops on opposite banks
// are four hundred metres apart on the map. Every such ride was waived
// outright: eleven and a half kilometres of somebody's petrol, given away
// because a straight line does not know about the river.
func TestAShortLineDownALongRoadIsNotAFreeRide(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)

	// Out along one bank, over the bridge, and back down the other: about 12.4
	// km of road between two points 445 metres apart.
	const road = `LINESTRING(25.80 40.85, 25.871 40.85, 25.871 40.854, 25.80 40.854)`
	mustExec(t, f, `
		UPDATE route_templates SET route_geom = ST_GeomFromText($1, 4326), route_km = 12.4,
		       destination = ST_SetSRID(ST_MakePoint(25.80, 40.854), 4326)`, road)
	// One seat, so the fuel is split with the driver alone and the figure is
	// large enough to survive the fifty-cent floor.
	mustExec(t, f, `
		UPDATE trip_occurrences SET route_geom = ST_GeomFromText($1, 4326), route_km = 12.4,
		       seats_total = 1, seats_free = 1`, road)
	mustExec(t, f, `
		UPDATE ride_requests SET status = 'open',
		       origin = ST_SetSRID(ST_MakePoint(25.805, 40.85), 4326),
		       destination = ST_SetSRID(ST_MakePoint(25.805, 40.854), 4326)`)

	var freeBelow float64
	must(t, f.pool.QueryRow(ctx,
		`SELECT free_below_km FROM driver_profiles WHERE user_id = $1`, f.driverID).Scan(&freeBelow),
		"the driver's threshold")

	askID, err := f.matcher().AskNext(ctx, f.requestID)
	if err != nil {
		t.Fatalf("AskNext: %v", err)
	}
	basis := f.basis(t, askID)

	if basis.TripKm <= freeBelow {
		t.Fatalf("the ride is priced as %.2f km, under the %.1f km the driver gives away",
			basis.TripKm, freeBelow)
	}
	var price float64
	must(t, f.pool.QueryRow(ctx,
		`SELECT suggested_price_eur FROM asks WHERE id = $1`, askID).Scan(&price), "the price")
	if price <= 0 {
		t.Fatalf("%.1f km of driving is offered free because the two stops are near each other",
			basis.TripKm)
	}
}

func (f *fixture) basis(t *testing.T, askID any) domain.Basis {
	t.Helper()
	var raw []byte
	must(t, f.pool.QueryRow(context.Background(),
		`SELECT pricing_basis FROM asks WHERE id = $1`, askID).Scan(&raw), "the receipt")

	var b domain.Basis
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("the receipt is not a receipt: %v", err)
	}
	return b
}
