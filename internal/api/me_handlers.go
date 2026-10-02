package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

	out := userResponse(user)
	if !d.addAsksPaused(r, out) {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the profile")
		return
	}
	writeJSON(w, http.StatusOK, out)
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
		FacebookHandle  *string   `json:"facebook_handle"`
		InstagramHandle *string   `json:"instagram_handle"`
		Gender          *string   `json:"gender"`
		WomenOnly       *bool     `json:"women_only"`
		RegionID        *string   `json:"home_region_id"`
		Bio             *string   `json:"bio"`
		Preferences     *[]string `json:"preferences"`
		// AsksPaused stops the matcher asking this driver, without retiring
		// any route. Only meaningful for somebody with a car.
		AsksPaused *bool `json:"asks_paused"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	up := store.UserUpdate{
		Gender:    body.Gender,
		WomenOnly: body.WomenOnly,
	}

	if body.Gender != nil {
		switch *body.Gender {
		case "female", "male", "other":
		default:
			writeError(w, http.StatusBadRequest, "invalid_gender", "gender is female, male or other")
			return
		}
	}
	// Women drivers only is for a woman to turn on. Asked for by anybody else it
	// is refused rather than stored: a switch that shows "on" and does nothing
	// is what this setting used to be.
	if body.WomenOnly != nil && *body.WomenOnly {
		gender := body.Gender
		if gender == nil {
			if err := d.Pool.QueryRow(r.Context(),
				`SELECT gender FROM users WHERE id = $1`, userID).Scan(&gender); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not read your profile")
				return
			}
		}
		if gender == nil || *gender != "female" {
			writeError(w, http.StatusBadRequest, "women_only_needs_female",
				"women drivers only is a setting for women; set your gender first")
			return
		}
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
	if body.Bio != nil {
		bio, err := domain.NormaliseBio(*body.Bio)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bio_too_long",
				"that is longer than a profile takes")
			return
		}
		// Checked after the length, because the length bounds the work, and
		// before anything is stored, because a bio that reaches the database is
		// a bio somebody can already read.
		if found, kind := domain.HasContactDetails(bio); found {
			// The kind goes in the code, so the app can name the thing to take
			// out without the error envelope growing a field. What does not go
			// anywhere is the matched text: echoing somebody's phone number
			// into an error response puts it in a log, which is where it was
			// not supposed to be in the first place.
			writeError(w, http.StatusBadRequest, "bio_contains_"+string(kind),
				"take the contact details out of your profile; it is visible to everybody, not only to whoever you travel with")
			return
		}
		up.Bio = &bio
	}
	if body.Preferences != nil {
		tags, err := domain.NormalisePreferences(*body.Preferences)
		switch {
		case errors.Is(err, domain.ErrContradictoryTags):
			writeError(w, http.StatusBadRequest, "contradictory_preferences",
				"those two cannot both be true")
			return
		case err != nil:
			writeError(w, http.StatusBadRequest, "unknown_preference",
				"that is not a preference this app knows")
			return
		}
		up.Preferences = &tags
	}
	if body.RegionID != nil {
		regionID, err := uuid.Parse(*body.RegionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "home_region_id must be a uuid")
			return
		}
		up.RegionID = &regionID
	}

	up.AsksPaused = body.AsksPaused

	user, err := store.UpdateUser(r.Context(), d.Pool, userID, up)
	if errors.Is(err, store.ErrNotADriver) {
		writeError(w, http.StatusConflict, "not_a_driver", "only a driver has asks to pause")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "the account no longer exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the update was rejected")
		return
	}

	out := userResponse(user)
	if !d.addAsksPaused(r, out) {
		writeError(w, http.StatusInternalServerError, "internal", "could not load the profile")
		return
	}
	writeJSON(w, http.StatusOK, out)
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
			"id":   region.ID,
			"name": region.Name,
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
		"bio":            u.Bio,
		// Never nil, so a client can render a list without a null check and
		// somebody with no preferences is an empty row rather than a missing
		// one.
		"preferences": nonNil(u.Preferences),
		// Composed rather than stored, from a fixed origin plus a handle that
		// has already been checked, so nothing a user typed decides where a
		// link points.
		"facebook_handle":  u.FacebookHandle,
		"instagram_handle": u.InstagramHandle,
		"facebook_url":     socialURL(domain.FacebookURL, u.FacebookHandle),
		"instagram_url":    socialURL(domain.InstagramURL, u.InstagramHandle),
	}
}

// nonNil turns a nil slice into an empty one, so that JSON carries [] rather
// than null and every client renders "no preferences" the same way.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// addAsksPaused puts the driver's pause switch on a profile response, and
// leaves it absent for somebody with no car: there is nothing to pause, and a
// false there would read as a switch they could turn on.
func (d Deps) addAsksPaused(r *http.Request, out map[string]any) bool {
	userID, ok := callerID(r.Context())
	if !ok {
		return true
	}
	var paused bool
	err := d.Pool.QueryRow(r.Context(),
		`SELECT notifications_muted FROM driver_profiles WHERE user_id = $1`, userID).Scan(&paused)
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	if err != nil {
		return false
	}
	out["asks_paused"] = paused
	return true
}
