package api

import (
	"errors"
	"math"
	"net/http"

	"carpool/internal/geo"
	"carpool/internal/store"
)

// handleRoutePreview shows a driver the roads between two places before they
// publish: the router's own first, then its alternatives, each with a point
// that publishing sends back as `via` to mean "this one". Nothing is stored.
func (d Deps) handleRoutePreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := callerID(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	var body struct {
		OriginLon      float64 `json:"origin_lon"`
		OriginLat      float64 `json:"origin_lat"`
		DestinationLon float64 `json:"destination_lon"`
		DestinationLat float64 `json:"destination_lat"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	origin := geo.Point{Lon: body.OriginLon, Lat: body.OriginLat}
	destination := geo.Point{Lon: body.DestinationLon, Lat: body.DestinationLat}

	// The same rule as publishing, so a road is not drawn for a route that
	// would then be refused.
	_, err := store.RegionAt(r.Context(), d.Pool, origin.Lon, origin.Lat)
	if err == nil {
		_, err = store.RegionAt(r.Context(), d.Pool, destination.Lon, destination.Lat)
	}
	if errors.Is(err, store.ErrOutsideGreece) {
		writeError(w, http.StatusBadRequest, "outside_country",
			"that route leaves Greece, which is as far as this app goes")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not locate the route")
		return
	}

	ctx, cancel := contextWithRoutingDeadline(r)
	defer cancel()
	roads, err := d.Router.Alternatives(ctx, origin, destination)
	if errors.Is(err, geo.ErrNoSegment) {
		writeError(w, http.StatusBadRequest, "outside_service_area",
			"one of those places is outside the area this app can route yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "routing_failed",
			"could not compute a driving route between those points")
		return
	}

	out := make([]map[string]any, 0, len(roads))
	for i, road := range roads {
		var via any
		if i > 0 {
			p := geo.FarthestFrom(road.Shape, roads[0].Shape)
			via = []float64{p.Lon, p.Lat}
		}
		out = append(out, map[string]any{
			"polyline": road.Polyline,
			"km":       math.Round(road.DistanceMeters/100) / 10,
			"min":      int(math.Round(road.Duration.Minutes())),
			"via":      via,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}
