package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestA500LeavesALogLine(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := chi.NewRouter()
	r.Use(logServerErrors)
	r.Get("/things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusInternalServerError, "internal", "no")
	})
	r.Get("/fine", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for _, p := range []string{"/things/42", "/fine"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	out := buf.String()
	if !strings.Contains(out, "status=500") || !strings.Contains(out, "route=/things/{id}") {
		t.Fatalf("no line for the 500: %q", out)
	}
	if strings.Count(out, "server error") != 1 {
		t.Fatalf("logged something that was not a 5xx: %q", out)
	}
}
