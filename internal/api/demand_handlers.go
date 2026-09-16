package api

import (
	"net/http"
	"time"

	"carpool/internal/store"
)

// demandPeriod is how far ahead the count looks, and it is a product decision
// rather than a tuning knob: the screen says "this week", so a week is what the
// server counts. It is reported in the response so the app never has to guess
// at what period the numbers describe — a sentence saying "this week" over
// numbers covering a month is the failure mode.
const demandPeriod = 7 * 24 * time.Hour

// demandCorridors is how many roads a driver is shown. Five is the cap on a
// list anybody reads before deciding, and past that the tail is corridors with
// one person on them that would push the busy one off the top of the screen.
const demandCorridors = 5

// handleDemand answers "would anybody actually ride with me".
//
// It is the reason to publish, which the app had no way of giving: a driver with
// nothing published sees an empty form, and the requests that would have made
// the form worth filling in were only ever visible to the people who made them.
//
// Behind auth and deliberately so. It is other people's journeys, and it is not
// public even though it carries no names — an unauthenticated endpoint would let
// anybody map where a small community travels.
//
// Not guarded on any Deps collaborator: it only reads the pool, so a deployment
// wired without the matcher still answers rather than 404ing a screen that
// exists.
func (d Deps) handleDemand(w http.ResponseWriter, r *http.Request) {
	if _, ok := callerID(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	corridors, err := store.Demand(r.Context(), d.Pool, d.now(), demandPeriod, demandCorridors)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load who is waiting")
		return
	}

	// Built field by field rather than by marshalling store.Corridor, so that a
	// field added to the struct cannot reach every signed-in user by being
	// exported. This is the wire, and the wire is the boundary that matters.
	out := make([]map[string]any, 0, len(corridors))
	for _, c := range corridors {
		out = append(out, map[string]any{
			"origin_label":      c.OriginLabel,
			"destination_label": c.DestinationLabel,
			"people":            c.People,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"corridors":   out,
		"period_days": int(demandPeriod / (24 * time.Hour)),
	})
}
