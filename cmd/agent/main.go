package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"planetary-mesh/internal/agent"
	"planetary-mesh/internal/security"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadAgentConfig(os.Args[1:])
	if err != nil {
		logger.Error("invalid agent config", "err", err)
		os.Exit(1)
	}
	if cfg.ConfigFile != "" {
		logger.Info("agent config loaded", "path", cfg.ConfigFile)
	}

	httpClient := http.DefaultClient
	var serverTLSConfig *tls.Config
	if cfg.SecureMode {
		clientTLSConfig, err := security.ClientTLSConfig(cfg.TLSFiles)
		if err != nil {
			logger.Error("load agent client TLS config failed", "err", err)
			os.Exit(1)
		}
		serverTLSConfig, err = security.ServerTLSConfig(cfg.TLSFiles, true)
		if err != nil {
			logger.Error("load agent server TLS config failed", "err", err)
			os.Exit(1)
		}
		httpClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: clientTLSConfig},
		}
	}

	loadTracker := agent.NewLoadTracker()
	registrationMetadata := func() agent.RegistrationMetadata {
		return agent.RegistrationMetadata{
			Capabilities: cfg.Capabilities,
			Load:         loadTracker.Snapshot(),
		}
	}

	if err := agent.RegisterWithCoordinatorClientWithMetadata(httpClient, cfg.CoordinatorURL, cfg.NodeID, cfg.AdvertiseAddr, registrationMetadata()); err != nil {
		logger.Warn("initial registration failed", "err", err)
	} else {
		logger.Info("registered with coordinator", "node_id", cfg.NodeID)
	}

	resultReporter := agent.NewResultReporter(httpClient, cfg.CoordinatorURL, cfg.NodeID)
	stopCh := make(chan struct{})
	agent.StartHeartbeatLoopWithClientAndMetadata(httpClient, cfg.CoordinatorURL, cfg.NodeID, cfg.AdvertiseAddr, registrationMetadata, stopCh)
	resultReporter.Start(stopCh)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           agent.MuxWithConfigLoadTrackerAndReporter(cfg.Executor, loadTracker, resultReporter),
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

	logger.Info("agent starting", "addr", cfg.Addr, "advertise_addr", cfg.AdvertiseAddr, "secure", cfg.SecureMode)
	var serveErr error
	if cfg.SecureMode {
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
