package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	// Coordinator listen address, default :8080.
	addr := getEnv("COORDINATOR_ADDR", ":8080")
	dispatchCfg := loadDispatchConfig()

	// In-memory node registry.
	registry := NewNodeRegistry()
	jobStore := NewJobStore()
	srv := &server{
		registry:    registry,
		jobs:        jobStore,
		httpClient:  &http.Client{Timeout: dispatchCfg.timeout},
		dispatchCfg: dispatchCfg,
	}

	// Start background health checker for nodes.
	stopHealth := startHealthChecker(registry)

	// HTTP routing.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/register", srv.handleRegister)
	mux.HandleFunc("/nodes", srv.handleListNodes)
	mux.HandleFunc("/jobs/", srv.handleJobByID)
	mux.HandleFunc("/jobs", srv.handleJobs)
	mux.HandleFunc("/metrics", srv.handleMetrics)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Handle shutdown signals.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[coordinator] starting on %s\n", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[coordinator] server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[coordinator] shutdown signal received")

	// Stop background health ticker.
	stopHealth()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("[coordinator] graceful shutdown failed: %v", err)
	}
}

// getEnv reads an environment variable, or returns a default if not set.
func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

// loadDispatchConfig reads dispatch tuning values from env with safe defaults.
func loadDispatchConfig() dispatchConfig {
	timeout := parseDurationEnv("DISPATCH_TIMEOUT", 5*time.Second)
	backoff := parseDurationEnv("DISPATCH_BACKOFF", 200*time.Millisecond)
	maxAttempts := parseIntEnv("DISPATCH_MAX_ATTEMPTS", 2)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return dispatchConfig{
		timeout:     timeout,
		maxAttempts: maxAttempts,
		backoff:     backoff,
	}
}

func parseDurationEnv(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return d
}

func parseIntEnv(key string, def int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return val
}
