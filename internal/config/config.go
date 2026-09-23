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
	// PetrolPriceEUR and DieselPriceEUR are the national figures the app prices
	// contributions with. Two, not one: they differ by ten cents or more at the
	// pump, and the consumption figure they multiply is already fuel-specific.
	//
	// National, not per prefecture. The Παρατηρητήριο Τιμών publishes by νομό
	// and nothing here fetches it, so seventy-five rows would be seventy-five
	// copies of the same number wearing a local badge. A prefecture gets its own
	// figure when somebody goes and looks one up; see store.FuelPriceAt.
	PetrolPriceEUR float64
	DieselPriceEUR float64
	// FuelPriceSource says where those numbers came from, in words. A price
	// nobody can trace is a price nobody can correct.
	FuelPriceSource string
	// OSRMDataVersion is stamped onto stored routes so a map-data swap can
	// trigger re-snapping.
	OSRMDataVersion string

	// SMS describes the gateway one-time codes are sent through. Empty means
	// none is configured, which is only tolerable under DevMode.
	SMSURL      string
	SMSToken    string
	SMSSender   string
	SMSTemplate string

	// InviteOnly makes a new account require a code somebody handed out. It
	// does not affect anyone who already has an account.
	InviteOnly bool

	// PublicBaseURL is the address route links are built on, and InstallURL
	// is where their page sends somebody without the app. Both optional.
	PublicBaseURL string
	InstallURL    string

	// DevMode enables developer conveniences that must never be reachable in
	// production — most importantly printing the OTP to the console instead of
	// sending an SMS. It defaults to false so production is the safe default.
	DevMode bool
}

// Load reads configuration via lookup, applying defaults and validating that
// required variables are present.
func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		OSRMURL:    optional(lookup, "OSRM_URL", "http://localhost:5000"),
		HTTPAddr:   optional(lookup, "HTTP_ADDR", ":8080"),
		DevMode:    optional(lookup, "DEV_MODE", "false") == "true",
		InviteOnly: optional(lookup, "INVITE_ONLY", "false") == "true",

		SMSURL:    optional(lookup, "SMS_URL", ""),
		SMSToken:  optional(lookup, "SMS_TOKEN", ""),
		SMSSender: optional(lookup, "SMS_SENDER", ""),
		// The default body suits the majority of gateways, which take a JSON
		// POST of sender, recipient and text. One that names its fields
		// differently is a change of environment variable, not of code.
		SMSTemplate: optional(lookup, "SMS_TEMPLATE",
			`{"sender":"{{sender}}","recipient":"{{phone}}","text":"{{message}}"}`),

		PublicBaseURL: optional(lookup, "PUBLIC_BASE_URL", ""),
		InstallURL:    optional(lookup, "INSTALL_URL", ""),

		RegionFixture:   optional(lookup, "REGION_FIXTURE", ""),
		FuelPriceSource: optional(lookup, "FUEL_PRICE_SOURCE", "configured at deploy"),
		OSRMDataVersion: optional(lookup, "OSRM_DATA_VERSION", "unknown"),
	}

	var err error
	cfg.PetrolPriceEUR, err = parseFloat(optional(lookup, "PETROL_PRICE_EUR", "1.859"), "PETROL_PRICE_EUR")
	if err != nil {
		return Config{}, err
	}
	cfg.DieselPriceEUR, err = parseFloat(optional(lookup, "DIESEL_PRICE_EUR", "1.729"), "DIESEL_PRICE_EUR")
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
