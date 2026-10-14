package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ready(context.Context) error { return nil }

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// webBuild lays out the shape `expo export --platform web` produces.
func webBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":                       `<!doctype html><div id="root"></div><script src="/app/_expo/static/js/web/entry-abc.js" defer></script>`,
		"_expo/static/js/web/entry-abc.js": `console.log("app")`,
		"assets/node_modules/icon.png":     "png",
		"metadata.json":                    `{}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The root of the public hostname used to be a JSON 404. It is the address
// people are given, so it says what this is and how to try it.
func TestLandingPageSaysWhatThisIsAndHowToTryIt(t *testing.T) {
	h := NewRouter(Deps{Ready: ready, WebDir: webBuild(t), InstallURL: "https://carpool.example/get"})

	rec := get(t, h, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`lang="el"`, `href="/app/"`, `href="https://carpool.example/get"`, "Θράκη"} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page lacks %q", want)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want a locked-down policy", csp)
	}

	if rec := get(t, h, http.MethodHead, "/"); rec.Code != http.StatusOK {
		t.Errorf("HEAD / = %d, want 200", rec.Code)
	}
}

// Without a web build or an install link the page must not offer either: a
// button that leads to a 404 is worse than no button.
func TestLandingPageOffersOnlyWhatExists(t *testing.T) {
	h := NewRouter(Deps{Ready: ready, InstallURL: "http://not-https.example/get"})

	body := get(t, h, http.MethodGet, "/").Body.String()
	if strings.Contains(body, `href="/app/"`) {
		t.Error("offers the web app with no web build to serve")
	}
	if strings.Contains(body, "not-https.example") {
		t.Error("offers an install link that is not https")
	}
}

func TestUnknownPathsStayJSON404(t *testing.T) {
	h := NewRouter(Deps{Ready: ready, WebDir: webBuild(t)})
	rec := get(t, h, http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("GET /nope = %d %s", rec.Code, rec.Body.String())
	}
}

// The web build is a single-page app: every path the router knows is answered
// with index.html, files are served as themselves, and a missing file is a 404
// rather than a page of HTML pretending to be a script.
func TestWebAppIsServedUnderApp(t *testing.T) {
	h := NewRouter(Deps{Ready: ready, WebDir: webBuild(t)})

	rec := get(t, h, http.MethodGet, "/app")
	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("GET /app = %d, want a redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/" {
		t.Fatalf("Location = %q, want /app/", loc)
	}
	if rec := get(t, h, http.MethodHead, "/app"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("HEAD /app = %d, want a redirect", rec.Code)
	}

	for _, path := range []string{"/app/", "/app/trip/123", "/app/r/abc_DEF-1", "/app/phone"} {
		rec := get(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<div id="root">`) {
			t.Errorf("GET %s = %d, want index.html", path, rec.Code)
			continue
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache so a deploy is picked up", path, cc)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("GET %s CSP = %q", path, csp)
		}
	}

	rec = get(t, h, http.MethodGet, "/app/_expo/static/js/web/entry-abc.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle = %d", rec.Code)
	}
	if b, _ := io.ReadAll(rec.Body); string(b) != `console.log("app")` {
		t.Fatalf("bundle body = %q", b)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("bundle Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed bundle Cache-Control = %q, want immutable", cc)
	}

	if rec := get(t, h, http.MethodGet, "/app/_expo/static/js/web/missing.js"); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404", rec.Code)
	}
}

func TestWebAppDoesNotLeaveItsDirectory(t *testing.T) {
	dir := webBuild(t)
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(Deps{Ready: ready, WebDir: dir})

	for _, path := range []string{"/app/../secret.txt", "/app/%2e%2e/secret.txt", "/app/..%2fsecret.txt"} {
		rec := get(t, h, http.MethodGet, path)
		if strings.Contains(rec.Body.String(), "do not serve") {
			t.Errorf("GET %s served a file outside the web build", path)
		}
	}
}

func TestNoWebBuildMeansNoApp(t *testing.T) {
	h := NewRouter(Deps{Ready: ready})
	if rec := get(t, h, http.MethodGet, "/app/"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /app/ without a build = %d, want 404", rec.Code)
	}
}
