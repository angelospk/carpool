// Package api wires the HTTP surface: chi routes, handlers and middleware.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/account"
	"carpool/internal/asks"
	"carpool/internal/auth"
	"carpool/internal/bookings"
	"carpool/internal/clock"
	"carpool/internal/drivers"
	"carpool/internal/geo"
	"carpool/internal/invites"
	"carpool/internal/jobs"
	"carpool/internal/standing"
	"carpool/internal/ws"
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
	Asks     *asks.Service
	// Standing records the agreements two people already made, so the matcher
	// stops handing their seat to a stranger.
	Standing *standing.Service
	// Bookings cancels a seat, whichever way it was booked.
	Bookings *bookings.Service
	// Invites admits new accounts, and decides whether a code is needed at all.
	Invites *invites.Service
	// Account answers the two requests a person can make about their own data.
	Account *account.Service
	// Drivers records a driver's car, and works out what it drinks so that the
	// driver does not get to say.
	Drivers *drivers.Service

	// PhoneChange moves an account to another number, which is a change of
	// credential rather than of profile: see internal/auth/phone_change.go.
	PhoneChange *auth.PhoneChangeService

	// Router snaps published routes to the road network.
	Router *geo.OSRM
	// Materializer runs immediately after a route is published so it is
	// matchable at once rather than at the next scheduled tick.
	Materializer *jobs.Materializer
	// OSRMDataVersion is stamped on stored routes so a map-data swap can
	// trigger re-snapping.
	OSRMDataVersion string
	// Hub fans chat messages out to foregrounded apps. Optional.
	Hub *ws.Hub

	// DevMode relaxes things that must stay strict in a deployment: OTP codes
	// go to the log instead of an SMS, and the browser is allowed to call this
	// API cross-origin so the Expo web build can be used for testing.
	DevMode bool
}

// NewRouter builds the versioned HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	if deps.DevMode {
		r.Use(devCORS)
	}

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
			if deps.Account != nil {
				r.Get("/me/export", deps.handleExportMe)
				r.Delete("/me", deps.handleDeleteMe)
			}
			r.Put("/me/push-token", deps.handlePutPushToken)

			// A driver seeing and retiring their own routes. Reads and writes
			// the pool directly, so there is no collaborator to guard on.
			r.Get("/me/routes", deps.handleMyRoutes)
			r.Delete("/me/routes/{id}", deps.handleRetireRoute)

			if deps.PhoneChange != nil {
				r.Post("/me/phone/request", deps.handleRequestPhoneChange)
				r.Post("/me/phone/confirm", deps.handleConfirmPhoneChange)
			}

			r.Put("/me/photo", deps.handlePutPhoto)
			r.Delete("/me/photo", deps.handleDeletePhoto)

			// Guarded on its own collaborator: without it there is no way to
			// become a driver at all, and a nil dereference would be a panic
			// rather than an honest 404.
			if deps.Drivers != nil {
				r.Put("/me/driver", deps.handlePutDriver)
				r.Get("/vehicles/makes", deps.handleListVehicleMakes)
				r.Get("/vehicles/models", deps.handleListVehicleModels)
			}

			// No collaborator to guard: the settlement list is embedded in
			// the binary, so this handler cannot be half-wired.
			r.Get("/places", deps.handlePlaces)

			if deps.Router != nil {
				r.Post("/routes", deps.handleCreateRoute)
			}
			if deps.Asks != nil {
				r.Post("/requests", deps.handleCreateRequest)
				r.Delete("/requests/{id}", deps.handleCancelRequest)
				r.Post("/bookings/{id}/again", deps.handleAgain)
				r.Post("/asks/{id}/accept", deps.handleAcceptAsk)
				r.Post("/asks/{id}/decline", deps.handleDeclineAsk)
			}

			// Guarded on its own collaborator: these handlers dereference
			// deps.Standing, and hanging them off the asks check would panic in
			// any wiring that has one and not the other.
			if deps.Bookings != nil {
				r.Delete("/bookings/{id}", deps.handleCancelBooking)
			}
			if deps.Standing != nil {
				r.Post("/bookings/{id}/repeat", deps.handleRepeatBooking)
				r.Get("/arrangements", deps.handleListArrangements)
				r.Post("/arrangements/{id}/approve", deps.handleApproveArrangement)
				r.Post("/arrangements/{id}/decline", deps.handleDeclineArrangement)
				r.Delete("/arrangements/{id}", deps.handleCancelArrangement)
			}

			r.Get("/trips/upcoming", deps.handleUpcomingTrips)
			r.Get("/trips/past", deps.handlePastTrips)
			r.Get("/invites/mine", deps.handleMyInvite)
			r.Route("/trips/{id}", func(r chi.Router) {
				r.Get("/", deps.handleGetTrip)
				r.Post("/confirm", deps.handleConfirmTrip)
				if deps.Bookings != nil {
					r.Delete("/", deps.handleCancelTrip)
				}
				r.Post("/rate", deps.handleRateTrip)
				r.Get("/messages", deps.handleListMessages)
				r.Post("/messages", deps.handlePostMessage)
				if deps.Hub != nil {
					r.Get("/socket", deps.handleTripSocket)
				}
			})

			r.Route("/users/{id}", func(r chi.Router) {
				r.Get("/photo", deps.handleGetPhoto)
				r.Head("/photo", deps.handleGetPhoto)
				r.Post("/block", deps.handleBlockUser)
				r.Delete("/block", deps.handleUnblockUser)
				r.Post("/report", deps.handleReportUser)
			})
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

// chiURLParam reads a path parameter. It exists so handlers do not each import
// chi directly.
func chiURLParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// routingDeadline bounds an OSRM call made while a client waits.
const routingDeadline = 15 * time.Second

func contextWithRoutingDeadline(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), routingDeadline)
}
