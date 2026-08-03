package api

import (
	"net/http"
	"strconv"
	"strings"

	"carpool/internal/places"
)

// How many settlements one query can return. A suggestion list nobody scrolls
// past the top of does not need more, and the limit arrives from the client.
// MaxPlaceResults is exported because it is part of the endpoint's contract
// rather than an internal detail: a client cannot ask for more than this.
const (
	defaultPlaceResults = 8
	MaxPlaceResults     = 20
)

// handlePlaces backs the field where a pickup point is typed.
//
// Before this, both ends of a route were typed as "25.8743, 40.8476
// Αλεξανδρούπολη", which is not something a person does twice. The list is
// embedded in the binary, so this reads nothing and can answer on every
// keystroke.
func (d Deps) handlePlaces(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "q is required")
		return
	}

	// A limit that is missing, negative, zero or not a number is a client that
	// did not ask, not a client to refuse: the field still needs suggestions.
	limit := defaultPlaceResults
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > MaxPlaceResults {
		limit = MaxPlaceResults
	}

	found := places.Search(q, limit)
	if found == nil {
		// Explicitly not null: an empty list is the ordinary answer halfway
		// through typing a word, and a client should not have to tell the two
		// apart.
		found = []places.Place{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": found})
}
