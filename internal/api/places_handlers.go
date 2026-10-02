package api

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"carpool/internal/places"
	"carpool/internal/store"
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
	// One character is not a search, but it is also not a mistake: it is the
	// first keystroke of one. Answering 400 would flash an error under the
	// field of somebody who is doing nothing wrong, so a query too short to
	// mean anything gets the same empty list as one that matches nothing.
	if len([]rune(q)) < places.MinQuery {
		writeJSON(w, http.StatusOK, map[string]any{"places": []places.Place{}})
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

// handleMeetingPoints answers "where exactly" once a town has been chosen.
//
// The settlement is named in a query parameter rather than in the path because
// a place id is "node/139260064", and a path segment with a slash in it is a
// route nobody can read six months later.
func (d Deps) handleMeetingPoints(w http.ResponseWriter, r *http.Request) {
	place := strings.TrimSpace(r.URL.Query().Get("place"))
	if place == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "place is required")
		return
	}

	// The harvested list with every approved correction applied. A failure here
	// is a failure: falling back to the raw list would put a point somebody
	// removed for a reason back in front of people.
	found, err := store.MeetingPointsFor(r.Context(), d.Pool, place)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"could not read the meeting points for that place")
		return
	}
	if found == nil {
		// Not an error and not a 404. Four settlements in five have no landmark
		// anybody would recognise, and that is an ordinary answer the app has
		// something to say about, not a failure.
		found = []places.MeetingPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": found})
}

// handlePlaceNear names the settlement at a coordinate, so a form can start
// from where the phone is instead of from a typed name. 404 when nothing is
// close enough to be the answer. A POST, because a phone's position in a query
// string ends up in proxy and access logs.
func handlePlaceNear(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lon *float64 `json:"lon"`
		Lat *float64 `json:"lat"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Lon == nil || body.Lat == nil ||
		math.IsNaN(*body.Lon) || math.IsNaN(*body.Lat) ||
		*body.Lon < -180 || *body.Lon > 180 || *body.Lat < -90 || *body.Lat > 90 {
		writeError(w, http.StatusBadRequest, "invalid_request", "lon and lat are required")
		return
	}
	p, ok := places.Nearest(*body.Lon, *body.Lat)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no place near that point")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"place": p})
}
