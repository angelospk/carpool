package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// logServerErrors writes one line for every response with a 5xx status.
//
// Most handlers answer a failed query with a 500 and a sentence for the user,
// and throw the error away. From the outside that is the right amount to say;
// from the inside it left nothing at all to go on — a list of routes that failed
// for every driver with a one-off route produced no log line, and the cause was
// found in Postgres's log instead. The route pattern rather than the path, so
// ids do not make every line unique.
func logServerErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if ww.Status() >= 500 {
			route := r.URL.Path
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			slog.Error("server error", "status", ww.Status(), "method", r.Method, "route", route)
		}
	})
}
