package geo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// A response with legs and steps, so a ferry can be placed anywhere in it.
type stubStep struct {
	Mode     string  `json:"mode"`
	Distance float64 `json:"distance"`
	Maneuver struct {
		Location []float64 `json:"location"`
	} `json:"maneuver"`
}

func step(mode string, lon, lat float64) stubStep {
	s := stubStep{Mode: mode}
	s.Maneuver.Location = []float64{lon, lat}
	return s
}

// osrmWithLegs answers the original request with a route made of the given
// legs, and any re-route — recognised by asking for steps or not, which is the
// one thing that actually distinguishes the two calls — as plain driving.
//
// A re-route whose first and last coordinates are the same is a journey that
// starts at the water, and answers zero.
func osrmWithLegs(t *testing.T, legs ...[]stubStep) (*OSRM, *[]string) {
	t.Helper()
	var seen []string

	type leg struct {
		Steps []stubStep `json:"steps"`
	}

	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		body := map[string]any{"code": "Ok"}

		if r.URL.Query().Get("steps") == "true" {
			out := make([]leg, 0, len(legs))
			for _, l := range legs {
				out = append(out, leg{Steps: l})
			}
			body["routes"] = []any{map[string]any{
				"distance": 184300.0, "duration": 74700.0, "geometry": "with-ferry", "legs": out,
			}}
		} else {
			coords := strings.Split(strings.TrimPrefix(r.URL.Path, "/route/v1/driving/"), ";")
			distance := 40000.0
			if coords[0] == coords[len(coords)-1] {
				distance = 0
			}
			body["routes"] = []any{map[string]any{
				"distance": distance, "duration": 2400.0, "geometry": "driving-only",
			}}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	return c, &seen
}

// The ordinary case: no ferry, nothing changes, and no second call is made.
func TestARouteOverLandIsReturnedWhole(t *testing.T) {
	c, seen := osrmWithLegs(t, []stubStep{step("driving", 25.9, 40.8), step("driving", 24.0, 40.7)})

	got, err := c.Route(context.Background(), []Point{{Lon: 25.9, Lat: 40.8}, {Lon: 22.9, Lat: 40.6}})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if got.SeaCrossing {
		t.Error("a route with no ferry was reported as a sea crossing")
	}
	if got.DistanceMeters != 184300 {
		t.Errorf("DistanceMeters = %v, want the whole route", got.DistanceMeters)
	}
	if len(*seen) != 1 {
		t.Errorf("made %d requests, want 1: nothing should be re-routed", len(*seen))
	}
}

// The decided behaviour: the trip stops at the port. Everything past the ferry
// is somebody else's journey, and charging fuel for it would be charging a
// driver for sailing.
func TestARouteOntoAFerryStopsAtThePort(t *testing.T) {
	c, _ := osrmWithLegs(t, []stubStep{
		step("driving", 27.28, 36.89),
		step("ferry", 27.30, 36.90), // the port: where the driving ends
		step("driving", 28.22, 36.43),
	})

	got, err := c.Route(context.Background(), []Point{{Lon: 27.28, Lat: 36.89}, {Lon: 28.22, Lat: 36.43}})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if !got.SeaCrossing {
		t.Fatal("a route crossing a ferry was not flagged")
	}
	if got.Port != (Point{Lon: 27.30, Lat: 36.90}) {
		t.Errorf("Port = %+v, want the coordinate the ferry leg starts from", got.Port)
	}
	// The distance is the re-routed driving portion, not the 184 km that
	// included 156 km of sea.
	if got.DistanceMeters != 40000 {
		t.Errorf("DistanceMeters = %v, want only the driving as far as the port", got.DistanceMeters)
	}
	if got.Geometry != "driving-only" {
		t.Errorf("Geometry = %q, want the truncated route rather than the one through the sea", got.Geometry)
	}
}

// A ferry in a later leg keeps the waypoints before it. A driver picking
// somebody up on the way to the port is still doing that part of the trip.
func TestWaypointsBeforeTheFerryAreKept(t *testing.T) {
	c, seen := osrmWithLegs(t,
		[]stubStep{step("driving", 23.7, 37.9)},
		[]stubStep{step("driving", 24.0, 38.0), step("ferry", 24.05, 38.02)},
	)

	_, err := c.Route(context.Background(), []Point{
		{Lon: 23.7, Lat: 37.9}, {Lon: 24.0, Lat: 38.0}, {Lon: 25.3, Lat: 37.4},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	last := (*seen)[len(*seen)-1]
	if !strings.Contains(last, "23.7,37.9;24,38;24.05,38.02") {
		t.Errorf("re-routed through %q, want origin, the stop before the ferry, then the port", last)
	}
}

// Nothing to drive. A crossing that begins at the water is a ferry timetable,
// not a car journey, and saying so is better than quoting a trip of zero
// kilometres.
func TestACrossingWithNoDrivingAtAllIsRefused(t *testing.T) {
	c, _ := osrmWithLegs(t, []stubStep{step("ferry", 25.15, 35.34)})

	_, err := c.Route(context.Background(), []Point{{Lon: 25.15, Lat: 35.34}, {Lon: 23.7, Lat: 37.9}})
	if !errors.Is(err, ErrSeaCrossingOnly) {
		t.Fatalf("Route: got %v, want ErrSeaCrossingOnly", err)
	}
}

// The shaped route is the one a driver's posted trip is built from, so a ferry
// has to stop it at the port too. Truncating only the matcher's route would
// leave a stored trip whose geometry crosses the sea and whose price charges
// petrol for it.
func TestTheStoredRouteAlsoStopsAtThePort(t *testing.T) {
	var seen []string
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String())
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Query().Get("steps") == "true" {
			_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":221400,"duration":43440,
				"geometry":{"coordinates":[[23.7,37.9],[25.3,37.4]]},
				"legs":[{"steps":[
					{"mode":"driving","maneuver":{"location":[23.7,37.9]}},
					{"mode":"ferry","maneuver":{"location":[24.0,38.0]}}]}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":12300,"duration":960,
			"geometry":{"coordinates":[[23.7,37.9],[23.9,37.95],[24.0,38.0]]}}]}`))
	})

	got, err := c.RouteWithShape(context.Background(), []Point{{Lon: 23.7, Lat: 37.9}, {Lon: 25.3, Lat: 37.4}})
	if err != nil {
		t.Fatalf("RouteWithShape: %v", err)
	}

	if !got.SeaCrossing {
		t.Fatal("a posted trip crossing a ferry was not flagged")
	}
	if got.DistanceMeters != 12300 {
		t.Errorf("DistanceMeters = %v, want the driving as far as the port", got.DistanceMeters)
	}
	last := got.Shape[len(got.Shape)-1]
	if last != (Point{Lon: 24.0, Lat: 38.0}) {
		t.Errorf("the stored shape ends at %+v, want the port", last)
	}
}

// Steps have to be asked for, or every ferry in the country is invisible.
func TestTheRouteRequestAsksForSteps(t *testing.T) {
	var query string
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":1,"duration":1,"geometry":"g"}]}`))
	})

	if _, err := c.Route(context.Background(), []Point{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if !strings.Contains(query, "steps=true") {
		t.Errorf("query = %q, want steps=true", query)
	}
}
