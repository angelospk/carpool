package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// handleGroupExport is the month's verified trips between members, as CSV,
// for an admin to hand to whoever pays for them. See groups.Export for what
// counts as a trip and what is left out.
func (d Deps) handleGroupExport(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	groupID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such group")
		return
	}
	month, err := time.Parse("2006-01", r.URL.Query().Get("month"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_month", "month must be YYYY-MM")
		return
	}
	trips, err := d.Groups.Export(r.Context(), userID, groupID, month)
	if d.groupError(w, err) {
		return
	}

	loc := d.Location
	if loc == nil {
		loc = time.UTC
	}
	var buf bytes.Buffer
	// A byte-order mark, so that a spreadsheet opened by double-click reads the
	// Greek as Greek rather than as mojibake. It is what an admin will do.
	buf.WriteString("\uFEFF")
	out := csv.NewWriter(&buf)
	_ = out.Write([]string{"date", "departure", "from", "to", "route_km", "driver", "passenger", "seats"})
	for _, v := range trips {
		_ = out.Write([]string{
			v.ServiceDate.Format("2006-01-02"),
			v.DepartEarliest.In(loc).Format("15:04"),
			csvCell(v.OriginLabel),
			csvCell(v.DestinationLabel),
			strconv.FormatFloat(v.RouteKm, 'f', 1, 64),
			csvCell(v.Driver),
			csvCell(v.Passenger),
			strconv.Itoa(v.Seats),
		})
	}
	out.Flush()
	if err := out.Error(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not build the export")
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="trips-`+month.Format("2006-01")+`.csv"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Names of people and where they went. Not a thing for a shared cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// csvCell defuses a value a spreadsheet would run as a formula.
//
// Names and place labels are typed by members, and a cell beginning with one
// of these characters is evaluated when the file is opened — by the admin, on
// their own machine. Leading whitespace is skipped before looking, because a
// spreadsheet skips it too. A leading apostrophe makes the cell text.
func csvCell(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	return s
}
