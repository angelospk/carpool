// Command api is the carpool backend: HTTP API, WebSocket hub and background
// workers in one binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/api"
	"carpool/internal/asks"
	"carpool/internal/auth"
	"carpool/internal/clock"
	"carpool/internal/config"
	"carpool/internal/dispatch"
	"carpool/internal/geo"
	"carpool/internal/jobs"
	"carpool/internal/outbox"
	"carpool/internal/push"
	"carpool/internal/standing"
	"carpool/internal/store"
	"carpool/internal/ws"
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
	router := geo.NewOSRM(cfg.OSRMURL, nil)

	askSvc := asks.NewService(pool, clk, nil).WithRouter(router)

	athens, err := time.LoadLocation(schedulingZone)
	if err != nil {
		return fmt.Errorf("loading %s: %w", schedulingZone, err)
	}

	// Background workers. They share the process context, so SIGTERM unwinds
	// them alongside the HTTP server.
	var workers sync.WaitGroup
	dispatcher := outbox.NewDispatcher(pool, clk, dispatch.NewRouter(pool, clk, push.NewExpo("", nil),
		func(ctx context.Context, requestID uuid.UUID) error {
			_, err := askSvc.AskNext(ctx, requestID)
			if errors.Is(err, asks.ErrNoCandidates) {
				// Nobody left to ask. The request stays open and is retried when
				// a new occurrence is materialized.
				return nil
			}
			return err
		}))

	start(ctx, &workers, "outbox dispatcher", outboxInterval, func(ctx context.Context) error {
		_, err := dispatcher.DispatchOnce(ctx)
		return err
	})
	start(ctx, &workers, "ask sweeper", sweepInterval, func(ctx context.Context) error {
		_, err := askSvc.SweepExpired(ctx)
		return err
	})
	materializer := jobs.NewMaterializer(pool, clk, athens)
	start(ctx, &workers, "occurrence materializer", materializeInterval, materializer.Run)

	srv := api.NewServer(cfg.HTTPAddr, api.NewRouter(api.Deps{
		Ready:    pool.Ping,
		Pool:     pool,
		Clock:    clk,
		Issuer:   issuer,
		OTP:      auth.NewOTPService(pool, clk, sms),
		Sessions: auth.NewSessionService(pool, clk, issuer),
		Asks:     askSvc,
		Standing: standing.NewService(pool, clk),

		Router:          router,
		Materializer:    materializer,
		OSRMDataVersion: cfg.OSRMDataVersion,
		Hub:             ws.NewHub(),
	}))

	slog.Info("listening", "addr", cfg.HTTPAddr)
	runErr := srv.Run(ctx)

	// The server has stopped, so the context is done and the workers are
	// unwinding; wait for them before the pool closes under them.
	workers.Wait()

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}
	slog.Info("stopped cleanly")
	return nil
}

// Worker cadences. Each loop also runs immediately on start, so a restart does
// not wait a full interval before catching up.
const (
	schedulingZone      = "Europe/Athens"
	outboxInterval      = 5 * time.Second
	sweepInterval       = 30 * time.Second
	materializeInterval = 15 * time.Minute
)

// start runs fn on a ticker until ctx is cancelled. A failing tick is logged
// and retried on the next one: a background worker must not take the process
// down.
func start(ctx context.Context, wg *sync.WaitGroup, name string, every time.Duration, fn func(context.Context) error) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(every)
		defer ticker.Stop()

		for {
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("background worker failed", "worker", name, "error", err)
			}
			select {
			case <-ctx.Done():
				slog.Info("background worker stopped", "worker", name)
				return
			case <-ticker.C:
			}
		}
	}()
}
