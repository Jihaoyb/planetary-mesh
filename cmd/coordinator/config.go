package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"planetary-mesh/internal/configfile"
	"planetary-mesh/internal/coordinator"
	"planetary-mesh/internal/security"
)

const (
	defaultCoordinatorConfigPath = "config/coordinator.env"
	coordinatorConfigFileEnv     = "COORDINATOR_CONFIG_FILE"
)

type coordinatorConfig struct {
	ConfigFile              string
	Addr                    string
	DatabaseURL             string
	ReconciliationGrace     time.Duration
	TLSFiles                security.TLSFiles
	AllowedNodeIdentities   map[string][]string
	AllowedNodeFingerprints map[string][]string
	SecureMode              bool
}

func loadCoordinatorConfig(args []string) (coordinatorConfig, error) {
	var cfgPath string
	fs := flag.NewFlagSet("coordinator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfgPath, "config", "", "path to env-style config file")
	if err := fs.Parse(args); err != nil {
		return coordinatorConfig{}, err
	}
	if fs.NArg() != 0 {
		return coordinatorConfig{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	path, err := configfile.ResolvePath(cfgPath, os.Getenv(coordinatorConfigFileEnv), defaultCoordinatorConfigPath)
	if err != nil {
		return coordinatorConfig{}, fmt.Errorf("resolve config file: %w", err)
	}

	fileValues := map[string]string{}
	if path != "" {
		fileValues, err = configfile.Load(path)
		if err != nil {
			return coordinatorConfig{}, fmt.Errorf("load config file %s: %w", path, err)
		}
	}
	source := configSource{fileValues: fileValues}

	tlsFiles := security.TLSFiles{
		CAFile:   source.get("COORDINATOR_TLS_CA_FILE", ""),
		CertFile: source.get("COORDINATOR_TLS_CERT_FILE", ""),
		KeyFile:  source.get("COORDINATOR_TLS_KEY_FILE", ""),
	}
	if err := tlsFiles.ValidateComplete("COORDINATOR"); err != nil {
		return coordinatorConfig{}, err
	}

	allowedIdentities, err := security.ParseIdentityAllowlist(source.get("COORDINATOR_ALLOWED_NODE_IDENTITIES", ""))
	if err != nil {
		return coordinatorConfig{}, fmt.Errorf("invalid COORDINATOR_ALLOWED_NODE_IDENTITIES: %w", err)
	}
	allowedFingerprints, err := security.ParseFingerprintAllowlist(source.get("COORDINATOR_ALLOWED_NODE_FINGERPRINTS", ""))
	if err != nil {
		return coordinatorConfig{}, fmt.Errorf("invalid COORDINATOR_ALLOWED_NODE_FINGERPRINTS: %w", err)
	}

	secureMode := tlsFiles.Configured()
	allowlistConfigured := len(allowedIdentities) > 0 || len(allowedFingerprints) > 0
	if secureMode && !allowlistConfigured {
		return coordinatorConfig{}, fmt.Errorf("secure coordinator mode requires COORDINATOR_ALLOWED_NODE_IDENTITIES or COORDINATOR_ALLOWED_NODE_FINGERPRINTS")
	}
	if !secureMode && allowlistConfigured {
		return coordinatorConfig{}, fmt.Errorf("node allowlists require coordinator TLS config")
	}

	reconciliationGraceRaw := source.get("COORDINATOR_RECONCILIATION_GRACE", coordinator.DefaultReconciliationGrace.String())
	reconciliationGrace, err := time.ParseDuration(reconciliationGraceRaw)
	if err != nil {
		return coordinatorConfig{}, fmt.Errorf("invalid COORDINATOR_RECONCILIATION_GRACE %q: %w", reconciliationGraceRaw, err)
	}
	if reconciliationGrace < 0 {
		return coordinatorConfig{}, fmt.Errorf("COORDINATOR_RECONCILIATION_GRACE cannot be negative")
	}

	return coordinatorConfig{
		ConfigFile:              path,
		Addr:                    source.get("COORDINATOR_ADDR", ":8080"),
		DatabaseURL:             source.get("COORDINATOR_DATABASE_URL", ""),
		ReconciliationGrace:     reconciliationGrace,
		TLSFiles:                tlsFiles,
		AllowedNodeIdentities:   allowedIdentities,
		AllowedNodeFingerprints: allowedFingerprints,
		SecureMode:              secureMode,
	}, nil
}

type configSource struct {
	fileValues map[string]string
}

func (s configSource) get(key, def string) string {
	if v, ok := s.fileValues[key]; ok {
		def = v
	}
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
