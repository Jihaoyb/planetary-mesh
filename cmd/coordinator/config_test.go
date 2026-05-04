package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCoordinatorConfigDefaults(t *testing.T) {
	clearCoordinatorEnv(t)

	cfg := loadCoordinatorConfigClean(t, nil)
	if cfg.Addr != ":8080" {
		t.Fatalf("expected default addr, got %q", cfg.Addr)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("expected empty database URL, got %q", cfg.DatabaseURL)
	}
	if cfg.SecureMode {
		t.Fatalf("expected plain mode by default")
	}
}

func TestLoadCoordinatorConfigFromFileAndEnvOverride(t *testing.T) {
	clearCoordinatorEnv(t)
	path := writeTempConfig(t, `
COORDINATOR_ADDR=:9090
COORDINATOR_DATABASE_URL=postgres://from-file
`)
	t.Setenv("COORDINATOR_ADDR", ":9999")

	cfg := loadCoordinatorConfigClean(t, []string{"--config", path})
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.Addr != ":9999" {
		t.Fatalf("expected env override addr, got %q", cfg.Addr)
	}
	if cfg.DatabaseURL != "postgres://from-file" {
		t.Fatalf("expected file database URL, got %q", cfg.DatabaseURL)
	}
}

func TestLoadCoordinatorConfigFromPathEnv(t *testing.T) {
	clearCoordinatorEnv(t)
	path := writeTempConfig(t, `COORDINATOR_ADDR=:9091`)
	t.Setenv("COORDINATOR_CONFIG_FILE", path)

	cfg := loadCoordinatorConfigClean(t, nil)
	if cfg.ConfigFile != path || cfg.Addr != ":9091" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadCoordinatorConfigRejectsTLSWithoutAllowlist(t *testing.T) {
	clearCoordinatorEnv(t)
	path := writeTempConfig(t, `
COORDINATOR_TLS_CA_FILE=ca.pem
COORDINATOR_TLS_CERT_FILE=coordinator.pem
COORDINATOR_TLS_KEY_FILE=coordinator-key.pem
`)
	err := loadCoordinatorConfigError(t, []string{"--config", path})
	if err == nil || !strings.Contains(err.Error(), "secure coordinator mode requires") {
		t.Fatalf("expected secure allowlist error, got %v", err)
	}
}

func TestLoadCoordinatorConfigRejectsAllowlistWithoutTLS(t *testing.T) {
	clearCoordinatorEnv(t)
	path := writeTempConfig(t, `COORDINATOR_ALLOWED_NODE_IDENTITIES=agent-1=dns:agent-1.local`)
	err := loadCoordinatorConfigError(t, []string{"--config", path})
	if err == nil || !strings.Contains(err.Error(), "node allowlists require coordinator TLS config") {
		t.Fatalf("expected allowlist TLS error, got %v", err)
	}
}

func TestLoadCoordinatorConfigRejectsPartialTLS(t *testing.T) {
	clearCoordinatorEnv(t)
	path := writeTempConfig(t, `COORDINATOR_TLS_CA_FILE=ca.pem`)
	err := loadCoordinatorConfigError(t, []string{"--config", path})
	if err == nil || !strings.Contains(err.Error(), "partial TLS config") {
		t.Fatalf("expected partial TLS error, got %v", err)
	}
}

func TestLoadCoordinatorConfigRejectsMissingExplicitFile(t *testing.T) {
	clearCoordinatorEnv(t)
	err := loadCoordinatorConfigError(t, []string{"--config", filepath.Join(t.TempDir(), "missing.env")})
	if err == nil || !strings.Contains(err.Error(), "load config file") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing file error, got %v", err)
	}
}

func loadCoordinatorConfigClean(t *testing.T, args []string) coordinatorConfig {
	t.Helper()
	cfg, err := loadCoordinatorConfig(args)
	if err != nil {
		t.Fatalf("loadCoordinatorConfig returned error: %v", err)
	}
	return cfg
}

func loadCoordinatorConfigError(t *testing.T, args []string) error {
	t.Helper()
	_, err := loadCoordinatorConfig(args)
	return err
}

func clearCoordinatorEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"COORDINATOR_CONFIG_FILE",
		"COORDINATOR_ADDR",
		"COORDINATOR_DATABASE_URL",
		"COORDINATOR_TLS_CA_FILE",
		"COORDINATOR_TLS_CERT_FILE",
		"COORDINATOR_TLS_KEY_FILE",
		"COORDINATOR_ALLOWED_NODE_IDENTITIES",
		"COORDINATOR_ALLOWED_NODE_FINGERPRINTS",
	}
	saved := make(map[string]string)
	present := make(map[string]bool)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			saved[key] = value
			present[key] = true
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			if present[key] {
				_ = os.Setenv(key, saved[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
