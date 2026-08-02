package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"carpool/internal/api"
)

// A browser will not let a page at one port call an API at another without
// permission from the API. The Expo web build is served from :8099 and the API
// listens on :8080, so without this every request from the browser fails before
// it is sent, and the app looks broken rather than blocked.
func TestDevModeAllowsTheBrowserToCallTheAPI(t *testing.T) {
	h := api.NewRouter(api.Deps{Ready: func(context.Context) error { return nil }, DevMode: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/livez", nil)
	req.Header.Set("Origin", "http://192.168.0.154:8099")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.0.154:8099" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the requesting origin echoed back", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") == "" {
		t.Error("no Access-Control-Allow-Credentials, so a browser will drop the auth header")
	}
}

// Anything that carries a bearer token is preflighted, so a 405 on OPTIONS
// blocks every authenticated call in the browser.
func TestDevModeAnswersThePreflight(t *testing.T) {
	h := api.NewRouter(api.Deps{Ready: func(context.Context) error { return nil }, DevMode: true})

	req := httptest.NewRequest(http.MethodOptions, "/v1/auth/otp/request", nil)
	req.Header.Set("Origin", "http://192.168.0.154:8099")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	for _, header := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	} {
		if rec.Header().Get(header) == "" {
			t.Errorf("preflight answered without %s", header)
		}
	}
}

// The whole point of the gate. This is a permissive, echo-any-origin policy,
// which is exactly what must never reach a deployment: it would let any website
// a logged-in user visits call this API with their credentials.
func TestWithoutDevModeThereIsNoCORSAtAll(t *testing.T) {
	h := api.NewRouter(api.Deps{Ready: func(context.Context) error { return nil }})

	req := httptest.NewRequest(http.MethodGet, "/v1/livez", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q on a production router, want none", got)
	}

	pre := httptest.NewRequest(http.MethodOptions, "/v1/livez", nil)
	pre.Header.Set("Origin", "https://evil.example")
	pre.Header.Set("Access-Control-Request-Method", "GET")
	preRec := httptest.NewRecorder()
	h.ServeHTTP(preRec, pre)

	if preRec.Code == http.StatusNoContent {
		t.Error("a production router answered a preflight, so the dev gate is not gating")
	}
}

// A request with no Origin is not from a browser — curl, the mobile app, a
// health probe. Echoing an empty origin back would be meaningless at best.
func TestARequestWithoutAnOriginGetsNoCORSHeaders(t *testing.T) {
	h := api.NewRouter(api.Deps{Ready: func(context.Context) error { return nil }, DevMode: true})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/livez", nil))

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q with no Origin sent", got)
	}
}
