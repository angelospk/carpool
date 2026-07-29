package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody is the single error envelope every endpoint returns. Freezing this
// shape early keeps the mobile client's error handling stable while the rest of
// the API is still moving.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError renders an error response in the standard envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeJSON renders v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already committed, so there is nothing to do for the
		// client; record it so a broken serializer is not silent.
		slog.Error("writing json response", "error", err)
	}
}
