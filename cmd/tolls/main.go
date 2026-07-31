// Command tolls shows the barriers on a route, what they cost, and how the
// cost divides between a driver and passengers who ride part of the way.
//
//	tolls -from 40.6401,22.9444 -to 40.8457,25.8740 \
//	      -rider "Καβάλα:40.9396,24.4069:40.8457,25.8740"
//
// Geometry comes from the OSRM sidecar at OSRM_URL, or from -polyline for a
// route outside the local extract.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"carpool/internal/geo"
	"carpool/internal/tolls"
)

func main() {
	var (
		from      = flag.String("from", "", "origin as lat,lon")
		to        = flag.String("to", "", "destination as lat,lon")
		polyline  = flag.String("polyline", "", "route geometry instead of routing one")
		precision = flag.Int("precision", 5, "polyline precision")
		osrmURL   = flag.String("osrm", envOr("OSRM_URL", "http://localhost:5000"), "OSRM base URL")
		class     = flag.String("class", "car", "vehicle class: car or motorcycle")
		riders    = flag.String("rider", "", "semicolon-separated: name:pickupLat,pickupLon:dropLat,dropLon[;...]")
	)
	flag.Parse()

	route, err := loadRoute(*polyline, *precision, *from, *to, *osrmURL)
	if err != nil {
		fail(err)
	}

	set, err := tolls.Load()
	if err != nil {
		fail(err)
	}

	result := set.Match(route)
	gates, priced := result.Gates(tolls.Class(*class))
	total, _ := result.Cents(tolls.Class(*class))

	fmt.Printf("route: %.1f km, %d points\n", routeKm(route), len(route))
	fmt.Printf("dataset: %s\n\n", result.DatasetDate)

	if len(result.Crossings) == 0 {
		fmt.Println("no toll stations on this route")
	}
	for _, c := range result.Crossings {
		cents, known := c.Station.Price(tolls.Class(*class))
		price := fmt.Sprintf("%6.2f €", float64(cents)/100)
		if !known {
			price = "     ? €"
		}
		fmt.Printf("  %6.1f km  %s  %-26s %s\n",
			c.MetresAlong/1000, price, c.Station.ShortEL, c.Station.OperatorEL)
	}
	if !priced {
		fmt.Println("\n(a station on this route has no price for that class; no total)")
		return
	}
	fmt.Printf("\n  total%s%.2f €\n", strings.Repeat(" ", 9), float64(total)/100)

	for _, a := range result.Ambiguous {
		fmt.Printf("  ? near %s, %.0f° off its direction — not charged\n",
			a.Station.ShortEL, a.HeadingOff)
	}

	if *riders == "" {
		return
	}

	rs, names, err := parseRiders(*riders, route)
	if err != nil {
		fail(err)
	}
	shares, err := tolls.Split(gates, rs)
	if err != nil {
		fail(err)
	}

	fmt.Printf("\nsplit between the driver and %d passenger(s):\n", len(rs))
	fmt.Printf("  %-22s %6.2f €\n", "driver", float64(shares.Driver)/100)
	for _, r := range rs {
		fmt.Printf("  %-22s %6.2f €   (%.0f–%.0f km)\n",
			names[r.ID], float64(shares.Riders[r.ID])/100,
			r.FromMetres/1000, r.ToMetres/1000)
	}
}

func loadRoute(polyline string, precision int, from, to, osrmURL string) ([]geo.Point, error) {
	if polyline != "" {
		return geo.DecodePolylinePrecision(polyline, precision)
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("need -polyline, or -from and -to")
	}

	a, err := parsePoint(from)
	if err != nil {
		return nil, err
	}
	b, err := parsePoint(to)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shaped, err := geo.NewOSRM(osrmURL, nil).RouteWithShape(ctx, []geo.Point{a, b})
	if err != nil {
		return nil, fmt.Errorf("routing: %w", err)
	}
	return shaped.Shape, nil
}

func parseRiders(spec string, route []geo.Point) ([]tolls.Rider, map[string]string, error) {
	names := map[string]string{}
	var out []tolls.Rider

	for i, entry := range strings.Split(spec, ";") {
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			return nil, nil, fmt.Errorf("rider %q: want name:pickup:dropoff", entry)
		}
		pickup, err := parsePoint(parts[1])
		if err != nil {
			return nil, nil, err
		}
		dropoff, err := parsePoint(parts[2])
		if err != nil {
			return nil, nil, err
		}
		fromM, toM, err := tolls.Segment(route, pickup, dropoff)
		if err != nil {
			return nil, nil, fmt.Errorf("rider %q: %w", parts[0], err)
		}
		id := strconv.Itoa(i)
		names[id] = parts[0]
		out = append(out, tolls.Rider{ID: id, FromMetres: fromM, ToMetres: toM})
	}
	return out, names, nil
}

func parsePoint(s string) (geo.Point, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return geo.Point{}, fmt.Errorf("point %q: want lat,lon", s)
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return geo.Point{}, fmt.Errorf("point %q: %w", s, err)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return geo.Point{}, fmt.Errorf("point %q: %w", s, err)
	}
	return geo.Point{Lat: lat, Lon: lon}, nil
}

func routeKm(route []geo.Point) float64 {
	total := 0.0
	for i := 1; i < len(route); i++ {
		total += tolls.DistanceM(route[i-1], route[i])
	}
	return total / 1000
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tolls:", err)
	os.Exit(1)
}
