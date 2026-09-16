package geo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func osrmStub(t *testing.T, handler http.HandlerFunc) *OSRM {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewOSRM(srv.URL, srv.Client())
}

func TestRouteParsesDistanceDurationAndGeometry(t *testing.T) {
	var gotPath string
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":12345.6,"duration":900.5,"geometry":"abcd"}]}`))
	})

	got, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	if got.DistanceMeters != 12345.6 {
		t.Errorf("DistanceMeters = %v, want 12345.6", got.DistanceMeters)
	}
	if got.Duration != 900500*time.Millisecond {
		t.Errorf("Duration = %v, want 900.5s", got.Duration)
	}
	if got.Geometry != "abcd" {
		t.Errorf("Geometry = %q, want abcd", got.Geometry)
	}
	if !strings.Contains(gotPath, "/route/v1/driving/") {
		t.Errorf("request path = %q, want the driving route service", gotPath)
	}
	if !strings.Contains(gotPath, "26.5,41.1;26.6,41.2") {
		t.Errorf("request path = %q, want lon,lat pairs in waypoint order", gotPath)
	}
}

func TestRouteRejectsFewerThanTwoWaypoints(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the server should not be called for an invalid waypoint list")
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}})

	if err == nil {
		t.Fatal("expected an error for a single waypoint, got nil")
	}
}

func TestRouteReportsNoRouteDistinctly(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"NoRoute","message":"Impossible route between points"}`))
	})

	_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})

	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("error = %v, want it to wrap ErrNoRoute so the matcher can drop the candidate", err)
	}
}

func TestRouteRejectsAResponseWithNoRoutes(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[]}`))
	})

	_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})

	if err == nil {
		t.Fatal("expected an error when a supposedly-Ok response carries no routes")
	}
}

func TestRouteRejectsMalformedJSON(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":`))
	})

	_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})

	if err == nil {
		t.Fatal("expected an error for a truncated response body")
	}
}

func TestRouteReportsAnHTTPErrorStatus(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})

	if err == nil {
		t.Fatal("expected an error for a 502 from the routing sidecar")
	}
}

func TestRouteHonoursContextCancellation(t *testing.T) {
	release := make(chan struct{})
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":1,"duration":1,"geometry":"x"}]}`))
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Route(ctx, []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to wrap context.Canceled", err)
	}
}

func TestNearestReturnsTheSnappedPointAndDistance(t *testing.T) {
	var gotPath string
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","waypoints":[{"location":[26.55,41.15],"distance":42.5}]}`))
	})

	got, err := c.Nearest(context.Background(), Point{Lon: 26.5, Lat: 41.1})
	if err != nil {
		t.Fatalf("Nearest: %v", err)
	}

	if got.Point.Lon != 26.55 || got.Point.Lat != 41.15 {
		t.Errorf("snapped point = %+v, want {26.55 41.15}", got.Point)
	}
	if got.DistanceMeters != 42.5 {
		t.Errorf("DistanceMeters = %v, want 42.5", got.DistanceMeters)
	}
	if !strings.Contains(gotPath, "/nearest/v1/driving/") {
		t.Errorf("request path = %q, want the nearest service", gotPath)
	}
}

func TestNearestRejectsAResponseWithNoWaypoints(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","waypoints":[]}`))
	})

	_, err := c.Nearest(context.Background(), Point{Lon: 26.5, Lat: 41.1})

	if err == nil {
		t.Fatal("expected an error when no waypoint is returned")
	}
}

func TestNearestReportsNoSegmentDistinctly(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"NoSegment","message":"Could not find a matching segment"}`))
	})

	_, err := c.Nearest(context.Background(), Point{Lon: 26.5, Lat: 41.1})

	if !errors.Is(err, ErrNoSegment) {
		t.Fatalf("error = %v, want it to wrap ErrNoSegment", err)
	}
}

func TestRouteReturnsGeoJSONCoordinatesForStorage(t *testing.T) {
	var gotQuery string
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":1000,"duration":60,
			"geometry":{"type":"LineString","coordinates":[[25.8,40.85],[26.0,40.85]]}}]}`))
	})

	got, err := c.RouteWithShape(context.Background(),
		[]Point{{Lon: 25.8, Lat: 40.85}, {Lon: 26.0, Lat: 40.85}})
	if err != nil {
		t.Fatalf("RouteWithShape: %v", err)
	}

	if !strings.Contains(gotQuery, "geometries=geojson") {
		t.Errorf("query = %q, want it to request geojson geometry", gotQuery)
	}
	if len(got.Shape) != 2 {
		t.Fatalf("got %d coordinates, want 2", len(got.Shape))
	}
	if got.Shape[0] != (Point{Lon: 25.8, Lat: 40.85}) {
		t.Errorf("first coordinate = %+v, want {25.8 40.85}", got.Shape[0])
	}
	if got.DistanceMeters != 1000 {
		t.Errorf("DistanceMeters = %v, want 1000", got.DistanceMeters)
	}
}

func TestRouteWithShapeRejectsADegenerateShape(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":0,"duration":0,
			"geometry":{"type":"LineString","coordinates":[[25.8,40.85]]}}]}`))
	})

	_, err := c.RouteWithShape(context.Background(),
		[]Point{{Lon: 25.8, Lat: 40.85}, {Lon: 26.0, Lat: 40.85}})

	// A single-point line cannot be stored as a LineString, and the matching SQL
	// depends on having a real line to locate points along.
	if err == nil {
		t.Fatal("expected an error for a route whose geometry has fewer than 2 points")
	}
}

// A clipped routing extract does not refuse a coordinate it does not cover. It
// snaps it to the nearest road it does have and answers Ok.
//
// Measured against the extract production serves — Evros plus a ~30 km buffer —
// Θεσσαλονίκη snapped 173 km away, Αθήνα 313 km and Πάτρα 408 km, and a
// Θεσσαλονίκη → Αθήνα route came back as a confident 186.6 km line through
// Thrace. Nothing downstream could tell that apart from a real answer: it is
// the geometry a published route is stored with and the distance a contribution
// is priced from. Legitimate places inside the corridor snapped within 6.5 km,
// so the two cases are separated by an order of magnitude and a radius can tell
// them apart.
//
// The radius is sent on every routing call rather than checked afterwards,
// because OSRM answers NoSegment for it and that error already has a path here.
func TestEveryRoutingCallBoundsHowFarAPointMaySnap(t *testing.T) {
	// Each call wants a different shape back, so each brings its own body.
	const okPolyline = `{"code":"Ok","routes":[{"distance":1,"duration":1,"geometry":"a"}]}`
	const okGeoJSON = `{"code":"Ok","routes":[{"distance":1,"duration":1,` +
		`"geometry":{"coordinates":[[26.5,41.1],[26.6,41.2]]},"legs":[]}]}`
	const okNearest = `{"code":"Ok","waypoints":[{"location":[26.5,41.1],"distance":3}]}`

	for _, tc := range []struct {
		name string
		body string
		want int
		call func(c *OSRM) error
	}{
		{"Route", okPolyline, 2, func(c *OSRM) error {
			_, err := c.Route(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})
			return err
		}},
		{"RouteWithShape", okGeoJSON, 2, func(c *OSRM) error {
			_, err := c.RouteWithShape(context.Background(), []Point{{Lon: 26.5, Lat: 41.1}, {Lon: 26.6, Lat: 41.2}})
			return err
		}},
		{"Nearest", okNearest, 1, func(c *OSRM) error {
			_, err := c.Nearest(context.Background(), Point{Lon: 26.5, Lat: 41.1})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
				// Read from RawQuery rather than Query(): OSRM separates
				// radiuses with ';', and Go has refused to parse a semicolon as
				// a query separator since 1.17, so Query() hands back nothing
				// at all. The wire format is OSRM's to define.
				for _, kv := range strings.Split(r.URL.RawQuery, "&") {
					if rest, ok := strings.CutPrefix(kv, "radiuses="); ok {
						got = rest
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})

			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got == "" {
				t.Fatal("no radiuses sent: an uncovered point would snap silently instead of being refused")
			}
			// One radius per coordinate, which is what OSRM requires.
			parts := strings.Split(got, ";")
			if len(parts) != tc.want {
				t.Errorf("sent %d radiuses for %d coordinates: %q", len(parts), tc.want, got)
			}
			for _, r := range parts {
				if r != strconv.Itoa(SnapRadiusMetres) {
					t.Errorf("radius = %q, want %d", r, SnapRadiusMetres)
				}
			}
		})
	}
}

// The radius has to be wide enough for a real Greek hamlet whose nearest mapped
// road is some way off, and narrow enough that a city outside the extract is
// refused rather than dragged inside it. The measurements above put the gap
// between 6.5 km and 173 km.
func TestTheSnapRadiusSitsInsideTheMeasuredGap(t *testing.T) {
	if SnapRadiusMetres <= 6_500 {
		t.Errorf("SnapRadiusMetres = %d: Ξάνθη snapped 6.5 km out on the served extract and would be refused",
			SnapRadiusMetres)
	}
	if SnapRadiusMetres >= 173_000 {
		t.Errorf("SnapRadiusMetres = %d: Θεσσαλονίκη snapped 173 km out and would still be accepted",
			SnapRadiusMetres)
	}
}

// What the caller gets when a point is outside the extract, which is the whole
// point of sending the radius: a named error rather than a plausible number.
func TestAPointOutsideTheExtractIsRefusedRatherThanSnapped(t *testing.T) {
	c := osrmStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"NoSegment","message":"Could not find a matching segment for coordinate 1"}`))
	})

	_, err := c.RouteWithShape(context.Background(),
		[]Point{{Lon: 26.5, Lat: 41.1}, {Lon: 23.7275, Lat: 37.9838}})
	if !errors.Is(err, ErrNoSegment) {
		t.Fatalf("error = %v, want ErrNoSegment", err)
	}
}
