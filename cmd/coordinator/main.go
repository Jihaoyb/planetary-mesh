package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"planetary-mesh/internal/coordinator"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := getEnv("COORDINATOR_ADDR", ":8080")
	databaseURL := os.Getenv("COORDINATOR_DATABASE_URL")

	var registry coordinator.NodeStore
	var jobs coordinator.JobStorage
	var postgresStore *coordinator.PostgresStore

	if databaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		store, err := coordinator.OpenPostgresStoreWithRetry(ctx, databaseURL)
		if err != nil {
			logger.Error("postgres initialization failed", "err", err)
			os.Exit(1)
		}
		postgresStore = store
		defer func() {
			if err := postgresStore.Close(); err != nil {
				logger.Warn("postgres close failed", "err", err)
			}
		}()

		registry = store.Nodes()
		jobs = store.Jobs()
		recovered, err := jobs.FailRunningJobs(coordinator.RestartRecoveryError)
		if err != nil {
			logger.Error("recover running jobs failed", "err", err)
			os.Exit(1)
		}
		logger.Info("postgres storage initialized", "recovered_running_jobs", recovered)
	} else {
		registry = coordinator.NewNodeRegistry()
		jobs = coordinator.NewJobStore()
		logger.Info("in-memory storage initialized")
	}

	srv := coordinator.NewServerWithConfig(registry, jobs, http.DefaultClient, coordinator.DefaultDispatchConfig(), logger)

	stopCh := make(chan struct{})
	coordinator.StartHealthChecker(registry, stopCh)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		close(stopCh)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown error", "err", err)
		}
	}()

	logger.Info("coordinator starting", "addr", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
	logger.Info("coordinator stopped")
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
