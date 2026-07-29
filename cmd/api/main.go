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

	if cfg.DevMode {
		slog.Warn("DEV_MODE is enabled: OTP codes are printed to the console and not sent by SMS")
	}

	srv := api.NewServer(cfg.HTTPAddr, api.NewRouter(api.Deps{
		Ready: pool.Ping,
	}))

	slog.Info("listening", "addr", cfg.HTTPAddr)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	slog.Info("stopped cleanly")
	return nil
}
