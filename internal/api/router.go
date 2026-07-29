// Package api wires the HTTP surface: chi routes, handlers and middleware.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/auth"
	"carpool/internal/clock"
)

// readyTimeout bounds the readiness probe so a hung database cannot hold the
// probe open until the orchestrator's own timeout fires.
const readyTimeout = 2 * time.Second

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	// Ready reports whether downstream dependencies (currently Postgres) are
	// usable. A nil error means ready.
	Ready func(context.Context) error

	Pool     *pgxpool.Pool
	Clock    clock.Clock
	Issuer   *auth.TokenIssuer
	OTP      *auth.OTPService
	Sessions *auth.SessionService
}

// NewRouter builds the versioned HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "the requested resource does not exist")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "that method is not allowed on this resource")
	})

	r.Route("/v1", func(r chi.Router) {
		r.Get("/livez", handleLivez)
		// /health is the name used in ARCHITECTURE.md's API sketch; it is a
		// liveness probe, so it shares the handler.
		r.Get("/health", handleLivez)
		r.Get("/readyz", handleReadyz(deps.Ready))

		// The health probes above are the only routes that work without the
		// service dependencies, so that a half-configured process still reports
		// honestly rather than panicking.
		if deps.Pool == nil {
			return
		}

		r.Get("/regions", deps.handleListRegions)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/otp/request", deps.handleOTPRequest)
			r.Post("/otp/verify", deps.handleOTPVerify)
			r.Post("/refresh", deps.handleRefresh)
			r.Post("/logout", deps.handleLogout)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAuth(deps.Issuer))

			r.Get("/me", deps.handleGetMe)
			r.Patch("/me", deps.handlePatchMe)
		})
	})

	return r
}

func handleLivez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func handleReadyz(ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		if ready != nil {
			if err := ready(ctx); err != nil {
				writeError(w, http.StatusServiceUnavailable, "not_ready", "dependencies are not ready")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
