package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

func newExecuteRequest(t *testing.T, payload protocol.ExecuteRequest) *http.Request {
	t.Helper()
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	protocol.SetVersionHeader(req.Header)
	return req
}

func TestExecuteHandlerSuccess(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   2 * time.Second,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "echo",
		Args:    []string{"hello"},
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	var resp protocol.ExecuteResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Stdout != "hello\n" {
		t.Fatalf("expected stdout hello line, got %q", resp.Stdout)
	}
}

func TestExecuteHandlerRecordsSuccessfulResultReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   2 * time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "echo",
		Args:    []string{"hello"},
	})
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Result().StatusCode)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	entry, ok := reporter.entries["job-1"]
	if !ok {
		t.Fatalf("expected result report to be cached")
	}
	if entry.report.Status != protocol.JobResultStatusCompleted || entry.report.Stdout != "hello\n" {
		t.Fatalf("unexpected cached report: %+v", entry.report)
	}
}

func TestExecuteHandlerDoesNotRunBuiltinTargetWithoutLogicalAllowlistEntry(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   time.Second,
	}
	status, resp := executeCommand(t, cfg, "builtin:echo", "hello")

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	if !strings.Contains(resp.LastError, "not allowlisted") {
		t.Fatalf("expected allowlist error, got %+v", resp)
	}
}

func TestExecuteHandlerCanUseExplicitBuiltinNamedLogicalKey(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"builtin:echo": "builtin:echo"},
		Timeout:   time.Second,
	}
	status, resp := executeCommand(t, cfg, "builtin:echo", "hello", "mesh")

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if resp.Stdout != "hello mesh\n" {
		t.Fatalf("expected joined stdout, got %q", resp.Stdout)
	}
}

func TestExecuteHandlerBuiltinLineCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"line-count": "builtin:line-count"},
		Timeout:   time.Second,
	}
	status, resp := executeCommand(t, cfg, "line-count", path)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, resp)
	}
	if resp.Stdout != "3\n" {
		t.Fatalf("expected 3 lines, got %q", resp.Stdout)
	}
	if resp.Stderr != "" || resp.LastError != "" || resp.ExitCode != nil {
		t.Fatalf("unexpected line-count response: %+v", resp)
	}
}

func TestExecuteHandlerBuiltinLineCountEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"line-count": "builtin:line-count"},
		Timeout:   time.Second,
	}
	status, resp := executeCommand(t, cfg, "line-count", path)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, resp)
	}
	if resp.Stdout != "0\n" {
		t.Fatalf("expected 0 lines, got %q", resp.Stdout)
	}
}

func TestExecuteHandlerBuiltinSleepAcceptsPlainSeconds(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "builtin:sleep"},
		Timeout:   time.Second,
	}
	status, resp := executeCommand(t, cfg, "sleep", "0")

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, resp)
	}
}

func TestExecuteHandlerExternalExecutableAllowlistStillWorks(t *testing.T) {
	t.Setenv("PLANETARY_MESH_AGENT_HELPER", "1")
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"helper": os.Args[0]},
		Timeout:   2 * time.Second,
	}
	args := externalHelperArgs("echo", "hello", "from", "helper")
	status, resp := executeCommand(t, cfg, "helper", args...)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", status, resp)
	}
	if resp.Stdout != "hello from helper\n" {
		t.Fatalf("unexpected helper stdout: %q", resp.Stdout)
	}
}

func TestExecuteHandlerRejectsMissingVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader([]byte(`{"job_id":"job-1","type":"command","command":"echo"}`)))
	w := httptest.NewRecorder()

	ExecuteHandler(w, req)
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
}

func TestExecuteHandlerRejectsDisallowedCommand(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   time.Second,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestExecuteHandlerRecordsAllowlistRejectionReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
	})
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	entry, ok := reporter.entries["job-1"]
	if !ok {
		t.Fatalf("expected failed result report to be cached")
	}
	if entry.report.Status != protocol.JobResultStatusFailed || entry.report.LastError == "" {
		t.Fatalf("unexpected cached failure report: %+v", entry.report)
	}
}

func TestExecuteHandlerNonZeroExit(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"false": "builtin:false"},
		Timeout:   time.Second,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "false",
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)

	if w.Result().StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Result().StatusCode)
	}
	var resp protocol.ExecuteResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode == nil || *resp.ExitCode != 1 || resp.LastError != "command exited with code 1" {
		t.Fatalf("unexpected non-zero response: %+v", resp)
	}
}

func TestExecuteHandlerRecordsNonZeroExitReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"false": "builtin:false"},
		Timeout:   time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "false",
	})
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Result().StatusCode)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	entry, ok := reporter.entries["job-1"]
	if !ok {
		t.Fatalf("expected failed result report to be cached")
	}
	if entry.report.Status != protocol.JobResultStatusFailed || entry.report.ExitCode == nil || *entry.report.ExitCode != 1 {
		t.Fatalf("unexpected cached non-zero report: %+v", entry.report)
	}
}

func TestExecuteHandlerTimeout(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "builtin:sleep"},
		Timeout:   10 * time.Millisecond,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1s"},
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)
	if w.Result().StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Result().StatusCode)
	}
}

func TestExecuteHandlerRecordsTimeoutReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "builtin:sleep"},
		Timeout:   10 * time.Millisecond,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1s"},
	})
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Result().StatusCode)
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	entry, ok := reporter.entries["job-1"]
	if !ok {
		t.Fatalf("expected timeout result report to be cached")
	}
	if entry.report.Status != protocol.JobResultStatusFailed || !strings.Contains(entry.report.LastError, "timed out") {
		t.Fatalf("unexpected cached timeout report: %+v", entry.report)
	}
}

func TestExecuteHandlerDoesNotReportInternalExecutionError(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"missing": "/definitely/not/a/planetary-mesh-test-command"},
		Timeout:   time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "missing",
	})
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
	if got := reporter.CachedCount(); got != 0 {
		t.Fatalf("expected retryable internal error not to be reported, got %d cached reports", got)
	}
}

func TestExecuteHandlerDoesNotReportCanceledRequest(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "builtin:sleep"},
		Timeout:   time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1s"},
	})
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	NewExecuteHandlerWithLoadTrackerAndReporter(cfg, nil, reporter)(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
	if got := reporter.CachedCount(); got != 0 {
		t.Fatalf("expected canceled request not to be reported, got %d cached reports", got)
	}
}

func TestExecuteHandlerTracksActiveExecution(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "builtin:sleep"},
		Timeout:   time.Second,
	}
	tracker := NewLoadTracker()
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"100ms"},
	})
	w := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)
		NewExecuteHandlerWithLoadTracker(cfg, tracker)(w, req)
	}()

	observedActive := false
	for i := 0; i < 100; i++ {
		if tracker.Snapshot().ActiveExecutions == 1 {
			observedActive = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !observedActive {
		t.Fatalf("expected active execution count to reach 1")
	}
	<-done
	if tracker.Snapshot().ActiveExecutions != 0 {
		t.Fatalf("expected active execution count to return to 0, got %d", tracker.Snapshot().ActiveExecutions)
	}
}

func TestExecuteHandlerLegacyStubStillWorks(t *testing.T) {
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "echo",
		Payload: "hello",
	})
	w := httptest.NewRecorder()

	ExecuteHandler(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func executeCommand(t *testing.T, cfg ExecutorConfig, command string, args ...string) (int, protocol.ExecuteResponse) {
	t.Helper()
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: command,
		Args:    args,
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)

	var resp protocol.ExecuteResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return w.Result().StatusCode, resp
}

func externalHelperArgs(args ...string) []string {
	return append([]string{"-test.run=TestExternalCommandHelperProcess", "--"}, args...)
}

func TestExternalCommandHelperProcess(t *testing.T) {
	if os.Getenv("PLANETARY_MESH_AGENT_HELPER") != "1" {
		return
	}

	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}

	switch args[0] {
	case "echo":
		fmt.Println(strings.Join(args[1:], " "))
		os.Exit(0)
	default:
		os.Exit(2)
	}
}
