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

	registry := coordinator.NewNodeRegistry()
	jobs := coordinator.NewJobStore()
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
