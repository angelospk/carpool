package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
//
// It also reads the account's status, on every request, from the database.
//
// That is a query per request and it is bought deliberately. A suspension has to
// take effect now, not when an access token happens to expire — a token minted
// an hour ago is otherwise an hour of somebody carrying on after the app decided
// they could not. Putting the status in the token would make it free and make it
// stale, which is exactly the wrong trade for the one thing moderation is for.
// Reading it here is also what makes reinstatement immediate.
func requireAuth(a authenticator, accounts accountReader) func(http.Handler) http.Handler {
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

			var status string
			err = accounts.QueryRow(r.Context(),
				`SELECT status FROM users WHERE id = $1`, userID).Scan(&status)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// The account is gone. Not "suspended": telling somebody they
				// had been punished when they had been erased is a different
				// and worse thing to say.
				writeError(w, http.StatusUnauthorized, "unauthorized", "the token is not valid")
				return
			case err != nil:
				// A database that cannot answer must not be read as consent.
				writeError(w, http.StatusInternalServerError, "internal", "could not check the account")
				return
			case status != "active":
				writeError(w, http.StatusForbidden, "account_suspended",
					"this account cannot be used")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
		})
	}
}

// accountReader is the part of pgx this middleware needs, so the test for it
// does not have to stand up a whole pool.
type accountReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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
