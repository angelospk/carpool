// Package config loads runtime configuration from the environment.
//
// Config is env-only (12-factor) per ARCHITECTURE.md. Load takes a lookup
// function rather than reading os.Getenv directly so it is testable without
// mutating global process state.
package config

import "fmt"

// minJWTSecretLen is 32 bytes — a 256-bit key, matching HS256's block size.
const minJWTSecretLen = 32

// LookupFunc matches the signature of os.LookupEnv.
type LookupFunc func(key string) (string, bool)

// Config is the fully resolved runtime configuration.
type Config struct {
	DatabaseURL string
	OSRMURL     string
	HTTPAddr    string
	JWTSecret   string

	// DevMode enables developer conveniences that must never be reachable in
	// production — most importantly printing the OTP to the console instead of
	// sending an SMS. It defaults to false so production is the safe default.
	DevMode bool
}

// Load reads configuration via lookup, applying defaults and validating that
// required variables are present.
func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		OSRMURL:  optional(lookup, "OSRM_URL", "http://localhost:5000"),
		HTTPAddr: optional(lookup, "HTTP_ADDR", ":8080"),
		DevMode:  optional(lookup, "DEV_MODE", "false") == "true",
	}

	var err error
	if cfg.DatabaseURL, err = required(lookup, "DATABASE_URL"); err != nil {
		return Config{}, err
	}
	if cfg.JWTSecret, err = required(lookup, "JWT_SECRET"); err != nil {
		return Config{}, err
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return Config{}, fmt.Errorf("config: JWT_SECRET must be at least %d bytes, got %d", minJWTSecretLen, len(cfg.JWTSecret))
	}

	return cfg, nil
}

func required(lookup LookupFunc, key string) (string, error) {
	v, ok := lookup(key)
	if !ok || v == "" {
		return "", fmt.Errorf("config: %s is required", key)
	}
	return v, nil
}

func optional(lookup LookupFunc, key, fallback string) string {
	if v, ok := lookup(key); ok && v != "" {
		return v
	}
	return fallback
}
