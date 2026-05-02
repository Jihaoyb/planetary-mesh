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

	"planetary-mesh/internal/coordinator"
	"planetary-mesh/internal/security"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := getEnv("COORDINATOR_ADDR", ":8080")
	databaseURL := os.Getenv("COORDINATOR_DATABASE_URL")
	tlsFiles := security.TLSFiles{
		CAFile:   os.Getenv("COORDINATOR_TLS_CA_FILE"),
		CertFile: os.Getenv("COORDINATOR_TLS_CERT_FILE"),
		KeyFile:  os.Getenv("COORDINATOR_TLS_KEY_FILE"),
	}
	if err := tlsFiles.ValidateComplete("COORDINATOR"); err != nil {
		logger.Error("invalid coordinator TLS config", "err", err)
		os.Exit(1)
	}
	secureMode := tlsFiles.Configured()

	allowedIdentities, err := security.ParseIdentityAllowlist(os.Getenv("COORDINATOR_ALLOWED_NODE_IDENTITIES"))
	if err != nil {
		logger.Error("invalid COORDINATOR_ALLOWED_NODE_IDENTITIES", "err", err)
		os.Exit(1)
	}
	allowedFingerprints, err := security.ParseFingerprintAllowlist(os.Getenv("COORDINATOR_ALLOWED_NODE_FINGERPRINTS"))
	if err != nil {
		logger.Error("invalid COORDINATOR_ALLOWED_NODE_FINGERPRINTS", "err", err)
		os.Exit(1)
	}
	allowlistConfigured := len(allowedIdentities) > 0 || len(allowedFingerprints) > 0
	if secureMode && !allowlistConfigured {
		logger.Error("secure coordinator mode requires COORDINATOR_ALLOWED_NODE_IDENTITIES or COORDINATOR_ALLOWED_NODE_FINGERPRINTS")
		os.Exit(1)
	}
	if !secureMode && allowlistConfigured {
		logger.Error("node allowlists require coordinator TLS config")
		os.Exit(1)
	}

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

	httpClient := http.DefaultClient
	var serverTLSConfig *tls.Config
	if secureMode {
		clientTLSConfig, err := security.ClientTLSConfig(tlsFiles)
		if err != nil {
			logger.Error("load coordinator client TLS config failed", "err", err)
			os.Exit(1)
		}
		serverTLSConfig, err = security.ServerTLSConfig(tlsFiles, true)
		if err != nil {
			logger.Error("load coordinator server TLS config failed", "err", err)
			os.Exit(1)
		}
		httpClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: clientTLSConfig},
		}
	}

	srv := coordinator.NewServerWithSecurity(
		registry,
		jobs,
		httpClient,
		coordinator.DefaultDispatchConfig(),
		coordinator.SecurityConfig{
			AllowedNodeIdentities:   allowedIdentities,
			AllowedNodeFingerprints: allowedFingerprints,
		},
		logger,
	)

	stopCh := make(chan struct{})
	coordinator.StartHealthChecker(registry, stopCh)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Mux(),
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

	logger.Info("coordinator starting", "addr", addr, "secure", secureMode)
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
	logger.Info("coordinator stopped")
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
