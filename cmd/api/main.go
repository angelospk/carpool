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

	"carpool/internal/account"
	"carpool/internal/api"
	"carpool/internal/asks"
	"carpool/internal/auth"
	"carpool/internal/bookings"
	"carpool/internal/clock"
	"carpool/internal/config"
	"carpool/internal/dispatch"
	"carpool/internal/drivers"
	"carpool/internal/drives"
	"carpool/internal/geo"
	"carpool/internal/groups"
	"carpool/internal/invites"
	"carpool/internal/jobs"
	"carpool/internal/live"
	"carpool/internal/moderation"
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

	// Boundaries are load-bearing rather than optional decoration: without them
	// RegionAt resolves nothing, every trip is refused as "outside_country" and
	// nothing can be priced — while /readyz cheerfully says the service is up.
	// Failing at boot is the only place that is honest.
	if cfg.RegionFixture != "" {
		if err := store.SeedRegionsFromFile(ctx, pool, cfg.RegionFixture); err != nil {
			return err
		}
		slog.Info("regions seeded", "fixture", cfg.RegionFixture)
	}
	var regions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM regions`).Scan(&regions); err != nil {
		return fmt.Errorf("counting regions: %w", err)
	}
	if regions == 0 {
		return errors.New("no regions: set REGION_FIXTURE to fixtures/regions/greece.geojson")
	}

	// Fuel prices are seeded unconditionally, unlike the boundaries: without
	// them nothing can suggest a contribution to anybody, so a deployment that
	// forgot would look healthy and quote nothing.
	if err := store.SeedFuelPrices(ctx, pool, store.FuelPrices{
		PetrolEUR: cfg.PetrolPriceEUR,
		DieselEUR: cfg.DieselPriceEUR,
		Source:    cfg.FuelPriceSource,
	}); err != nil {
		return err
	}
	slog.Info("fuel prices seeded",
		"petrol", cfg.PetrolPriceEUR, "diesel", cfg.DieselPriceEUR, "source", cfg.FuelPriceSource)

	// The SMS provider is a deployment choice. Only DEV_MODE may select the
	// console sender, which prints codes instead of sending them.
	var sms auth.SMSSender
	switch {
	case cfg.SMSURL != "":
		gateway, err := auth.NewHTTPSMS(auth.HTTPSMSConfig{
			URL:      cfg.SMSURL,
			Token:    cfg.SMSToken,
			Sender:   cfg.SMSSender,
			Template: cfg.SMSTemplate,
		})
		if err != nil {
			return fmt.Errorf("sms gateway: %w", err)
		}
		sms = gateway
		slog.Info("sms gateway configured", "url", cfg.SMSURL, "sender", cfg.SMSSender)
	case cfg.DevMode:
		slog.Warn("DEV_MODE is enabled: OTP codes are printed to the console and not sent by SMS")
		sms = auth.ConsoleSender{}
	default:
		// Refusing to boot is safer than silently starting an instance whose
		// sign-up cannot work.
		return errors.New("no SMS provider is configured: set SMS_URL, or DEV_MODE=true for local development")
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
				// Nobody to ask right now. The request stays open, and a trip
				// becoming available on its corridor wakes it: publishing,
				// materializing and giving a seat back all queue a wake_waiting
				// that puts it back through here. Until that existed this was a
				// dead end, and the request expired next to a car with room.
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
	// The other end of the same idea, and it was missing: an offer expired, a
	// request never did. Both filters read the window rather than the status
	// precisely because of that, and both said in a comment that a sweeper
	// would come along. This is it.
	requestSweeper := jobs.NewRequestSweeper(pool, clk)
	start(ctx, &workers, "request sweeper", sweepInterval, func(ctx context.Context) error {
		_, err := requestSweeper.Run(ctx)
		return err
	})
	materializer := jobs.NewMaterializer(pool, clk, athens)
	start(ctx, &workers, "occurrence materializer", materializeInterval, materializer.Run)
	start(ctx, &workers, "retention janitor", retentionInterval, jobs.NewJanitor(pool, clk).Run)
	// The safety net under the wakes. Publishing a trip, materializing one,
	// giving a seat back and ending an offer all wake the requests waiting on
	// that corridor within seconds. This catches what has no event: a driver
	// turning notifications back on, widening their radius, coming off a
	// suspension — and whatever the wake's shortlist left behind.
	// Tells drivers who asked for it that somebody on their road is waiting,
	// shortly before a departure. A minute is fine-grained enough for a
	// quarter-hour lead and cheap: it reads departures due in that quarter.
	driveSvc := drives.NewService(pool, clk)
	start(ctx, &workers, "drive nudger", nudgeInterval, func(ctx context.Context) error {
		_, err := driveSvc.Nudge(ctx)
		return err
	})
	reconciler := jobs.NewReconciler(pool, clk)
	start(ctx, &workers, "match reconciler", jobs.ReconcileInterval, func(ctx context.Context) error {
		_, err := reconciler.Run(ctx)
		return err
	})

	sessions := auth.NewSessionService(pool, clk, issuer)

	srv := api.NewServer(cfg.HTTPAddr, api.NewRouter(api.Deps{
		Ready:    pool.Ping,
		Pool:     pool,
		Clock:    clk,
		Location: athens,
		Issuer:   issuer,
		OTP:      auth.NewOTPService(pool, clk, sms),
		Signup:   auth.NewSignupService(pool, clk),
		Sessions: sessions,
		Asks:     askSvc,
		Standing: standing.NewService(pool, clk),
		Bookings: bookings.NewService(pool, clk),
		Invites:  invites.NewService(pool, clk, cfg.InviteOnly),
		Groups:   groups.NewService(pool, clk),
		Live:     live.NewService(pool, clk),
		Drives:   driveSvc,
		DevMode:  cfg.DevMode,
		// Generous for a household or an office behind one address, far too
		// few to be worth pumping SMS through.
		OTPPerIPPerHour: 20,

		PublicBaseURL: cfg.PublicBaseURL,
		InstallURL:    cfg.InstallURL,
		Account:       account.NewService(pool, clk),
		Drivers:       drivers.NewService(pool, clk),
		Moderation:    moderation.NewService(pool, clk),
		// Keyed on the JWT secret: the phone-change codes are stored as an HMAC
		// rather than a bare hash, and this is the key that makes a leaked
		// table useless without it.
		PhoneChange: auth.NewPhoneChangeService(pool, clk, sms, sessions, []byte(cfg.JWTSecret)),

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
	nudgeInterval       = time.Minute
	// Retention periods are measured in months, so an hour either way is
	// nothing. What the cadence buys is that a restart does not leave the
	// backlog for a day, since every loop also runs once on start.
	retentionInterval = time.Hour
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
