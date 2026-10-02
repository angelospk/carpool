package api

import (
	"net/http"

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
	writeJSON(w, http.StatusOK, map[string]any{"trips": trips})
}
