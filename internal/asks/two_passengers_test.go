package asks_test

import (
	"context"
	"errors"
	"testing"

	"carpool/internal/geo"
	"carpool/internal/match"
	"carpool/internal/store"
)

// Two passengers on one departure, whose rides overlap on the road, both get
// their seat, and the driver's list of stops follows the road rather than the
// order the offers were accepted in. A third passenger is refused at the party
// cap and the seats stay as they were. Each piece is tested alone elsewhere;
// this is the whole path together.
func TestTwoPassengersShareOneDepartureInRoadOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mustExec(t, f, `DELETE FROM asks`)
	mustExec(t, f, `UPDATE trip_occurrences SET seats_total = 4, seats_free = 4`)
	svc := f.service()

	// The road runs east. Their rides overlap: a boards, b boards, a gets off,
	// b gets off. b is accepted first, so accept order is not road order.
	a, _, askA := f.placedAsk("+306900000051", 1, geo.Point{Lon: 25.84, Lat: 40.85}, geo.Point{Lon: 25.92, Lat: 40.85})
	b, _, askB := f.placedAsk("+306900000052", 1, geo.Point{Lon: 25.88, Lat: 40.85}, geo.Point{Lon: 25.96, Lat: 40.85})
	if _, err := svc.Accept(ctx, askB, f.driverID); err != nil {
		t.Fatalf("accept b: %v", err)
	}
	if _, err := svc.Accept(ctx, askA, f.driverID); err != nil {
		t.Fatalf("accept a: %v", err)
	}

	if got := f.seatsFree(); got != 2 {
		t.Fatalf("seats_free = %d, want 2", got)
	}
	var confirmed int
	must(t, f.pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE occurrence_id = $1 AND status = 'confirmed'`,
		f.occurrenceID).Scan(&confirmed), "bookings")
	if confirmed != 2 {
		t.Fatalf("%d confirmed bookings, want 2", confirmed)
	}

	trip, err := store.GetTrip(ctx, f.pool, f.occurrenceID, f.driverID, f.clock.Now())
	if err != nil {
		t.Fatalf("GetTrip: %v", err)
	}
	type stop struct {
		kind string
		who  string
	}
	want := []stop{{"pickup", a.String()}, {"pickup", b.String()}, {"dropoff", a.String()}, {"dropoff", b.String()}}
	if len(trip.Stops) != len(want) {
		t.Fatalf("%d stops, want %d: %+v", len(trip.Stops), len(want), trip.Stops)
	}
	lastLon := 0.0
	for i, s := range trip.Stops {
		if s.PassengerID == nil || s.Point == nil {
			t.Fatalf("stop %d has no passenger or point: %+v", i, s)
		}
		if got := (stop{s.Kind, s.PassengerID.String()}); got != want[i] {
			t.Errorf("stop %d = %v, want %v", i, got, want[i])
		}
		if s.Point[0] < lastLon {
			t.Errorf("stop %d at lon %v is behind the stop before it (%v)", i, s.Point[0], lastLon)
		}
		lastLon = s.Point[0]
	}

	// The party cap: the car holds two passengers' stops at most, whatever the
	// seats say. The refused accept must not take a seat.
	_, _, askC := f.placedAsk("+306900000053", 1, onRoadFrom, onRoadTo)
	if _, err := svc.Accept(ctx, askC, f.driverID); !errors.Is(err, match.ErrPartyCapReached) {
		t.Fatalf("third accept = %v, want ErrPartyCapReached", err)
	}
	if got := f.seatsFree(); got != 2 {
		t.Fatalf("seats_free = %d after a refused accept, want 2", got)
	}
}
