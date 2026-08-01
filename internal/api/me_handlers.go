package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/store"
)

func (d Deps) handleGetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	user, err := store.GetUser(r.Context(), d.Pool, userID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "the account no longer exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the profile")
		return
	}

	writeJSON(w, http.StatusOK, userResponse(user))
}

func (d Deps) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		Name      *string `json:"name"`
		PhotoURL  *string `json:"photo_url"`
		SocialURL *string `json:"social_url"`
		Gender    *string `json:"gender"`
		WomenOnly *bool   `json:"women_only"`
		RegionID  *string `json:"home_region_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	up := store.UserUpdate{
		Name:      body.Name,
		PhotoURL:  body.PhotoURL,
		SocialURL: body.SocialURL,
		Gender:    body.Gender,
		WomenOnly: body.WomenOnly,
	}
	if body.RegionID != nil {
		regionID, err := uuid.Parse(*body.RegionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "home_region_id must be a uuid")
			return
		}
		up.RegionID = &regionID
	}

	user, err := store.UpdateUser(r.Context(), d.Pool, userID, up)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "the account no longer exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the update was rejected")
		return
	}

	writeJSON(w, http.StatusOK, userResponse(user))
}

func (d Deps) handleListRegions(w http.ResponseWriter, r *http.Request) {
	regions, err := store.ListRegions(r.Context(), d.Pool)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list regions")
		return
	}

	out := make([]map[string]any, 0, len(regions))
	for _, region := range regions {
		out = append(out, map[string]any{
			"id":             region.ID,
			"name":           region.Name,
			"fuel_price_eur": region.FuelPriceEUR,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"regions": out})
}

func userResponse(u store.User) map[string]any {
	return map[string]any{
		"id":             u.ID,
		"phone":          u.Phone,
		"name":           u.Name,
		"photo_url":      u.PhotoURL,
		"gender":         u.Gender,
		"home_region_id": u.HomeRegionID,
		"women_only":     u.WomenOnly,
		"status":         u.Status,
		"social_url":     u.SocialURL,
	}
}
