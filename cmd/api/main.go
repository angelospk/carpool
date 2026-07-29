// Command api is the carpool backend: HTTP API, WebSocket hub and background
// workers in one binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/api"
	"carpool/internal/auth"
	"carpool/internal/clock"
	"carpool/internal/config"
	"carpool/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}

	// SIGTERM (and Ctrl-C) cancel this context, which unwinds the server and
	// every worker goroutine hanging off it.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrations run at boot behind an advisory lock, so several instances
	// starting together converge on one schema.
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	slog.Info("schema is up to date")

	if cfg.RegionFixture != "" {
		if err := store.SeedRegionsFromFile(ctx, pool, cfg.RegionFixture, cfg.FuelPriceEUR); err != nil {
			return err
		}
		slog.Info("regions seeded", "fixture", cfg.RegionFixture)
	}

	// The SMS provider is a deployment choice. Only DEV_MODE may select the
	// console sender, which prints codes instead of sending them.
	var sms auth.SMSSender
	if cfg.DevMode {
		slog.Warn("DEV_MODE is enabled: OTP codes are printed to the console and not sent by SMS")
		sms = auth.ConsoleSender{}
	} else {
		// No production SMS provider is wired yet; refusing to boot is safer
		// than silently starting an instance whose sign-up cannot work.
		return errors.New("no SMS provider is configured: set DEV_MODE=true for local development")
	}

	clk := clock.Real{}
	issuer := auth.NewTokenIssuer(cfg.JWTSecret, clk)

	srv := api.NewServer(cfg.HTTPAddr, api.NewRouter(api.Deps{
		Ready:    pool.Ping,
		Pool:     pool,
		Clock:    clk,
		Issuer:   issuer,
		OTP:      auth.NewOTPService(pool, clk, sms),
		Sessions: auth.NewSessionService(pool, clk, issuer),
	}))

	slog.Info("listening", "addr", cfg.HTTPAddr)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	slog.Info("stopped cleanly")
	return nil
}
