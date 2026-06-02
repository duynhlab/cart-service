package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/duynhlab/cart-service/config"
	database "github.com/duynhlab/cart-service/internal/core"
	"github.com/duynhlab/cart-service/internal/core/repository"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
	v1 "github.com/duynhlab/cart-service/internal/web/v1"
	"github.com/duynhlab/cart-service/middleware"
	"github.com/duynhlab/pkg/authmw"
	"github.com/duynhlab/pkg/grpcx"
	"github.com/duynhlab/pkg/logger/clog"
	"github.com/duynhlab/pkg/obsx"
	authv1 "github.com/duynhlab/pkg/proto/auth/v1"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		panic("Configuration validation failed: " + err.Error())
	}

	clog.Setup(cfg.Logging.Level)

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

	initProfiling(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return
	}
	defer pool.Close()
	slog.Info("Database connection pool established")

	cartRepo := repository.NewPostgresCartRepository(pool)
	cartService := logicv1.NewCartService(cartRepo)
	cartHandler := v1.NewCartHandler(cartService)

	// Validate tokens against auth over gRPC (shared fail-closed authmw).
	authConn, err := grpcx.Dial(cfg.AuthGRPCAddr)
	if err != nil {
		slog.Error("Failed to dial auth gRPC", "addr", cfg.AuthGRPCAddr, "error", err)
		return
	}
	defer func() { _ = authConn.Close() }()
	authClient := authv1.NewAuthServiceClient(authConn)
	slog.Info("Auth gRPC client initialized", "auth_grpc_addr", cfg.AuthGRPCAddr)

	var isShuttingDown atomic.Bool
	srv := setupServer(cfg, authClient, cartHandler, &isShuttingDown)
	runGracefulShutdown(cfg, srv, tp, pool, &isShuttingDown)
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

func initProfiling(cfg *config.Config) {
	if !cfg.Profiling.Enabled {
		slog.Info("Profiling disabled (PROFILING_ENABLED=false)")
		return
	}
	if err := middleware.InitProfiling(); err != nil {
		slog.Warn("Failed to initialize profiling", "error", err)
		return
	}
	slog.Info("Profiling initialized", "endpoint", cfg.Profiling.Endpoint)
}

func setupServer(cfg *config.Config, authClient authv1.AuthServiceClient, cartHandler *v1.CartHandler, isShuttingDown *atomic.Bool) *http.Server {
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
	privateCart.Use(authmw.Middleware(authClient))
	{
		privateCart.GET("/cart", cartHandler.GetCart)
		privateCart.POST("/cart", cartHandler.AddToCart)
		privateCart.DELETE("/cart", cartHandler.ClearCart)
		privateCart.GET("/cart/count", cartHandler.GetCartCount)
		privateCart.PATCH("/cart/items/:itemId", cartHandler.UpdateCartItem)
		privateCart.DELETE("/cart/items/:itemId", cartHandler.RemoveCartItem)
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

	middleware.StopProfiling()
	slog.Info("Graceful shutdown complete")
}
