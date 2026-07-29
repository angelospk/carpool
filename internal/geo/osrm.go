// Package geo holds geographic primitives, the OSRM client and PostGIS helpers.
package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors let the matcher distinguish "this candidate is unroutable",
// which is an ordinary outcome worth dropping the candidate for, from "the
// routing sidecar is broken", which is not.
var (
	// ErrNoRoute means OSRM could not connect the requested waypoints.
	ErrNoRoute = errors.New("osrm: no route between the given points")
	// ErrNoSegment means a point could not be snapped to the road network.
	ErrNoSegment = errors.New("osrm: point does not snap to any road segment")
)

// Point is a WGS84 coordinate. Longitude first, matching OSRM's and PostGIS's
// axis order.
type Point struct {
	Lon float64
	Lat float64
}

// Route is a routed path between waypoints.
type Route struct {
	DistanceMeters float64
	Duration       time.Duration
	// Geometry is the encoded polyline as returned by OSRM.
	Geometry string
}

// Snap is the result of snapping a point to the road network.
type Snap struct {
	Point          Point
	DistanceMeters float64
}

// OSRM is a client for a self-hosted OSRM routing sidecar. All routing calls
// are server-side; the mobile app never talks to OSRM directly.
type OSRM struct {
	baseURL string
	http    *http.Client
}

// NewOSRM returns a client for the OSRM instance at baseURL. If httpClient is
// nil a client with a conservative timeout is used; callers should still pass a
// context deadline, since OSRM latency is the dominant cost of fine matching.
func NewOSRM(baseURL string, httpClient *http.Client) *OSRM {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OSRM{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// osrmResponse covers the fields this client reads from both services.
type osrmResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry string  `json:"geometry"`
	} `json:"routes"`
	Waypoints []struct {
		Location []float64 `json:"location"`
		Distance float64   `json:"distance"`
	} `json:"waypoints"`
}

// Route computes the driving route through the given waypoints in order.
func (c *OSRM) Route(ctx context.Context, waypoints []Point) (Route, error) {
	if len(waypoints) < 2 {
		return Route{}, fmt.Errorf("osrm: a route needs at least 2 waypoints, got %d", len(waypoints))
	}

	coords := make([]string, len(waypoints))
	for i, p := range waypoints {
		coords[i] = formatPoint(p)
	}
	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=full&geometries=polyline6",
		c.baseURL, strings.Join(coords, ";"))

	resp, err := c.get(ctx, url)
	if err != nil {
		return Route{}, err
	}
	if resp.Code != "Ok" {
		return Route{}, codeError(resp)
	}
	if len(resp.Routes) == 0 {
		return Route{}, fmt.Errorf("osrm: response code Ok but no routes returned")
	}

	r := resp.Routes[0]
	return Route{
		DistanceMeters: r.Distance,
		Duration:       time.Duration(r.Duration * float64(time.Second)),
		Geometry:       r.Geometry,
	}, nil
}

// Nearest snaps a point to the nearest routable road segment.
func (c *OSRM) Nearest(ctx context.Context, p Point) (Snap, error) {
	url := fmt.Sprintf("%s/nearest/v1/driving/%s?number=1", c.baseURL, formatPoint(p))

	resp, err := c.get(ctx, url)
	if err != nil {
		return Snap{}, err
	}
	if resp.Code != "Ok" {
		return Snap{}, codeError(resp)
	}
	if len(resp.Waypoints) == 0 {
		return Snap{}, fmt.Errorf("osrm: response code Ok but no waypoints returned")
	}

	wp := resp.Waypoints[0]
	if len(wp.Location) != 2 {
		return Snap{}, fmt.Errorf("osrm: waypoint location has %d coordinates, want 2", len(wp.Location))
	}
	return Snap{
		Point:          Point{Lon: wp.Location[0], Lat: wp.Location[1]},
		DistanceMeters: wp.Distance,
	}, nil
}

func (c *OSRM) get(ctx context.Context, url string) (osrmResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return osrmResponse{}, fmt.Errorf("osrm: building request: %w", err)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return osrmResponse{}, fmt.Errorf("osrm: request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		// Read a little of the body to make the error diagnosable without
		// risking an unbounded read from a misbehaving sidecar.
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return osrmResponse{}, fmt.Errorf("osrm: unexpected status %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var parsed osrmResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return osrmResponse{}, fmt.Errorf("osrm: decoding response: %w", err)
	}
	return parsed, nil
}

func codeError(resp osrmResponse) error {
	switch resp.Code {
	case "NoRoute":
		return fmt.Errorf("%w: %s", ErrNoRoute, resp.Message)
	case "NoSegment":
		return fmt.Errorf("%w: %s", ErrNoSegment, resp.Message)
	default:
		return fmt.Errorf("osrm: service returned code %q: %s", resp.Code, resp.Message)
	}
}

// formatPoint renders a coordinate as OSRM expects: lon,lat.
func formatPoint(p Point) string {
	return strconv.FormatFloat(p.Lon, 'f', -1, 64) + "," + strconv.FormatFloat(p.Lat, 'f', -1, 64)
}
