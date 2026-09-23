package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/groups"
)

func groupJSON(g groups.Group) map[string]any {
	out := map[string]any{
		"id": g.ID, "name": g.Name, "role": g.Role, "members": g.Members,
	}
	// Absent rather than empty for a member, so that a client cannot mistake
	// "you may not see it" for "there is none".
	if g.Code != "" {
		out["code"] = g.Code
	}
	return out
}

func (d Deps) handleMyGroups(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	mine, err := d.Groups.Mine(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load your groups")
		return
	}
	out := make([]map[string]any, 0, len(mine))
	for _, g := range mine {
		out = append(out, groupJSON(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": out})
}

func (d Deps) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	g, err := d.Groups.Create(r.Context(), userID, body.Name)
	if d.groupError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, groupJSON(g))
}

func (d Deps) handleJoinGroup(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	g, err := d.Groups.Join(r.Context(), userID, body.Code)
	if d.groupError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, groupJSON(g))
}

func (d Deps) handleLeaveGroup(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	groupID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such group")
		return
	}
	if d.groupError(w, d.Groups.Leave(r.Context(), userID, groupID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleRotateGroupCode(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	groupID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such group")
		return
	}
	code, err := d.Groups.RotateCode(r.Context(), userID, groupID)
	if d.groupError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code})
}

// groupError writes the response for a groups error and reports whether it
// did. A nil error writes nothing.
func (d Deps) groupError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, groups.ErrBadName):
		writeError(w, http.StatusBadRequest, "invalid_name",
			"a group needs a name between 2 and 60 characters that somebody can read")
	case errors.Is(err, groups.ErrCodeNotUsable):
		writeError(w, http.StatusBadRequest, "code_not_usable", "that code does not open any group")
	case errors.Is(err, groups.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts",
			"too many wrong codes today, try again tomorrow")
	case errors.Is(err, groups.ErrTooManyGroups):
		writeError(w, http.StatusConflict, "too_many_groups", "you are already in as many groups as one person can be")
	case errors.Is(err, groups.ErrNotMember):
		// Not a 403: a group you are not in should not be shown to exist.
		writeError(w, http.StatusNotFound, "not_found", "no such group")
	case errors.Is(err, groups.ErrNotAdmin):
		writeError(w, http.StatusForbidden, "not_admin", "only the group's admins can do that")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "could not update the group")
	}
	return true
}
