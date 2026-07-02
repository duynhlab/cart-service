package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/duynhlab/cart-service/config"
	migrations "github.com/duynhlab/cart-service/db/migrations"
	seed "github.com/duynhlab/cart-service/db/seed"
	database "github.com/duynhlab/cart-service/internal/core"
	"github.com/duynhlab/cart-service/internal/core/repository"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
	v1 "github.com/duynhlab/cart-service/internal/web/v1"
	"github.com/duynhlab/cart-service/middleware"
	"github.com/duynhlab/pkg/authmw"
	"github.com/duynhlab/pkg/logger/clog"
	"github.com/duynhlab/pkg/migratex"
	"github.com/duynhlab/pkg/obsx"
)

func main() {
	cfg := config.Load()

	clog.Setup(cfg.Logging.Level)

	// Subcommands (`migrate`, `seed`) run an embedded SQL set and exit; no args
	// serves the app.
	if len(os.Args) > 1 {
		if runSubcommand(os.Args[1], cfg) {
			return
		}
	}

	if err := cfg.Validate(); err != nil {
		panic("Configuration validation failed: " + err.Error())
	}

	slog.Info("Service starting",
		"service", cfg.Service.Name,
		"version", cfg.Service.Version,
		"env", cfg.Service.Env,
		"port", cfg.Service.Port,
	)

	tp := initTracing(cfg)

	shutdownMetrics := initMetrics(cfg)
	defer func() {
		if shutdownMetrics != nil {
			if err := shutdownMetrics(context.Background()); err != nil {
				slog.Error("Metrics provider shutdown error", "error", err)
			}
		}
	}()

	stopProfiling := initProfiling(cfg)
	defer func() {
		if stopProfiling != nil {
			if err := stopProfiling(context.Background()); err != nil {
				slog.Error("Profiling shutdown error", "error", err)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return
	}
	defer pool.Close()
	slog.Info("Database connection pool established")

	cartRepo := repository.NewPostgresCartRepository(pool)
	cartService := logicv1.NewCartService(cartRepo)
	cartHandler := v1.NewCartHandler(cartService)

	// Local JWT verification via JWKS — the only credential path, no fallback.
	verifier, err := authmw.NewVerifier(cfg.JWKSURL, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		slog.Error("JWT verifier init failed", "error", err)
		return
	}

	var isShuttingDown atomic.Bool
	srv := setupServer(cfg, verifier, cartHandler, &isShuttingDown)
	runGracefulShutdown(cfg, srv, tp, pool, &isShuttingDown)
}

// runSubcommand handles the `migrate` and `seed` subcommands. It returns true
// when a subcommand was recognised and executed (the caller then exits), or
// false to fall through to serving the app.
//
// `migrate` applies the versioned schema migrations and runs in every
// environment (init container, direct DB host). `seed` applies DEV-ONLY demo
// data and is invoked explicitly — never by `migrate` or the serve path — so
// production databases are never seeded.
func runSubcommand(cmd string, cfg *config.Config) bool {
	switch cmd {
	case "migrate":
		if err := migratex.Run(migrations.FS, "sql", cfg.Database.BuildDSN()); err != nil {
			slog.Error("Schema migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("Schema migrations applied")
		return true
	case "seed":
		// Demo data is DEV-ONLY; refuse to seed a production database.
		if cfg.IsProduction() {
			slog.Error("seed refused in production — demo data is dev-only")
			os.Exit(1)
		}
		if err := applySeed(cfg); err != nil {
			slog.Error("Demo seed failed", "error", err)
			os.Exit(1)
		}
		slog.Info("Demo seed data applied")
		return true
	default:
		return false
	}
}

// applySeed executes the embedded dev-only seed SQL directly against the
// database. It does NOT use golang-migrate: seeds are idempotent (ON CONFLICT)
// and must not share the schema_migrations version table with the schema
// migrations. Simple query protocol lets each multi-statement seed file run in
// one Exec.
func applySeed(cfg *config.Config) error {
	ctx := context.Background()

	poolCfg, err := pgxpool.ParseConfig(cfg.Database.BuildDSN())
	if err != nil {
		return fmt.Errorf("parse seed DSN: %w", err)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect for seed: %w", err)
	}
	defer pool.Close()

	entries, err := fs.ReadDir(seed.FS, "sql")
	if err != nil {
		return fmt.Errorf("read seed dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		b, readErr := fs.ReadFile(seed.FS, "sql/"+name)
		if readErr != nil {
			return fmt.Errorf("read seed %s: %w", name, readErr)
		}
		if _, execErr := pool.Exec(ctx, string(b)); execErr != nil {
			return fmt.Errorf("apply seed %s: %w", name, execErr)
		}
	}
	return nil
}

func initTracing(cfg *config.Config) interface{ Shutdown(context.Context) error } {
	if !cfg.Tracing.Enabled {
		slog.Info("Tracing disabled (TRACING_ENABLED=false)")
		return nil
	}
	tp, err := middleware.InitTracing(cfg)
	if err != nil {
		slog.Warn("Failed to initialize tracing", "error", err)
		return nil
	}
	slog.Info("Tracing initialized",
		"endpoint", cfg.Tracing.Endpoint,
		"sample_rate", cfg.Tracing.SampleRate,
	)
	return tp
}

func initMetrics(cfg *config.Config) func(context.Context) error {
	if !cfg.Metrics.Enabled {
		slog.Info("Metrics disabled (METRICS_ENABLED=false)")
		return nil
	}
	shutdown, err := obsx.SetupMetrics()
	if err != nil {
		slog.Warn("Failed to initialize metrics", "error", err)
		return nil
	}
	slog.Info("Metrics initialized (OTel MeterProvider → Prometheus default registry)")
	return shutdown
}

func initProfiling(cfg *config.Config) func(context.Context) error {
	if !cfg.Profiling.Enabled {
		slog.Info("Profiling disabled (PROFILING_ENABLED=false)")
		return nil
	}
	stop, err := obsx.SetupProfiling()
	if err != nil {
		slog.Warn("Failed to initialize profiling", "error", err)
		return nil
	}
	slog.Info("Profiling initialized", "endpoint", cfg.Profiling.Endpoint)
	return stop
}

func setupServer(cfg *config.Config, verifier *authmw.Verifier, cartHandler *v1.CartHandler, isShuttingDown *atomic.Bool) *http.Server {
	r := gin.Default()

	r.Use(middleware.TracingMiddleware())
	r.Use(middleware.LoggingMiddleware())
	r.Use(middleware.PrometheusMiddleware())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/ready", func(c *gin.Context) {
		if isShuttingDown.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "shutting_down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Cart v1 routes — all private (JWT required). Variant A edge naming.
	privateCart := r.Group("/cart/v1/private")
	privateCart.Use(authmw.MiddlewareJWT(verifier))
	{
		privateCart.GET("/cart", cartHandler.GetCart)
		privateCart.POST("/cart", cartHandler.AddToCart)
		privateCart.DELETE("/cart", cartHandler.ClearCart)
		privateCart.GET("/cart/count", cartHandler.GetCartCount)
		privateCart.PATCH("/cart/items/:itemId", cartHandler.UpdateCartItem)
		privateCart.DELETE("/cart/items/:itemId", cartHandler.RemoveCartItem)
	}

	// Internal routes — service-to-service only (e.g. the order-fulfillment saga
	// clears the cart after checkout). No JWT: reachable by in-cluster DNS only,
	// never on the Kong gateway, fenced by NetworkPolicy (order→cart).
	internalCart := r.Group("/cart/v1/internal")
	{
		internalCart.DELETE("/cart/:userId", cartHandler.ClearCartByUserID)
	}

	return &http.Server{
		Addr:              ":" + cfg.Service.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func runGracefulShutdown(
	cfg *config.Config,
	srv *http.Server,
	tp interface{ Shutdown(context.Context) error },
	pool interface{ Close() },
	isShuttingDown *atomic.Bool,
) {
	go func() {
		slog.Info("Starting cart service", "port", cfg.Service.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to start server", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	<-ctx.Done()
	slog.Info("Shutdown signal received")

	isShuttingDown.Store(true)
	drainDelay := cfg.GetReadinessDrainDelayDuration()
	if drainDelay > 0 {
		slog.Info("Readiness drain delay started", "delay", drainDelay)
		time.Sleep(drainDelay)
	}

	shutdownTimeout := cfg.GetShutdownTimeoutDuration()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	slog.Info("Shutting down server...", "timeout", shutdownTimeout)

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	} else {
		slog.Info("HTTP server shutdown complete")
	}

	pool.Close()
	slog.Info("Database pool closed")

	if tp != nil {
		if err := tp.Shutdown(shutdownCtx); err != nil {
			slog.Error("Tracer shutdown error", "error", err)
		} else {
			slog.Info("Tracer shutdown complete")
		}
	}

	slog.Info("Graceful shutdown complete")
}
