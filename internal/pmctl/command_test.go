package pmctl

import (
	"bytes"
	"context"
	"errors"
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

func TestWriteNodesAndJobs(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := writeNodes(&out, []Node{{ID: "node-1", State: "HEALTHY", Address: "localhost:8081", LastSeen: now}}); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	if !strings.Contains(out.String(), "node-1") || !strings.Contains(out.String(), "HEALTHY") {
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
