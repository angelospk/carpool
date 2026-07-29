package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// ctxKey is the unexported key type for values this package puts on a context.
type ctxKey int

const userIDKey ctxKey = iota

// authenticator validates a bearer token and returns the user it identifies.
type authenticator interface {
	ParseAccess(token string) (uuid.UUID, error)
}

// requireAuth rejects requests without a valid access token and puts the caller's
// id on the request context.
func requireAuth(a authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
				return
			}

			userID, err := a.ParseAccess(token)
			if err != nil {
				// The reason is not disclosed: expired, forged and malformed
				// all look the same to a caller.
				writeError(w, http.StatusUnauthorized, "unauthorized", "the token is not valid")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
		})
	}
}

// bearerToken extracts a bearer token from the Authorization header.
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

// callerID returns the authenticated user's id. It is only valid downstream of
// requireAuth.
func callerID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}
