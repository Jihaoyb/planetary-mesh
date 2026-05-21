package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestQueuedSchedulerDispatchesJobAfterNodeRegisters(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusOK, 0, protocol.ExecuteResponse{
		Status: "ok",
		Stdout: "scheduled\n",
	})

	reg := NewNodeRegistry()
	store := NewJobStore()
	srv := NewServerWithConfig(reg, store, client, DefaultDispatchConfig(), nil)
	mux := srv.Mux()

	job := submitCommandJob(t, mux, "echo", "scheduled")
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("expected no dispatch attempts before a healthy node exists, got %d", calls.Load())
	}
	queued, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get queued job: %v", err)
	}
	if queued.Status != JobStatusQueued {
		t.Fatalf("expected job to remain QUEUED, got %s", queued.Status)
	}

	registerNode(t, mux, "agent-scheduler", "agent.local:8081")
	srv.dispatchQueuedJobs()
	final := waitForJobStatus(t, mux, job.ID, JobStatusCompleted, 2*time.Second)

	if final.NodeID != "agent-scheduler" {
		t.Errorf("expected NodeID agent-scheduler, got %s", final.NodeID)
	}
	if final.Stdout != "scheduled\n" {
		t.Errorf("expected scheduler stdout to be recorded, got %q", final.Stdout)
	}
	if calls.Load() != 1 {
		t.Errorf("expected one scheduler dispatch attempt, got %d", calls.Load())
	}
}

func TestQueuedSchedulerExpiresOldQueuedJobs(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusOK, 0, protocol.ExecuteResponse{Status: "ok"})

	reg := NewNodeRegistry()
	store := NewJobStore()
	srv := NewServerWithConfig(reg, store, client, DefaultDispatchConfig(), nil)
	mux := srv.Mux()

	job := submitCommandJob(t, mux, "echo", "expired")
	now := time.Now().UTC()
	store.mu.Lock()
	store.jobs[job.ID].CreatedAt = now.Add(-25 * time.Hour)
	store.jobs[job.ID].UpdatedAt = now.Add(-25 * time.Hour)
	store.mu.Unlock()

	srv.dispatchQueuedJobs()

	final, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get expired job: %v", err)
	}
	if final.Status != JobStatusFailed {
		t.Fatalf("expected expired job to be FAILED, got %s", final.Status)
	}
	if final.LastError != QueuedJobExpiredError {
		t.Fatalf("expected queued expiration error, got %q", final.LastError)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no dispatch calls for expired job, got %d", calls.Load())
	}
	if got := srv.Metrics().JobsFailed.Load(); got != 1 {
		t.Fatalf("expected failed metric to include expired job, got %d", got)
	}
}

func TestDispatchSkipsDuplicateConcurrentJob(t *testing.T) {
	var calls atomic.Uint64
	var once sync.Once
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			once.Do(func() { close(requestStarted) })
			<-releaseRequest
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: time.Second, MaxAttempts: 1, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, client, cfg, nil)
	mux := srv.Mux()
	registerNode(t, mux, "agent-duplicate", "agent.local:8081")
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.dispatchJob(job.ID)
		close(done)
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for first dispatch attempt")
	}

	srv.dispatchJob(job.ID)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected duplicate dispatch to be skipped, got %d calls", got)
	}
	close(releaseRequest)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for dispatch to finish")
	}
	final, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	if final.Status != JobStatusCompleted || final.Attempts != 1 {
		t.Fatalf("expected one completed dispatch attempt, got %+v", final)
	}
}

func TestReportedResultCanWinConcurrentDispatchRaceWithoutDoubleCounting(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			close(requestStarted)
			<-releaseRequest
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok", Stdout: "sync\n"}), nil
		}),
	}

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: time.Second, MaxAttempts: 1, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, client, cfg, nil)
	mux := srv.Mux()
	registerNode(t, mux, "agent-race", "agent.local:8081")
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.dispatchJob(job.ID)
		close(done)
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for dispatch request")
	}

	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID: "agent-race",
		Status: string(JobStatusCompleted),
		Stdout: "reported\n",
	})
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected result report 200, got %d", w.Result().StatusCode)
	}
	close(releaseRequest)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for dispatch to finish")
	}

	final, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	if final.Status != JobStatusCompleted || final.Stdout != "reported\n" {
		t.Fatalf("expected reported result to win race, got %+v", final)
	}
	if got := srv.Metrics().JobsCompleted.Load(); got != 1 {
		t.Fatalf("expected one completed metric, got %d", got)
	}
	if got := srv.Metrics().ResultReportsAccepted.Load(); got != 1 {
		t.Fatalf("expected one accepted report metric, got %d", got)
	}
}

func TestDispatchSkipsNonQueuedJob(t *testing.T) {
	client, calls := startFakeAgentClient(t, http.StatusOK, 0, protocol.ExecuteResponse{Status: "ok"})

	reg := NewNodeRegistry()
	store := NewJobStore()
	srv := NewServerWithConfig(reg, store, client, DefaultDispatchConfig(), nil)
	mux := srv.Mux()
	registerNode(t, mux, "agent-stale", "agent.local:8081")

	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "agent-stale"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	srv.dispatchJob(job.ID)
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected stale non-queued dispatch to be skipped, got %d calls", got)
	}
}

func TestReconciliationGraceAcceptsReportedResultBeforeExpiry(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "agent-reconcile"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)
	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })

	if err := srv.StartReconciliationGrace(stopCh, []string{job.ID}, 50*time.Millisecond, RestartRecoveryError); err != nil {
		t.Fatalf("start reconciliation grace: %v", err)
	}
	if got := srv.Metrics().ReconciliationPendingJobs.Load(); got != 1 {
		t.Fatalf("expected one pending reconciliation job, got %d", got)
	}
	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID: "agent-reconcile",
		Status: string(JobStatusCompleted),
		Stdout: "recovered\n",
	})
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected report 200, got %d", w.Result().StatusCode)
	}
	time.Sleep(80 * time.Millisecond)

	final, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	if final.Status != JobStatusCompleted || final.Stdout != "recovered\n" {
		t.Fatalf("expected reported result to win grace, got %+v", final)
	}
	if got := srv.Metrics().StartupRecoveredJobs.Load(); got != 0 {
		t.Fatalf("expected no startup recovery failures, got %d", got)
	}
	if got := srv.Metrics().ReconciliationPendingJobs.Load(); got != 0 {
		t.Fatalf("expected reconciliation pending gauge to clear, got %d", got)
	}
}

func TestReconciliationGraceExpiresCapturedRunningJobs(t *testing.T) {
	store := NewJobStore()
	captured, err := store.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create captured job: %v", err)
	}
	newRunning, err := store.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create new running job: %v", err)
	}
	if _, err := store.StartAttempt(captured.ID, "agent-old"); err != nil {
		t.Fatalf("start captured job: %v", err)
	}
	if _, err := store.StartAttempt(newRunning.ID, "agent-new"); err != nil {
		t.Fatalf("start new running job: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)
	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })

	if err := srv.StartReconciliationGrace(stopCh, []string{captured.ID}, 10*time.Millisecond, RestartRecoveryError); err != nil {
		t.Fatalf("start reconciliation grace: %v", err)
	}
	if got := srv.Metrics().ReconciliationPendingJobs.Load(); got != 1 {
		t.Fatalf("expected one pending reconciliation job, got %d", got)
	}
	time.Sleep(40 * time.Millisecond)

	gotCaptured, _, err := store.Get(captured.ID)
	if err != nil {
		t.Fatalf("get captured job: %v", err)
	}
	if gotCaptured.Status != JobStatusFailed || gotCaptured.LastError != RestartRecoveryError {
		t.Fatalf("expected captured job to fail after grace, got %+v", gotCaptured)
	}
	gotNew, _, err := store.Get(newRunning.ID)
	if err != nil {
		t.Fatalf("get new running job: %v", err)
	}
	if gotNew.Status != JobStatusRunning {
		t.Fatalf("new running job should not be failed by startup grace, got %+v", gotNew)
	}
	if got := srv.Metrics().StartupRecoveredJobs.Load(); got != 1 {
		t.Fatalf("expected one startup recovery failure, got %d", got)
	}
	if got := srv.Metrics().ReconciliationPendingJobs.Load(); got != 0 {
		t.Fatalf("expected reconciliation pending gauge to clear, got %d", got)
	}
}

func TestReconciliationGraceZeroFailsImmediately(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "agent-zero"); err != nil {
		t.Fatalf("start job: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)
	stopCh := make(chan struct{})
	defer close(stopCh)

	if err := srv.StartReconciliationGrace(stopCh, []string{job.ID}, 0, RestartRecoveryError); err != nil {
		t.Fatalf("start reconciliation grace: %v", err)
	}
	got, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusFailed || got.LastError != RestartRecoveryError {
		t.Fatalf("expected immediate recovery failure, got %+v", got)
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

	schema := protocol.SchemaStatus{Ready: true, Version: 2, ExpectedVersion: 2}
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
	srv.Metrics().ResultReportsAccepted.Store(3)
	srv.Metrics().ResultReportsIgnored.Store(4)
	srv.Metrics().ReconciliationPendingJobs.Store(5)
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
		"planetary_job_result_reports_accepted_total 3",
		"planetary_job_result_reports_ignored_total 4",
		"planetary_jobs_reconciliation_pending 5",
		`planetary_nodes{state="HEALTHY"} 1`,
		"planetary_postgres_schema_ready 1",
		"planetary_postgres_schema_version 2",
		"planetary_postgres_schema_expected_version 2",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected metrics to contain %q, got:\n%s", want, text)
		}
	}
}
