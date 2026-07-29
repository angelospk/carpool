package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	env := map[string]string{
		"OSRM_URL":   "http://localhost:5000",
		"JWT_SECRET": "0123456789abcdef0123456789abcdef",
	}

	_, err := Load(lookup(env))

	if err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error should name the missing variable, got: %v", err)
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://localhost/carpool",
		"JWT_SECRET":   "tooshort",
	}

	_, err := Load(lookup(env))

	if err == nil {
		t.Fatal("expected an error for a JWT secret below the minimum length, got nil")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("error should name JWT_SECRET, got: %v", err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://localhost/carpool",
		"JWT_SECRET":   strings.Repeat("a", 32),
	}

	cfg, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OSRMURL != "http://localhost:5000" {
		t.Errorf("OSRMURL default = %q, want http://localhost:5000", cfg.OSRMURL)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DevMode {
		t.Error("DevMode should default to false so production is the safe default")
	}
}

func TestLoadReadsDevMode(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://localhost/carpool",
		"JWT_SECRET":   strings.Repeat("a", 32),
		"DEV_MODE":     "true",
	}

	cfg, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.DevMode {
		t.Error("DEV_MODE=true should enable DevMode")
	}
}

func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}
