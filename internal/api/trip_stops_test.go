package api_test

import (
	"net/http"
	"testing"
)

// secondPassenger books another passenger onto the fixture trip, getting in
// at lon 25.90 and out at 25.95: after the first passenger's pickup (25.82)
// and before their drop-off (25.98).
func (h *harness) secondPassenger(t *testing.T) string {
	t.Helper()
	token, _ := h.signUp("+306900000033", "Δεύτερη")
	h.mustExec(`
		WITH p AS (SELECT id FROM users WHERE phone = '+306900000033'),
		     o AS (SELECT id FROM trip_occurrences LIMIT 1),
		     r AS (INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end,
		                                      kind, status, seats, origin_label, destination_label, luggage, luggage_count)
		           SELECT p.id, ST_SetSRID(ST_MakePoint(25.90,40.85),4326), ST_SetSRID(ST_MakePoint(25.95,40.85),4326),
		                  '2026-07-30T04:00:00Z','2026-07-30T06:00:00Z','open','matched', 2,
		                  'Μέστη πλατεία', 'Κομοτηνή σταθμός', 'suitcase', 1 FROM p
		           RETURNING id, passenger_id),
		     a AS (INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
		                             suggested_price_eur, itinerary_version, status, expires_at)
		           SELECT r.id, o.id, 1, 1, ST_SetSRID(ST_MakePoint(25.90,40.85),4326),
		                  ST_SetSRID(ST_MakePoint(25.95,40.85),4326), 1.80, 0, 'accepted', now() + interval '1 hour'
		             FROM r, o RETURNING id)
		INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur, pickup_point, dropoff_point, luggage_count)
		SELECT o.id, r.id, r.passenger_id, a.id, 2, 1.80, ST_SetSRID(ST_MakePoint(25.90,40.85),4326),
		       ST_SetSRID(ST_MakePoint(25.95,40.85),4326), 1 FROM o, r, a`)
	return token
}

type stop struct {
	Kind         string     `json:"kind"`
	Mine         bool       `json:"mine"`
	Name         *string    `json:"name"`
	Label        *string    `json:"label"`
	Point        *[2]float64 `json:"point"`
	At           *string    `json:"at"`
	Seats        *int       `json:"seats"`
	LuggageCount *int       `json:"luggage_count"`
}

type tripWithStops struct {
	Stops      []stop `json:"stops"`
	Passengers []struct {
		Name     string   `json:"name"`
		PriceEUR *float64 `json:"price_eur"`
		Seats    *int     `json:"seats"`
	} `json:"passengers"`
}

// QA 2026-10-17 U F3: nobody could see where anyone gets in, in what order,
// or how many seats and bags each passenger has.
func TestTheDriverSeesEveryStopInRoadOrder(t *testing.T) {
	h := newHarness(t)
	driver, _, occ := h.bookedTrip(t)
	h.secondPassenger(t)

	var trip tripWithStops
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, driver), &trip)
	if len(trip.Stops) != 4 {
		t.Fatalf("%d stops, want 4: %+v", len(trip.Stops), trip.Stops)
	}
	want := []struct{ kind, name string }{
		{"pickup", "Επιβάτης"}, {"pickup", "Δεύτερη"}, {"dropoff", "Δεύτερη"}, {"dropoff", "Επιβάτης"},
	}
	for i, w := range want {
		s := trip.Stops[i]
		if s.Kind != w.kind || s.Name == nil || *s.Name != w.name {
			t.Errorf("stop %d = %s %v, want %s %s", i, s.Kind, s.Name, w.kind, w.name)
		}
		if s.Point == nil || s.At == nil {
			t.Errorf("stop %d has no point or time", i)
		}
	}
	if s := trip.Stops[1]; s.Seats == nil || *s.Seats != 2 || s.LuggageCount == nil || *s.LuggageCount != 1 ||
		s.Label == nil || *s.Label != "Μέστη πλατεία" {
		t.Errorf("second pickup: seats %v luggage %v label %v, want 2, 1, Μέστη πλατεία", s.Seats, s.LuggageCount, s.Label)
	}
}

// A passenger sees their own stops in full, and only that the others exist.
// And not what the others pay or how many seats they took (QA 2026-10-17 A 6).
func TestAPassengerSeesTheirOwnStopsAndNotTheOthersPrice(t *testing.T) {
	h := newHarness(t)
	_, passenger, occ := h.bookedTrip(t)
	h.secondPassenger(t)

	var trip tripWithStops
	h.decode(h.do(http.MethodGet, "/v1/trips/"+occ.String(), nil, passenger), &trip)
	if len(trip.Stops) != 4 {
		t.Fatalf("%d stops, want 4", len(trip.Stops))
	}
	for i, s := range trip.Stops {
		if s.Mine {
			if s.Point == nil || s.At == nil {
				t.Errorf("own stop %d without point or time", i)
			}
			continue
		}
		if s.Point != nil || s.Name != nil || s.Label != nil || s.Seats != nil || s.At != nil {
			t.Errorf("another passenger's stop %d leaks %+v", i, s)
		}
	}
	if !trip.Stops[0].Mine || !trip.Stops[3].Mine {
		t.Errorf("own stops should be first and last")
	}
	for _, p := range trip.Passengers {
		if p.Name == "Δεύτερη" && (p.PriceEUR != nil || p.Seats != nil) {
			t.Errorf("a co-passenger's price %v and seats %v are visible", p.PriceEUR, p.Seats)
		}
	}
}
