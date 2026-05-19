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
	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadCoordinatorConfig(os.Args[1:])
	if err != nil {
		logger.Error("invalid coordinator config", "err", err)
		os.Exit(1)
	}
	if cfg.ConfigFile != "" {
		logger.Info("coordinator config loaded", "path", cfg.ConfigFile)
	}

	var registry coordinator.NodeStore
	var jobs coordinator.JobStorage
	var postgresStore *coordinator.PostgresStore
	storageBackend := "in_memory"
	var schemaStatus *protocol.SchemaStatus
	var recoveredRunningJobs int64

	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		store, err := coordinator.OpenPostgresStoreWithRetry(ctx, cfg.DatabaseURL)
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
		storageBackend = "postgres"
		status := store.SchemaStatus()
		schemaStatus = &status
		recovered, err := jobs.FailRunningJobs(coordinator.RestartRecoveryError)
		if err != nil {
			logger.Error("recover running jobs failed", "err", err)
			os.Exit(1)
		}
		recoveredRunningJobs = recovered
		logger.Info(
			"postgres storage initialized",
			"recovered_running_jobs", recovered,
			"schema_ready", status.Ready,
			"schema_version", status.Version,
			"schema_expected_version", status.ExpectedVersion,
		)
	} else {
		registry = coordinator.NewNodeRegistry()
		jobs = coordinator.NewJobStore()
		logger.Info("in-memory storage initialized")
	}

	httpClient := http.DefaultClient
	var serverTLSConfig *tls.Config
	if cfg.SecureMode {
		clientTLSConfig, err := security.ClientTLSConfig(cfg.TLSFiles)
		if err != nil {
			logger.Error("load coordinator client TLS config failed", "err", err)
			os.Exit(1)
		}
		serverTLSConfig, err = security.ServerTLSConfig(cfg.TLSFiles, true)
		if err != nil {
			logger.Error("load coordinator server TLS config failed", "err", err)
			os.Exit(1)
		}
		httpClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: clientTLSConfig},
		}
	}

	srv := coordinator.NewServerWithRuntime(
		registry,
		jobs,
		httpClient,
		coordinator.DefaultDispatchConfig(),
		coordinator.SecurityConfig{
			AllowedNodeIdentities:   cfg.AllowedNodeIdentities,
			AllowedNodeFingerprints: cfg.AllowedNodeFingerprints,
		},
		coordinator.RuntimeConfig{
			StorageBackend: storageBackend,
			Schema:         schemaStatus,
			SecureMode:     cfg.SecureMode,
		},
		logger,
	)
	srv.Metrics().StartupRecoveredJobs.Store(uint64(recoveredRunningJobs))

	stopCh := make(chan struct{})
	coordinator.StartHealthChecker(registry, stopCh)
	srv.StartQueuedJobScheduler(stopCh)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
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

	logger.Info("coordinator starting", "addr", cfg.Addr, "secure", cfg.SecureMode)
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
	logger.Info("coordinator stopped")
}
