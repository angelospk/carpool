package api

import (
	"errors"
	"net/http"

	"carpool/internal/domain"
	"carpool/internal/geo"
	"carpool/internal/store"
	"carpool/internal/vehicles"
)

// typicalSeats is what a route usually offers, and so what an estimate divides
// by once the driver is added.
//
// It is the publication default rather than a measurement, which is the honest
// limit of it: with a few dozen routes there is no median worth taking, and a
// figure derived from six of them would look more principled than it is. Worth
// revisiting against real occurrences when there are enough to have a middle.
const typicalSeats = 3

// handleEstimate answers "roughly what will this cost", before any driver has
// seen the trip.
//
// A POST rather than a GET, on Codex's advice and for a reason worth keeping:
// exact origin and destination coordinates in a query string end up in proxy
// logs, in analytics and in browser history, and a pair of coordinates is where
// somebody lives and where they go. In a body they end up in none of those.
//
// Nothing is stored. No request, no occurrence, no estimate row: nobody has
// agreed to anything, and a stored figure invites somebody to argue the real
// price against a guess the app made before it knew whose car it was.
func (d Deps) handleEstimate(w http.ResponseWriter, r *http.Request) {
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

	// Both ends in the country, the same rule a real request answers to, so a
	// trip the app would refuse is not quoted a price first. Both, not just the
	// pickup: quoting somebody 30 € for Θεσσαλονίκη → Σόφια and then refusing
	// the request is worse than refusing it once.
	region, err := store.RegionAt(r.Context(), d.Pool, origin.Lon, origin.Lat)
	if err == nil {
		_, err = store.RegionAt(r.Context(), d.Pool, destination.Lon, destination.Lat)
	}
	if errors.Is(err, store.ErrOutsideGreece) {
		writeError(w, http.StatusBadRequest, "outside_country",
			"that trip leaves Greece, which is as far as this app goes")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not locate the trip")
		return
	}

	ctx, cancel := contextWithRoutingDeadline(r)
	defer cancel()

	route, err := d.Router.Route(ctx, []geo.Point{origin, destination})
	if errors.Is(err, geo.ErrSeaCrossingOnly) {
		writeError(w, http.StatusBadRequest, "sea_crossing",
			"that journey is a ferry crossing, so there is no drive to share")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "routing_failed",
			"could not compute a driving route between those points")
		return
	}

	// Both pump prices, because the estimate blends them: the driver is unknown,
	// so asking the passenger to reason about petrol against diesel is asking
	// the wrong person about a car that does not exist.
	petrol, err := store.FuelPriceAt(r.Context(), d.Pool, origin.Lon, origin.Lat, vehicles.Petrol)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_estimate",
			"no fuel price is on record, so there is nothing to estimate from")
		return
	}
	diesel, err := store.FuelPriceAt(r.Context(), d.Pool, origin.Lon, origin.Lat, vehicles.Diesel)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_estimate",
			"no fuel price is on record, so there is nothing to estimate from")
		return
	}

	typical, err := vehicles.TypicalCar()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read the vehicle data")
		return
	}
	costPerKm, err := typical.CostPerKmEUR(petrol.PriceEUR, diesel.PriceEUR)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_estimate",
			"nothing to estimate from")
		return
	}
	// How far up the figure could go with a thirsty car. Not a lower bound as
	// well: the thriftiest real car is five times cheaper than the ceiling, and
	// a range that wide reads as "nobody knows".
	dearest, err := vehicles.DearestCostPerKmEUR(petrol.PriceEUR, diesel.PriceEUR)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "no_estimate",
			"nothing to estimate from")
		return
	}

	// The older of the two prices, because the figure is only as current as its
	// stalest input.
	asOf := petrol.UpdatedAt
	if diesel.UpdatedAt.Before(asOf) {
		asOf = diesel.UpdatedAt
	}

	estimate := domain.Estimate(domain.EstimateInputs{
		RouteKm:             route.DistanceMeters / 1000,
		RouteMin:            int(route.Duration.Minutes()) + 1,
		CostPerKmEUR:        costPerKm,
		CeilingCostPerKmEUR: dearest,
		Divisor:             typicalSeats + 1,
		Version:             typical.Version,
		FuelPriceAsOf:       asOf.Format("2006-01-02"),
		RegionName:          region.Name,
	})

	// Nothing about this is worth keeping, and a cached estimate is a stale
	// price shown as a current one.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, estimate)
}
