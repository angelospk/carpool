package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/moderation"
)

// handleOpenReports is the queue. Oldest first, with the names and the count of
// what else has been said about the same person, because one complaint is a
// disagreement and the fifth is a pattern.
func (d Deps) handleOpenReports(w http.ResponseWriter, r *http.Request) {
	reports, err := d.Moderation.Open(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read the queue")
		return
	}

	out := make([]map[string]any, 0, len(reports))
	for _, rep := range reports {
		out = append(out, map[string]any{
			"id":              rep.ID,
			"reason":          rep.Reason,
			"created_at":      rep.CreatedAt,
			"reporter_id":     rep.ReporterID,
			"reporter_name":   rep.ReporterName,
			"reported_id":     rep.ReportedID,
			"reported_name":   rep.ReportedName,
			"reported_status": rep.ReportedStatus,
			"prior_reports":   rep.PriorReports,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

// handleDecideReport closes one report. suspend says which way.
//
// Two endpoints rather than one with a flag in the body, so that the log of a
// moderator's session reads as a list of what they decided rather than a list
// of identical calls.
func (d Deps) handleDecideReport(suspend bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		moderatorID, ok := callerID(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
			return
		}
		reportID, err := uuid.Parse(chiURLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "the report id must be a uuid")
			return
		}

		var body struct {
			Note string `json:"note"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}

		err = d.Moderation.Decide(r.Context(), reportID, moderatorID, suspend, body.Note)
		switch {
		case errors.Is(err, moderation.ErrNoReport):
			writeError(w, http.StatusNotFound, "not_found", "no such report")
		case errors.Is(err, moderation.ErrAlreadyDecided):
			// 409, not 404: the report exists and has been dealt with, which is
			// a different thing for a moderator to see than "it is not there".
			writeError(w, http.StatusConflict, "already_decided",
				"another moderator has already decided this one")
		case errors.Is(err, moderation.ErrDeleted):
			writeError(w, http.StatusConflict, "account_deleted",
				"that account has been erased; there is nothing left to suspend")
		case err != nil:
			writeError(w, http.StatusBadRequest, "invalid_request", "the decision was rejected")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// handleReinstate gives an account back. It is separate from deciding a report
// because an appeal is usually not about one complaint.
func (d Deps) handleReinstate(w http.ResponseWriter, r *http.Request) {
	moderatorID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	userID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the user id must be a uuid")
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	err = d.Moderation.Reinstate(r.Context(), userID, moderatorID, body.Note)
	switch {
	case errors.Is(err, moderation.ErrNoAccount):
		writeError(w, http.StatusNotFound, "not_found", "no such account")
	case errors.Is(err, moderation.ErrDeleted):
		writeError(w, http.StatusConflict, "account_deleted",
			"that account has been erased and cannot be brought back")
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "the reinstatement was rejected")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMyReports shows somebody what became of what they filed.
//
// It says whether the report was looked at. It does not say what happened to
// the other account: that is a fact about a stranger, and knowing it is not part
// of having complained.
func (d Deps) handleMyReports(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	reports, err := d.Moderation.ByReporter(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your reports")
		return
	}

	out := make([]map[string]any, 0, len(reports))
	for _, rep := range reports {
		out = append(out, map[string]any{
			"id":            rep.ID,
			"reason":        rep.Reason,
			"created_at":    rep.CreatedAt,
			"status":        rep.Status,
			"reported_name": rep.ReportedName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}
