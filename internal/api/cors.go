package api

import "net/http"

// devCORS lets a browser talk to this API from a different origin.
//
// It exists for one job: the Expo web build. That build is served as static
// files on one port and the API listens on another, which makes every call from
// the page a cross-origin request. Without permission from the API the browser
// refuses to send them, and the app looks broken rather than blocked — which is
// a bad hour to spend, because nothing in the app's own logs says anything.
//
// The policy is as permissive as a policy gets: it echoes whatever origin
// asked, and allows credentials. That combination is precisely what must never
// reach a deployment. Echoing the origin back defeats the point of the
// same-origin rule, and doing it *with* credentials means any page a logged-in
// user happens to visit could call this API as them. So it is bound to DEV_MODE,
// which already announces itself in the log as printing OTP codes to the
// console, and a test asserts that a router built without it answers nothing.
//
// A real web client, if there ever is one, needs an allowlist of exact origins
// read from configuration. This is not that, and is not a step towards it.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Not a browser: curl, the mobile app, a probe. Nothing to permit.
			next.ServeHTTP(w, r)
			return
		}

		// Vary matters even in development, because a caching proxy between the
		// laptop and this machine would otherwise serve one origin's permission
		// to another.
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			// Echoed rather than listed: the browser has already said what it
			// intends to send, and a fixed list is one more thing to forget to
			// update when a header is added.
			if want := r.Header.Get("Access-Control-Request-Headers"); want != "" {
				w.Header().Set("Access-Control-Allow-Headers", want)
			} else {
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			}
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
