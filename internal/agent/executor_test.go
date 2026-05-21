package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Allowlist: map[string]string{"echo": "echo"},
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
	if !strings.Contains(resp.Stdout, "hello") {
		t.Fatalf("expected stdout to contain hello, got %q", resp.Stdout)
	}
}

func TestExecuteHandlerRecordsSuccessfulResultReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "echo"},
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
	if entry.report.Status != protocol.JobResultStatusCompleted || !strings.Contains(entry.report.Stdout, "hello") {
		t.Fatalf("unexpected cached report: %+v", entry.report)
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
		Allowlist: map[string]string{"echo": "echo"},
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
		Allowlist: map[string]string{"echo": "echo"},
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
		Allowlist: map[string]string{"false": "false"},
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
}

func TestExecuteHandlerRecordsNonZeroExitReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"false": "false"},
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
		Allowlist: map[string]string{"sleep": "sleep"},
		Timeout:   10 * time.Millisecond,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1"},
	})
	w := httptest.NewRecorder()

	NewExecuteHandler(cfg)(w, req)
	if w.Result().StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Result().StatusCode)
	}
}

func TestExecuteHandlerRecordsTimeoutReport(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"sleep": "sleep"},
		Timeout:   10 * time.Millisecond,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1"},
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
		Allowlist: map[string]string{"sleep": "sleep"},
		Timeout:   time.Second,
	}
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"1"},
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
		Allowlist: map[string]string{"sleep": "sleep"},
		Timeout:   time.Second,
	}
	tracker := NewLoadTracker()
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "sleep",
		Args:    []string{"0.1"},
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
