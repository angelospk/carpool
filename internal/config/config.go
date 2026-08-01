// Package config loads runtime configuration from the environment.
//
// Config is env-only (12-factor) per ARCHITECTURE.md. Load takes a lookup
// function rather than reading os.Getenv directly so it is testable without
// mutating global process state.
package config

import (
	"fmt"
	"strconv"
)

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

	// RegionFixture, when set, is a GeoJSON file seeded into regions at boot.
	RegionFixture string
	// FuelPriceEUR is the price seeded regions start with; admins update it later.
	FuelPriceEUR float64
	// OSRMDataVersion is stamped onto stored routes so a map-data swap can
	// trigger re-snapping.
	OSRMDataVersion string

	// SMS describes the gateway one-time codes are sent through. Empty means
	// none is configured, which is only tolerable under DevMode.
	SMSURL      string
	SMSToken    string
	SMSSender   string
	SMSTemplate string

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

		SMSURL:    optional(lookup, "SMS_URL", ""),
		SMSToken:  optional(lookup, "SMS_TOKEN", ""),
		SMSSender: optional(lookup, "SMS_SENDER", ""),
		// The default body suits the majority of gateways, which take a JSON
		// POST of sender, recipient and text. One that names its fields
		// differently is a change of environment variable, not of code.
		SMSTemplate: optional(lookup, "SMS_TEMPLATE",
			`{"sender":"{{sender}}","recipient":"{{phone}}","text":"{{message}}"}`),

		RegionFixture:   optional(lookup, "REGION_FIXTURE", ""),
		OSRMDataVersion: optional(lookup, "OSRM_DATA_VERSION", "unknown"),
	}

	var err error
	cfg.FuelPriceEUR, err = parseFloat(optional(lookup, "FUEL_PRICE_EUR", "1.800"), "FUEL_PRICE_EUR")
	if err != nil {
		return Config{}, err
	}

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

func parseFloat(v, key string) (float64, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number, got %q", key, v)
	}
	return f, nil
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
