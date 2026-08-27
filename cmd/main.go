package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/niksmi-lab/unique-clicks-service/internal/config"
	"github.com/niksmi-lab/unique-clicks-service/internal/handlers"
	"github.com/niksmi-lab/unique-clicks-service/internal/httpmw"
	"github.com/niksmi-lab/unique-clicks-service/internal/metrics"
	"github.com/niksmi-lab/unique-clicks-service/internal/service"
	"github.com/niksmi-lab/unique-clicks-service/internal/storage/postgres"
	"github.com/niksmi-lab/unique-clicks-service/internal/worker"

	"github.com/jackc/pgx/v5/pgxpool"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Environment)
	slog.SetDefault(logger)

	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database config: %w", err)
	}
	poolConfig.MaxConns = cfg.MaxDBConnections

	connectCtx, cancel := context.WithTimeout(appCtx, cfg.RequestTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(connectCtx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	telemetry := metrics.New(pool, version)
	store := postgres.New(pool, telemetry)
	analytics := service.NewAnalyticsService(store, telemetry)

	mux := http.NewServeMux()
	handlers.NewAnalyticsHandler(analytics, logger, cfg.RequestTimeout, 1000).RegisterRoutes(mux)
	handlers.NewHealthHandler(store, time.Second).RegisterRoutes(mux)
	mux.Handle("GET /metrics", telemetry.Handler())

	var httpHandler http.Handler = mux
	httpHandler = httpmw.SecurityHeaders(httpHandler)
	httpHandler = httpmw.Recover(logger, httpHandler)
	httpHandler = httpmw.Metrics(telemetry, httpHandler)
	httpHandler = httpmw.AccessLog(logger, httpHandler)
	httpHandler = httpmw.RequestID(httpHandler)

	server := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.RequestTimeout + 2*time.Second,
		WriteTimeout:      cfg.RequestTimeout + 2*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	cleaner := worker.NewCleaner(
		analytics,
		logger,
		cfg.CleanupInterval,
		cfg.RequestTimeout,
		cfg.RetentionDays,
		telemetry,
	)
	go cleaner.Run(appCtx)

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server started", "address", cfg.ServerAddr, "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-appCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("http server stopped")
	return nil
}

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo
	if environment == "development" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
