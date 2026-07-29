package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivezReportsAliveEvenWhenTheDatabaseIsDown(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error {
		return errors.New("db is unreachable")
	}})

	rec := do(r, http.MethodGet, "/v1/livez")

	// Liveness must not depend on the database, otherwise an orchestrator would
	// restart a process that is perfectly healthy but waiting on Postgres.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyzReportsUnavailableWhenTheDatabaseIsDown(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error {
		return errors.New("db is unreachable")
	}})

	rec := do(r, http.MethodGet, "/v1/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestReadyzReportsReadyWhenDependenciesAreHealthy(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error { return nil }})

	rec := do(r, http.MethodGet, "/v1/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteReturnsTheErrorEnvelope(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error { return nil }})

	rec := do(r, http.MethodGet, "/v1/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (body %q)", err, rec.Body.String())
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want not_found", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Error("error.message should not be empty")
	}
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}
