package pmctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

type fakeClient struct {
	status protocol.CoordinatorStatusResponse
	nodes  []Node
	jobs   []Job
	job    Job

	command string
	args    []string
	err     error
}

func (f *fakeClient) Status(context.Context) (protocol.CoordinatorStatusResponse, error) {
	return f.status, f.err
}

func (f *fakeClient) ListNodes(context.Context) ([]Node, error) {
	return f.nodes, f.err
}

func (f *fakeClient) ListJobs(context.Context) ([]Job, error) {
	return f.jobs, f.err
}

func (f *fakeClient) GetJob(_ context.Context, id string) (Job, error) {
	if f.err != nil {
		return Job{}, f.err
	}
	if id != f.job.ID {
		return Job{}, errors.New("unexpected job id")
	}
	return f.job, nil
}

func (f *fakeClient) CreateCommandJob(_ context.Context, command string, args []string) (Job, error) {
	f.command = command
	f.args = append([]string(nil), args...)
	return f.job, f.err
}

func TestRunCommandStatus(t *testing.T) {
	client := &fakeClient{status: protocol.CoordinatorStatusResponse{
		Status:          "ok",
		ProtocolVersion: protocol.Version,
		StorageBackend:  "in_memory",
		Dispatch:        protocol.DispatchStatus{MaxAttempts: 3, Timeout: "10s", BaseBackoff: "500ms"},
	}}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"status"}, &out, false); err != nil {
		t.Fatalf("status command: %v", err)
	}
	text := out.String()
	for _, want := range []string{"STATUS", "PROTOCOL", "in_memory", "attempts=3"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestRunCommandSubmitCommand(t *testing.T) {
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "echo", Args: []string{"hello"}, Status: "QUEUED"}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"submit", "command", "echo", "hello"}, &out, false)
	if err != nil {
		t.Fatalf("submit command: %v", err)
	}
	if client.command != "echo" || len(client.args) != 1 || client.args[0] != "hello" {
		t.Fatalf("unexpected command request: command=%q args=%q", client.command, client.args)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "QUEUED") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestRunCommandJSONOutput(t *testing.T) {
	client := &fakeClient{jobs: []Job{{ID: "job-1", Status: "COMPLETED"}}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"jobs", "list"}, &out, true)
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	if !strings.Contains(out.String(), `"id": "job-1"`) {
		t.Fatalf("expected JSON output, got:\n%s", out.String())
	}
}

func TestRunCommandNodesJSONOutputIncludesMetadataDefaults(t *testing.T) {
	client := &fakeClient{nodes: []Node{{ID: "node-1", State: "HEALTHY"}}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"nodes", "list"}, &out, true)
	if err != nil {
		t.Fatalf("nodes list: %v", err)
	}
	text := out.String()
	for _, want := range []string{`"id": "node-1"`, `"capabilities": []`, `"active_executions": 0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected JSON output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestRunCommandUsageErrors(t *testing.T) {
	err := runCommandWithClient(context.Background(), &fakeClient{}, []string{"jobs"}, ioDiscard{}, false)
	if err == nil || !isUsageError(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestParseGlobalFlagsUsesEnvDefaults(t *testing.T) {
	cfg := Config{CoordinatorURL: "http://from-env:8080"}
	jsonOut, args, err := parseGlobalFlags([]string{"--json", "status"}, &cfg)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !jsonOut || len(args) != 1 || args[0] != "status" {
		t.Fatalf("unexpected parse result: json=%t args=%q", jsonOut, args)
	}
	if cfg.CoordinatorURL != "http://from-env:8080" {
		t.Fatalf("unexpected coordinator URL: %s", cfg.CoordinatorURL)
	}
}

func TestConfigFromSourcesLoadsFileAndEnvOverride(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `
PMCTL_COORDINATOR_URL=http://from-file:8080
PMCTL_TLS_CA_FILE=file-ca.pem
PMCTL_TLS_CERT_FILE=file-cert.pem
PMCTL_TLS_KEY_FILE=file-key.pem
`)
	t.Setenv("PMCTL_COORDINATOR_URL", "http://from-env:8080")

	cfg, err := ConfigFromSources([]string{"--config", path, "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.CoordinatorURL != "http://from-env:8080" {
		t.Fatalf("expected env coordinator URL, got %q", cfg.CoordinatorURL)
	}
	if cfg.TLSFiles.CAFile != "file-ca.pem" || cfg.TLSFiles.CertFile != "file-cert.pem" || cfg.TLSFiles.KeyFile != "file-key.pem" {
		t.Fatalf("unexpected TLS config: %+v", cfg.TLSFiles)
	}
}

func TestConfigFromSourcesUsesConfigPathEnv(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `PMCTL_COORDINATOR_URL=http://from-path-env:8080`)
	t.Setenv("PMCTL_CONFIG_FILE", path)

	cfg, err := ConfigFromSources([]string{"status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path || cfg.CoordinatorURL != "http://from-path-env:8080" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestConfigFromSourcesLoadsExampleConfig(t *testing.T) {
	clearPMCTLEnv(t)
	path := filepath.Join("..", "..", "config", "pmctl.env.example")

	cfg, err := ConfigFromSources([]string{"--config", path, "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.CoordinatorURL != "http://localhost:8080" {
		t.Fatalf("expected local coordinator URL, got %q", cfg.CoordinatorURL)
	}
	if cfg.TLSFiles.Configured() {
		t.Fatalf("expected example pmctl config to default to plain mode")
	}
}

func TestConfigFromSourcesFlagOverridesEnvAfterParsing(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `PMCTL_COORDINATOR_URL=http://from-file:8080`)
	t.Setenv("PMCTL_COORDINATOR_URL", "http://from-env:8080")

	cfg, err := ConfigFromSources([]string{"--config", path, "--coordinator-url", "http://from-flag:8080", "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	_, args, err := parseGlobalFlags([]string{"--config", path, "--coordinator-url", "http://from-flag:8080", "status"}, &cfg)
	if err != nil {
		t.Fatalf("parseGlobalFlags returned error: %v", err)
	}
	if len(args) != 1 || args[0] != "status" {
		t.Fatalf("unexpected args: %q", args)
	}
	if cfg.CoordinatorURL != "http://from-flag:8080" {
		t.Fatalf("expected flag coordinator URL, got %q", cfg.CoordinatorURL)
	}
}

func TestConfigFromSourcesRejectsMissingExplicitFile(t *testing.T) {
	clearPMCTLEnv(t)
	_, err := ConfigFromSources([]string{"--config", filepath.Join(t.TempDir(), "missing.env"), "status"})
	if err == nil || !strings.Contains(err.Error(), "load config file") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing config file error, got %v", err)
	}
}

func TestWriteNodesAndJobs(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := writeNodes(&out, []Node{{
		ID:           "node-1",
		State:        "HEALTHY",
		Address:      "localhost:8081",
		LastSeen:     now,
		Capabilities: []string{"profile:local", "role:worker"},
		Load:         protocol.NodeLoad{ActiveExecutions: 2},
	}}); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	if !strings.Contains(out.String(), "ACTIVE") || !strings.Contains(out.String(), "node-1") || !strings.Contains(out.String(), "HEALTHY") || !strings.Contains(out.String(), "profile:local,role:worker") {
		t.Fatalf("unexpected nodes output:\n%s", out.String())
	}

	out.Reset()
	if err := writeJobs(&out, []Job{{ID: "job-1", Status: "COMPLETED", Type: "command", Command: "echo", Args: []string{"hi"}, UpdatedAt: now}}); err != nil {
		t.Fatalf("write jobs: %v", err)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "echo hi") {
		t.Fatalf("unexpected jobs output:\n%s", out.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func clearPMCTLEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"PMCTL_CONFIG_FILE",
		"PMCTL_COORDINATOR_URL",
		"PMCTL_TLS_CA_FILE",
		"PMCTL_TLS_CERT_FILE",
		"PMCTL_TLS_KEY_FILE",
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

func writePMCTLTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pmctl.env")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
