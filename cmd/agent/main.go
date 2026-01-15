package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// main wires config, coordinator registration, heartbeat, and HTTP server.
func main() {
	addr := getEnv("AGENT_ADDR", ":8081")
	coordURL := getEnv("COORDINATOR_URL", "http://localhost:8080")
	nodeID := getEnv("NODE_ID", defaultNodeID())
	cfg := loadAgentConfig()
	httpClient := &http.Client{Timeout: cfg.requestTimeout}

	if err := registerWithCoordinator(coordURL, nodeID, addr, httpClient); err != nil {
		log.Printf("[agent] failed to register with coordinator: %v", err)
	} else {
		log.Printf("[agent] registered with coordinator as %q", nodeID)
	}

	// Start periodic heartbeat
	stopHeartbeat := startHeartbeatLoop(coordURL, nodeID, addr, cfg.heartbeatInterval, httpClient)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/execute", executeHandler)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[agent] starting on %s\n", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[agent] server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[agent] shutdown signal received")
	stopHeartbeat()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("[agent] graceful shutdown failed: %v", err)
	}
}

// healthHandler handles /healthz as before.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

type agentConfig struct {
	heartbeatInterval time.Duration
	requestTimeout    time.Duration
}

// loadAgentConfig reads agent timing config from environment with sane defaults.
func loadAgentConfig() agentConfig {
	return agentConfig{
		heartbeatInterval: parseDurationEnv("HEARTBEAT_INTERVAL", 10*time.Second),
		requestTimeout:    parseDurationEnv("COORD_REQUEST_TIMEOUT", 5*time.Second),
	}
}

// parseDurationEnv parses a duration string or falls back to the provided default.
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
