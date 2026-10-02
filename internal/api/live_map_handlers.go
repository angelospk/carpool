package api

import (
	"net/http"

	"carpool/internal/places"
	"carpool/internal/store"
)

// handleLiveMap is the map of cars on the road now, for anybody signed in.
// What it may show is decided in store.LiveMap.
func (d Deps) handleLiveMap(w http.ResponseWriter, r *http.Request) {
	trips, err := store.LiveMap(r.Context(), d.Pool, d.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the map")
		return
	}
	// Settlement names worked out here from the ends, never the driver's own
	// labels; a car whose end is near no settlement is left off the map.
	shown := trips[:0]
	for _, t := range trips {
		from, okFrom := places.Nearest(t.OriginLon, t.OriginLat)
		to, okTo := places.Nearest(t.DestinationLon, t.DestinationLat)
		if !okFrom || !okTo {
			continue
		}
		t.From, t.To = from.Name, to.Name
		shown = append(shown, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": shown})
}
