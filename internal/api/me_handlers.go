package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/domain"
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
		Name *string `json:"name"`
		// photo_url is deliberately absent. It used to be settable here, which
		// let anybody point their avatar at any URL on the internet: an origin
		// they control, serving whatever they like, fetched by every other
		// user's device. Photos now go through the upload path and nowhere else.
		FacebookHandle  *string `json:"facebook_handle"`
		InstagramHandle *string `json:"instagram_handle"`
		Gender          *string `json:"gender"`
		WomenOnly       *bool   `json:"women_only"`
		RegionID        *string `json:"home_region_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	up := store.UserUpdate{
		Gender:    body.Gender,
		WomenOnly: body.WomenOnly,
	}

	// Validated here rather than left to the column constraint, because a CHECK
	// violation aborts the transaction and comes back as "the update was
	// rejected", which tells a person nothing about which field to look at.
	if body.Name != nil {
		name, err := domain.NormaliseName(*body.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_name", "that name cannot be used")
			return
		}
		up.Name = &name
	}
	for _, h := range []struct {
		in  *string
		out **string
	}{
		{body.FacebookHandle, &up.FacebookHandle},
		{body.InstagramHandle, &up.InstagramHandle},
	} {
		if h.in == nil {
			continue
		}
		handle, err := domain.NormaliseHandle(*h.in)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_handle", "that is not a username")
			return
		}
		*h.out = &handle
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

// avatarURL is the address of an uploaded photo, or nothing.
//
// The legacy photo_url column is deliberately not a fallback. It holds URLs on
// origins the user chose, from before uploads existed, and serving one means
// every other user's device fetches an address somebody else controls, which
// tells that somebody who looked and when. Nothing can set it any more; the
// people who have one get an avatar again when they upload.
func avatarURL(id uuid.UUID, version *string) *string {
	if version == nil {
		return nil
	}
	url := "/v1/users/" + id.String() + "/photo?v=" + *version
	return &url
}

func socialURL(compose func(string) string, handle *string) *string {
	if handle == nil || *handle == "" {
		return nil
	}
	url := compose(*handle)
	return &url
}

func userResponse(u store.User) map[string]any {
	return map[string]any{
		"id":             u.ID,
		"phone":          u.Phone,
		"name":           u.Name,
		"photo_url":      avatarURL(u.ID, u.PhotoVersion),
		"gender":         u.Gender,
		"home_region_id": u.HomeRegionID,
		"women_only":     u.WomenOnly,
		"status":         u.Status,
		// Composed rather than stored, from a fixed origin plus a handle that
		// has already been checked, so nothing a user typed decides where a
		// link points.
		"facebook_handle":  u.FacebookHandle,
		"instagram_handle": u.InstagramHandle,
		"facebook_url":     socialURL(domain.FacebookURL, u.FacebookHandle),
		"instagram_url":    socialURL(domain.InstagramURL, u.InstagramHandle),
	}
}
