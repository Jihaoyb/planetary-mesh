package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAgentConfigDefaults(t *testing.T) {
	clearAgentEnv(t)

	cfg := loadAgentConfigClean(t, nil)
	if cfg.Addr != ":8081" {
		t.Fatalf("expected default addr, got %q", cfg.Addr)
	}
	if cfg.CoordinatorURL != "http://localhost:8080" {
		t.Fatalf("expected default coordinator URL, got %q", cfg.CoordinatorURL)
	}
	if cfg.AdvertiseAddr != ":8081" {
		t.Fatalf("expected default advertise addr, got %q", cfg.AdvertiseAddr)
	}
	if cfg.Executor.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout, got %s", cfg.Executor.Timeout)
	}
	if len(cfg.Capabilities) != 0 {
		t.Fatalf("expected no default capabilities, got %q", cfg.Capabilities)
	}
	if cfg.SecureMode {
		t.Fatalf("expected plain mode by default")
	}
}

func TestLoadAgentConfigFromFileAndEnvOverride(t *testing.T) {
	clearAgentEnv(t)
	path := writeAgentTempConfig(t, `
NODE_ID=file-agent
AGENT_ADDR=:8082
AGENT_ADVERTISE_ADDR=http://localhost:8082
COORDINATOR_URL=http://localhost:9090
AGENT_EXEC_TIMEOUT=5s
AGENT_COMMAND_ALLOWLIST=echo=echo
AGENT_CAPABILITIES=role:worker,profile:local
`)
	t.Setenv("NODE_ID", "env-agent")
	t.Setenv("AGENT_CAPABILITIES", "role:override")

	cfg := loadAgentConfigClean(t, []string{"--config", path})
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.NodeID != "env-agent" {
		t.Fatalf("expected env node id override, got %q", cfg.NodeID)
	}
	if cfg.Addr != ":8082" || cfg.AdvertiseAddr != "http://localhost:8082" {
		t.Fatalf("unexpected address config: %+v", cfg)
	}
	if cfg.Executor.Timeout != 5*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.Executor.Timeout)
	}
	if cfg.Executor.Allowlist["echo"] != "echo" {
		t.Fatalf("unexpected allowlist: %+v", cfg.Executor.Allowlist)
	}
	if len(cfg.Capabilities) != 1 || cfg.Capabilities[0] != "role:override" {
		t.Fatalf("unexpected capabilities: %+v", cfg.Capabilities)
	}
}

func TestLoadAgentConfigFromPathEnv(t *testing.T) {
	clearAgentEnv(t)
	path := writeAgentTempConfig(t, `AGENT_ADDR=:8091`)
	t.Setenv("AGENT_CONFIG_FILE", path)

	cfg := loadAgentConfigClean(t, nil)
	if cfg.ConfigFile != path || cfg.Addr != ":8091" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadAgentExampleConfigs(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		nodeID        string
		addr          string
		advertiseAddr string
	}{
		{
			name:          "agent 1",
			path:          filepath.Join("..", "..", "config", "agent-1.env.example"),
			nodeID:        "local-agent-1",
			addr:          ":8081",
			advertiseAddr: "http://localhost:8081",
		},
		{
			name:          "agent 2",
			path:          filepath.Join("..", "..", "config", "agent-2.env.example"),
			nodeID:        "local-agent-2",
			addr:          ":8082",
			advertiseAddr: "http://localhost:8082",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAgentEnv(t)

			cfg := loadAgentConfigClean(t, []string{"--config", tc.path})
			if cfg.ConfigFile != tc.path {
				t.Fatalf("expected config file %q, got %q", tc.path, cfg.ConfigFile)
			}
			if cfg.NodeID != tc.nodeID {
				t.Fatalf("expected node id %q, got %q", tc.nodeID, cfg.NodeID)
			}
			if cfg.Addr != tc.addr || cfg.AdvertiseAddr != tc.advertiseAddr {
				t.Fatalf("unexpected address config: %+v", cfg)
			}
			if cfg.CoordinatorURL != "http://localhost:8080" {
				t.Fatalf("expected local coordinator URL, got %q", cfg.CoordinatorURL)
			}
			if cfg.Executor.Allowlist["echo"] != "echo" {
				t.Fatalf("expected echo allowlist entry, got %+v", cfg.Executor.Allowlist)
			}
			if len(cfg.Capabilities) == 0 {
				t.Fatalf("expected example capabilities")
			}
			if cfg.SecureMode {
				t.Fatalf("expected example agent config to default to plain mode")
			}
		})
	}
}

func TestLoadAgentConfigSecureDefaultsAdvertiseURL(t *testing.T) {
	clearAgentEnv(t)
	path := writeAgentTempConfig(t, `
AGENT_ADDR=:9443
AGENT_TLS_CA_FILE=ca.pem
AGENT_TLS_CERT_FILE=agent.pem
AGENT_TLS_KEY_FILE=agent-key.pem
`)

	cfg := loadAgentConfigClean(t, []string{"--config", path})
	if !cfg.SecureMode {
		t.Fatalf("expected secure mode")
	}
	if cfg.CoordinatorURL != "https://localhost:8080" {
		t.Fatalf("expected secure coordinator default, got %q", cfg.CoordinatorURL)
	}
	if cfg.AdvertiseAddr != "https://localhost:9443" {
		t.Fatalf("expected HTTPS advertise URL, got %q", cfg.AdvertiseAddr)
	}
}

func TestLoadAgentConfigRejectsSecureHTTPURL(t *testing.T) {
	clearAgentEnv(t)
	path := writeAgentTempConfig(t, `
COORDINATOR_URL=http://localhost:8080
AGENT_TLS_CA_FILE=ca.pem
AGENT_TLS_CERT_FILE=agent.pem
AGENT_TLS_KEY_FILE=agent-key.pem
`)
	err := loadAgentConfigError(t, []string{"--config", path})
	if err == nil || !strings.Contains(err.Error(), "secure agent mode requires COORDINATOR_URL to use https") {
		t.Fatalf("expected secure URL error, got %v", err)
	}
}

func TestLoadAgentConfigRejectsInvalidTimeoutAndAllowlist(t *testing.T) {
	clearAgentEnv(t)
	timeoutPath := writeAgentTempConfig(t, `AGENT_EXEC_TIMEOUT=not-a-duration`)
	if err := loadAgentConfigError(t, []string{"--config", timeoutPath}); err == nil || !strings.Contains(err.Error(), "invalid AGENT_EXEC_TIMEOUT") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	clearAgentEnv(t)
	allowlistPath := writeAgentTempConfig(t, `AGENT_COMMAND_ALLOWLIST=bad-entry`)
	if err := loadAgentConfigError(t, []string{"--config", allowlistPath}); err == nil || !strings.Contains(err.Error(), "invalid AGENT_COMMAND_ALLOWLIST") {
		t.Fatalf("expected allowlist error, got %v", err)
	}

	clearAgentEnv(t)
	capabilitiesPath := writeAgentTempConfig(t, `AGENT_CAPABILITIES=-bad`)
	if err := loadAgentConfigError(t, []string{"--config", capabilitiesPath}); err == nil || !strings.Contains(err.Error(), "invalid AGENT_CAPABILITIES") {
		t.Fatalf("expected capabilities error, got %v", err)
	}
}

func TestLoadAgentConfigRejectsMissingExplicitFile(t *testing.T) {
	clearAgentEnv(t)
	err := loadAgentConfigError(t, []string{"--config", filepath.Join(t.TempDir(), "missing.env")})
	if err == nil || !strings.Contains(err.Error(), "load config file") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing file error, got %v", err)
	}
}

func loadAgentConfigClean(t *testing.T, args []string) agentConfig {
	t.Helper()
	cfg, err := loadAgentConfig(args)
	if err != nil {
		t.Fatalf("loadAgentConfig returned error: %v", err)
	}
	return cfg
}

func loadAgentConfigError(t *testing.T, args []string) error {
	t.Helper()
	_, err := loadAgentConfig(args)
	return err
}

func clearAgentEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"AGENT_CONFIG_FILE",
		"AGENT_ADDR",
		"AGENT_ADVERTISE_ADDR",
		"COORDINATOR_URL",
		"NODE_ID",
		"AGENT_EXEC_TIMEOUT",
		"AGENT_COMMAND_ALLOWLIST",
		"AGENT_CAPABILITIES",
		"AGENT_TLS_CA_FILE",
		"AGENT_TLS_CERT_FILE",
		"AGENT_TLS_KEY_FILE",
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

func writeAgentTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
