package geo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
