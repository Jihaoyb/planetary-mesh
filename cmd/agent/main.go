package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"planetary-mesh/internal/agent"
	"planetary-mesh/internal/security"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := getEnv("AGENT_ADDR", ":8081")
	tlsFiles := security.TLSFiles{
		CAFile:   os.Getenv("AGENT_TLS_CA_FILE"),
		CertFile: os.Getenv("AGENT_TLS_CERT_FILE"),
		KeyFile:  os.Getenv("AGENT_TLS_KEY_FILE"),
	}
	if err := tlsFiles.ValidateComplete("AGENT"); err != nil {
		logger.Error("invalid agent TLS config", "err", err)
		os.Exit(1)
	}
	secureMode := tlsFiles.Configured()

	defaultCoordURL := "http://localhost:8080"
	if secureMode {
		defaultCoordURL = "https://localhost:8080"
	}
	coordURL := getEnv("COORDINATOR_URL", defaultCoordURL)
	if secureMode && !strings.HasPrefix(coordURL, "https://") {
		logger.Error("secure agent mode requires COORDINATOR_URL to use https")
		os.Exit(1)
	}
	advertiseAddr := getEnv("AGENT_ADVERTISE_ADDR", addr)
	if _, ok := os.LookupEnv("AGENT_ADVERTISE_ADDR"); secureMode && !ok {
		advertiseAddr = security.HostPortToURL("https", addr)
	}
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

	httpClient := http.DefaultClient
	var serverTLSConfig *tls.Config
	if secureMode {
		clientTLSConfig, err := security.ClientTLSConfig(tlsFiles)
		if err != nil {
			logger.Error("load agent client TLS config failed", "err", err)
			os.Exit(1)
		}
		serverTLSConfig, err = security.ServerTLSConfig(tlsFiles, true)
		if err != nil {
			logger.Error("load agent server TLS config failed", "err", err)
			os.Exit(1)
		}
		httpClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: clientTLSConfig},
		}
	}

	if err := agent.RegisterWithCoordinatorClient(httpClient, coordURL, nodeID, advertiseAddr); err != nil {
		logger.Warn("initial registration failed", "err", err)
	} else {
		logger.Info("registered with coordinator", "node_id", nodeID)
	}

	stopCh := make(chan struct{})
	agent.StartHeartbeatLoopWithClient(httpClient, coordURL, nodeID, advertiseAddr, stopCh)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           agent.MuxWithConfig(cfg),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         serverTLSConfig,
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

	logger.Info("agent starting", "addr", addr, "advertise_addr", advertiseAddr, "secure", secureMode)
	var serveErr error
	if secureMode {
		serveErr = httpServer.ListenAndServeTLS("", "")
	} else {
		serveErr = httpServer.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("server error", "err", serveErr)
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
