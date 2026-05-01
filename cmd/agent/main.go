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

	"planetary-mesh/internal/agent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := getEnv("AGENT_ADDR", ":8081")
	advertiseAddr := getEnv("AGENT_ADVERTISE_ADDR", addr)
	coordURL := getEnv("COORDINATOR_URL", "http://localhost:8080")
	nodeID := getEnv("NODE_ID", agent.DefaultNodeID())
	execTimeout := getEnv("AGENT_EXEC_TIMEOUT", agent.DefaultExecutionTimeout.String())
	allowlistRaw := getEnv("AGENT_COMMAND_ALLOWLIST", agent.DefaultAllowlist)

	timeout, err := time.ParseDuration(execTimeout)
	if err != nil {
		logger.Error("invalid AGENT_EXEC_TIMEOUT", "value", execTimeout, "err", err)
		os.Exit(1)
	}
	allowlist, err := agent.ParseAllowlist(allowlistRaw)
	if err != nil {
		logger.Error("invalid AGENT_COMMAND_ALLOWLIST", "value", allowlistRaw, "err", err)
		os.Exit(1)
	}
	cfg := agent.ExecutorConfig{
		Allowlist: allowlist,
		Timeout:   timeout,
	}

	if err := agent.RegisterWithCoordinator(coordURL, nodeID, advertiseAddr); err != nil {
		logger.Warn("initial registration failed", "err", err)
	} else {
		logger.Info("registered with coordinator", "node_id", nodeID)
	}

	stopCh := make(chan struct{})
	agent.StartHeartbeatLoop(coordURL, nodeID, advertiseAddr, stopCh)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           agent.MuxWithConfig(cfg),
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

	logger.Info("agent starting", "addr", addr, "advertise_addr", advertiseAddr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
	logger.Info("agent stopped")
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
