package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

func startFakeAgentClient(t *testing.T, status int, delay time.Duration, resp protocol.ExecuteResponse) (*http.Client, *atomic.Uint64) {
	t.Helper()
	var calls atomic.Uint64
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/execute" {
				return jsonResponse(http.StatusNotFound, map[string]string{"status": "not_found"}), nil
			}
			if !protocol.HasExpectedVersion(r.Header) {
				return jsonResponse(http.StatusConflict, map[string]string{"status": "error", "last_error": "protocol version mismatch"}), nil
			}
			calls.Add(1)
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return jsonResponse(status, resp), nil
		}),
	}
	return client, &calls
}

func registerNode(t *testing.T, mux http.Handler, id, addr string) {
	t.Helper()
	body, _ := json.Marshal(registerRequest{ID: id, Address: addr})
	req := newVersionedRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("register failed: %d", w.Result().StatusCode)
	}
}

func submitCommandJob(t *testing.T, mux http.Handler, command string, args ...string) Job {
	t.Helper()
	body, _ := json.Marshal(createJobRequest{Type: "command", Command: command, Args: args})
	req := newVersionedRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("create job failed: %d", w.Result().StatusCode)
	}
	var j Job
	if err := json.NewDecoder(w.Result().Body).Decode(&j); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	return j
}

func waitForJobStatus(t *testing.T, mux http.Handler, jobID string, want JobStatus, timeout time.Duration) Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req := newVersionedRequest(http.MethodGet, "/jobs/"+jobID, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Result().StatusCode == http.StatusOK {
			var j Job
			if err := json.NewDecoder(w.Result().Body).Decode(&j); err == nil && j.Status == want {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s to reach %s", jobID, want)
	return Job{}
}

func TestEndToEndCommandLifecycle(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusOK, 0, protocol.ExecuteResponse{
		Status: "ok",
		Stdout: "hello e2e\n",
	})

	reg := NewNodeRegistry()
	store := NewJobStore()
	srv := NewServerWithConfig(reg, store, client, DefaultDispatchConfig(), nil)
	mux := srv.Mux()

	registerNode(t, mux, "agent-e2e", "agent.local:8081")

	job := submitCommandJob(t, mux, "echo", "hello e2e")
	final := waitForJobStatus(t, mux, job.ID, JobStatusCompleted, 2*time.Second)

	if final.NodeID != "agent-e2e" {
		t.Errorf("expected NodeID agent-e2e, got %s", final.NodeID)
	}
	if final.Stdout != "hello e2e\n" {
		t.Errorf("expected stdout to be recorded, got %q", final.Stdout)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 agent call, got %d", calls.Load())
	}
}

func TestDispatchRetriesOn500ThenFails(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusInternalServerError, 0, protocol.ExecuteResponse{
		Status:    "error",
		LastError: "internal failure",
	})

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 3, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, client, cfg, nil)
	mux := srv.Mux()

	registerNode(t, mux, "agent-flaky", "agent.local:8081")

	job := submitCommandJob(t, mux, "echo", "boom")
	final := waitForJobStatus(t, mux, job.ID, JobStatusFailed, 2*time.Second)

	if final.LastError == "" {
		t.Fatalf("expected last_error to be set")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 dispatch attempts, got %d", got)
	}
}

func TestDispatchNoRetryOn422(t *testing.T) {
	exitCode := 2
	client, calls := startFakeAgentClient(t, http.StatusUnprocessableEntity, 0, protocol.ExecuteResponse{
		Status:    "error",
		ExitCode:  &exitCode,
		LastError: "command exited with code 2",
		Stderr:    "boom\n",
	})

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 3, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, client, cfg, nil)
	mux := srv.Mux()

	registerNode(t, mux, "agent-422", "agent.local:8081")

	job := submitCommandJob(t, mux, "false")
	final := waitForJobStatus(t, mux, job.ID, JobStatusFailed, time.Second)

	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 attempt for 422, got %d", calls.Load())
	}
	if final.ExitCode == nil || *final.ExitCode != 2 {
		t.Errorf("expected exit code 2, got %#v", final.ExitCode)
	}
}

func TestDispatchTimeoutTriggersRetry(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusOK, 200*time.Millisecond, protocol.ExecuteResponse{Status: "ok"})

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: 20 * time.Millisecond, MaxAttempts: 2, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, client, cfg, nil)
	mux := srv.Mux()

	registerNode(t, mux, "agent-slow", "agent.local:8081")

	job := submitCommandJob(t, mux, "sleep", "1")
	waitForJobStatus(t, mux, job.ID, JobStatusFailed, 2*time.Second)

	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts due to timeout retries, got %d", got)
	}
}

func TestProtocolVersionMismatchOnJobs(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	body, _ := json.Marshal(createJobRequest{Type: "command", Command: "echo"})
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
}

func TestMetricsEndpointExposesCounters(t *testing.T) {
	client, _ := startFakeAgentClient(t, http.StatusOK, 0, protocol.ExecuteResponse{Status: "ok"})

	schema := protocol.SchemaStatus{Ready: true, Version: 1, ExpectedVersion: 1}
	srv := NewServerWithRuntime(
		NewNodeRegistry(),
		NewJobStore(),
		client,
		DefaultDispatchConfig(),
		SecurityConfig{},
		RuntimeConfig{StorageBackend: "postgres", Schema: &schema},
		nil,
	)
	srv.Metrics().StartupRecoveredJobs.Store(2)
	mux := srv.Mux()

	registerNode(t, mux, "agent-metrics", "agent.local:8081")
	job := submitCommandJob(t, mux, "echo", "metrics")
	waitForJobStatus(t, mux, job.ID, JobStatusCompleted, 2*time.Second)

	req := newVersionedRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	text := string(body)
	for _, want := range []string{
		"planetary_jobs_created_total 1",
		"planetary_jobs_completed_total 1",
		"planetary_jobs_recovered_on_startup_total 2",
		`planetary_nodes{state="HEALTHY"} 1`,
		"planetary_postgres_schema_ready 1",
		"planetary_postgres_schema_version 1",
		"planetary_postgres_schema_expected_version 1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected metrics to contain %q, got:\n%s", want, text)
		}
	}
}
