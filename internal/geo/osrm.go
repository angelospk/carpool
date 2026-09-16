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

// SnapRadiusMetres bounds how far a coordinate may be dragged to reach a road.
//
// It exists because a clipped extract does not refuse what it does not cover —
// it snaps to the nearest road it does have and answers Ok. Measured against
// the extract this deployment serves, Evros plus a ~30 km buffer: Θεσσαλονίκη
// snapped 173 km away, Αθήνα 313 km, Πάτρα 408 km, and Θεσσαλονίκη → Αθήνα came
// back as a confident 186,6 km line drawn through Thrace. Nothing downstream
// could tell that from a real answer — it is the geometry a published route is
// stored with and the distance a contribution is priced from.
//
// Twenty kilometres sits in the measured gap. Legitimate places inside the
// served corridor snapped within 6,5 km (Ξάνθη, at the edge of the clip), and
// the fabrications start at 173 km, so there is an order of magnitude either
// side of this number. It is generous on purpose: a hamlet whose nearest mapped
// road is a few kilometres of track away is a real place somebody lives in, and
// refusing it costs a pickup the app could have served.
const SnapRadiusMetres = 20_000

// radiuses is the per-coordinate snapping bound OSRM expects, one value per
// waypoint. Sent on every routing call rather than checked after the fact,
// because OSRM answers NoSegment for it and that already has an error path.
func radiuses(n int) string {
	if n <= 0 {
		return ""
	}
	one := strconv.Itoa(SnapRadiusMetres)
	out := make([]string, n)
	for i := range out {
		out[i] = one
	}
	return strings.Join(out, ";")
}

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

	// SeaCrossing reports that reaching the destination needed a ferry, and
	// that everything below describes only the driving as far as the port.
	SeaCrossing bool
	// Port is where the driving ends, set only when SeaCrossing is true.
	Port Point
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

// osrmLegs is read separately from osrmResponse because only Route asks for
// steps, and every other caller would carry the cost of parsing them.
type osrmLegs struct {
	Routes []struct {
		Legs []struct {
			Steps []struct {
				Mode     string `json:"mode"`
				Maneuver struct {
					Location []float64 `json:"location"`
				} `json:"maneuver"`
			} `json:"steps"`
		} `json:"legs"`
	} `json:"routes"`
}

// ErrSeaCrossingOnly is returned when a journey is a ferry crossing with no
// driving before it.
var ErrSeaCrossingOnly = errors.New("osrm: the journey begins at the water")

// ferryMode is OSRM's name for a step taken on a boat.
const ferryMode = "ferry"

// Route computes the driving route through the given waypoints in order.
func (c *OSRM) Route(ctx context.Context, waypoints []Point) (Route, error) {
	if len(waypoints) < 2 {
		return Route{}, fmt.Errorf("osrm: a route needs at least 2 waypoints, got %d", len(waypoints))
	}

	coords := make([]string, len(waypoints))
	for i, p := range waypoints {
		coords[i] = formatPoint(p)
	}
	// steps=true is what makes a ferry visible. Without it OSRM will happily
	// route a car across the Aegean and report it as driving.
	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=full&geometries=polyline6&steps=true&radiuses=%s",
		c.baseURL, strings.Join(coords, ";"), radiuses(len(waypoints)))

	body, err := c.getBody(ctx, url)
	if err != nil {
		return Route{}, err
	}

	var resp osrmResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Route{}, fmt.Errorf("osrm: decoding response: %w", err)
	}
	if resp.Code != "Ok" {
		return Route{}, codeError(resp)
	}
	if len(resp.Routes) == 0 {
		return Route{}, fmt.Errorf("osrm: response code Ok but no routes returned")
	}

	r := resp.Routes[0]
	route := Route{
		DistanceMeters: r.Distance,
		Duration:       time.Duration(r.Duration * float64(time.Second)),
		Geometry:       r.Geometry,
	}

	port, legIndex, found := firstFerry(body)
	if !found {
		return route, nil
	}
	return c.truncateAtPort(ctx, waypoints, port, legIndex)
}

// firstFerry finds where the driving stops: the coordinate the first ferry
// step departs from, and the leg it belongs to.
func firstFerry(body []byte) (port Point, legIndex int, found bool) {
	var parsed osrmLegs
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Routes) == 0 {
		// Not being able to see the steps is not a reason to fail a route that
		// OSRM answered. It does mean a ferry could pass unnoticed, which is
		// why the shape above is the smallest one that can be parsed.
		return Point{}, 0, false
	}

	for i, leg := range parsed.Routes[0].Legs {
		for _, s := range leg.Steps {
			if s.Mode != ferryMode {
				continue
			}
			if len(s.Maneuver.Location) != 2 {
				return Point{}, 0, false
			}
			return Point{Lon: s.Maneuver.Location[0], Lat: s.Maneuver.Location[1]}, i, true
		}
	}
	return Point{}, 0, false
}

// truncateAtPort re-routes the journey to end where the boat begins.
//
// The trip is the driving. Somebody taking a car onto a ferry buys a ticket
// this service knows nothing about, and the fuel formula would charge them for
// the crossing as though they had driven it — a hundred and fifty kilometres of
// sea at the price of petrol.
//
// Re-routing rather than adding up the steps before the ferry: it costs one
// more call in the rare case, and it returns a geometry, a distance and a
// duration that agree with each other, which arithmetic over a truncated step
// list does not.
func (c *OSRM) truncateAtPort(ctx context.Context, waypoints []Point, port Point, legIndex int) (Route, error) {
	// Leg i runs from waypoint i to waypoint i+1, so everything up to and
	// including waypoint legIndex is still driven. A passenger picked up on the
	// way to the port is still being picked up.
	kept := append(append([]Point{}, waypoints[:legIndex+1]...), port)
	if len(kept) < 2 {
		return Route{}, ErrSeaCrossingOnly
	}

	landward, err := c.routeWithoutFerryCheck(ctx, kept)
	if err != nil {
		return Route{}, err
	}
	// The journey started at the water: there is no driving to sell.
	if landward.DistanceMeters == 0 {
		return Route{}, ErrSeaCrossingOnly
	}

	landward.SeaCrossing = true
	landward.Port = port
	return landward, nil
}

// routeWithoutFerryCheck is Route without the truncation, so that the re-route
// cannot recurse if OSRM still reports a ferry between the origin and the port.
func (c *OSRM) routeWithoutFerryCheck(ctx context.Context, waypoints []Point) (Route, error) {
	coords := make([]string, len(waypoints))
	for i, p := range waypoints {
		coords[i] = formatPoint(p)
	}
	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=full&geometries=polyline6&radiuses=%s",
		c.baseURL, strings.Join(coords, ";"), radiuses(len(coords)))

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
	url := fmt.Sprintf("%s/nearest/v1/driving/%s?number=1&radiuses=%s",
		c.baseURL, formatPoint(p), radiuses(1))

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
	body, err := c.getBody(ctx, url)
	if err != nil {
		return osrmResponse{}, err
	}
	var parsed osrmResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return osrmResponse{}, fmt.Errorf("osrm: decoding response: %w", err)
	}
	return parsed, nil
}

// getBody returns the raw response, so a caller that needs two views of the
// same payload does not have to fetch it twice.
func (c *OSRM) getBody(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("osrm: building request: %w", err)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osrm: request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		// Read a little of the body to make the error diagnosable without
		// risking an unbounded read from a misbehaving sidecar.
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("osrm: unexpected status %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	}

	// Bounded, because everything below decodes it in memory and a national
	// route with steps is already the largest response this client handles.
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("osrm: reading response: %w", err)
	}
	return body, nil
}

// maxResponseBytes caps a single OSRM response. A cross-country route with
// steps is a few hundred kilobytes; this is far above that and far below
// anything that would matter to the process.
const maxResponseBytes = 32 << 20

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

// ShapedRoute is a route together with its decoded geometry.
type ShapedRoute struct {
	DistanceMeters float64
	Duration       time.Duration
	// Shape is the route line as ordered coordinates.
	Shape []Point

	// SeaCrossing reports that the journey needed a ferry and that everything
	// above describes only the driving as far as Port.
	SeaCrossing bool
	Port        Point
}

// geoJSONRouteResponse mirrors osrmResponse but with a GeoJSON geometry.
type geoJSONRouteResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"routes"`
}

// RouteWithShape routes through the waypoints and returns the road-network
// geometry as coordinates.
//
// GeoJSON is requested rather than an encoded polyline because the shape is
// destined for PostGIS: decoding a polyline first would add a codec, and a
// rounding bug there would quietly distort every stored route. The app is
// served an encoded polyline from the stored geometry instead.
func (c *OSRM) RouteWithShape(ctx context.Context, waypoints []Point) (ShapedRoute, error) {
	shaped, body, err := c.fetchShape(ctx, waypoints, true)
	if err != nil {
		return ShapedRoute{}, err
	}

	port, legIndex, found := firstFerry(body)
	if !found {
		return shaped, nil
	}
	return c.shapeAsFarAsThePort(ctx, waypoints, port, legIndex)
}

// shapeWithoutFerryCheck is the plain fetch, used for the re-route so that it
// cannot recurse if OSRM still reports a ferry between the origin and the port.
func (c *OSRM) shapeWithoutFerryCheck(ctx context.Context, waypoints []Point) (ShapedRoute, error) {
	shaped, _, err := c.fetchShape(ctx, waypoints, false)
	return shaped, err
}

// fetchShape routes through the waypoints and returns the shaped result along
// with the raw body, which the caller needs a second view of to see the steps.
//
// A driver posting Athens to Mykonos is posting a drive to the port, and the
// steps are what say so; without them the stored trip is a line across the
// Aegean priced as though it were tarmac. They are asked for only on the first
// call, because the re-route has nothing left to discover.
func (c *OSRM) fetchShape(ctx context.Context, waypoints []Point, withSteps bool) (ShapedRoute, []byte, error) {
	if len(waypoints) < 2 {
		return ShapedRoute{}, nil, fmt.Errorf("osrm: a route needs at least 2 waypoints, got %d", len(waypoints))
	}

	coords := make([]string, len(waypoints))
	for i, p := range waypoints {
		coords[i] = formatPoint(p)
	}
	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=full&geometries=geojson&radiuses=%s",
		c.baseURL, strings.Join(coords, ";"), radiuses(len(coords)))
	if withSteps {
		url += "&steps=true"
	}

	body, err := c.getBody(ctx, url)
	if err != nil {
		return ShapedRoute{}, nil, err
	}

	var parsed geoJSONRouteResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ShapedRoute{}, nil, fmt.Errorf("osrm: decoding response: %w", err)
	}
	if parsed.Code != "Ok" {
		return ShapedRoute{}, nil, codeError(osrmResponse{Code: parsed.Code, Message: parsed.Message})
	}
	if len(parsed.Routes) == 0 {
		return ShapedRoute{}, nil, fmt.Errorf("osrm: response code Ok but no routes returned")
	}

	r := parsed.Routes[0]
	shape := make([]Point, 0, len(r.Geometry.Coordinates))
	for _, c := range r.Geometry.Coordinates {
		if len(c) < 2 {
			continue
		}
		shape = append(shape, Point{Lon: c[0], Lat: c[1]})
	}
	if len(shape) < 2 {
		return ShapedRoute{}, nil, fmt.Errorf("osrm: route geometry has %d points, need at least 2", len(shape))
	}

	return ShapedRoute{
		DistanceMeters: r.Distance,
		Duration:       time.Duration(r.Duration * float64(time.Second)),
		Shape:          shape,
	}, body, nil
}

// shapeAsFarAsThePort re-routes a posted trip to end where the boat begins.
// The reasoning is the same as truncateAtPort's, and so is the shape of it.
func (c *OSRM) shapeAsFarAsThePort(ctx context.Context, waypoints []Point, port Point, legIndex int) (ShapedRoute, error) {
	kept := append(append([]Point{}, waypoints[:legIndex+1]...), port)
	if len(kept) < 2 {
		return ShapedRoute{}, ErrSeaCrossingOnly
	}

	landward, err := c.shapeWithoutFerryCheck(ctx, kept)
	if err != nil {
		return ShapedRoute{}, err
	}
	if landward.DistanceMeters == 0 {
		return ShapedRoute{}, ErrSeaCrossingOnly
	}

	landward.SeaCrossing = true
	landward.Port = port
	return landward, nil
}

// WKT renders a shape as a PostGIS LineString literal.
func (s ShapedRoute) WKT() string {
	var b strings.Builder
	b.WriteString("LINESTRING(")
	for i, p := range s.Shape {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatFloat(p.Lon, 'f', -1, 64))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatFloat(p.Lat, 'f', -1, 64))
	}
	b.WriteByte(')')
	return b.String()
}
