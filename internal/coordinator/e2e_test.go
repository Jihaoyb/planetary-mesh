package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startFakeAgent returns an httptest.Server that handles /execute and lets
// the test inspect how many times it has been called.
func startFakeAgent(t *testing.T, status int, delay time.Duration) (*httptest.Server, *atomic.Uint64) {
	t.Helper()
	var calls atomic.Uint64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// registerNode is a helper that POSTs /register to the given coordinator mux.
func registerNode(t *testing.T, mux http.Handler, id, addr string) {
	t.Helper()
	body, _ := json.Marshal(registerRequest{ID: id, Address: addr})
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("register failed: %d", w.Result().StatusCode)
	}
}

// submitJob POSTs /jobs and returns the created job.
func submitJob(t *testing.T, mux http.Handler, jobType, payload string) Job {
	t.Helper()
	body, _ := json.Marshal(createJobRequest{Type: jobType, Payload: payload})
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
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

// waitForJobStatus polls GET /jobs/{id} until the status matches or the
// deadline expires. Returns the final job snapshot.
func waitForJobStatus(t *testing.T, mux http.Handler, jobID string, want JobStatus, timeout time.Duration) Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/jobs/"+jobID, nil)
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

// TestEndToEndJobLifecycle exercises register -> submit -> dispatch -> COMPLETED
// against a fake agent, going through the real HTTP mux.
func TestEndToEndJobLifecycle(t *testing.T) {
	fakeAgent, calls := startFakeAgent(t, http.StatusOK, 0)

	reg := NewNodeRegistry()
	store := NewJobStore()
	srv := NewServerWithConfig(reg, store, fakeAgent.Client(), DefaultDispatchConfig(), nil)
	mux := srv.Mux()

	u, _ := url.Parse(fakeAgent.URL)
	registerNode(t, mux, "agent-e2e", u.Host)

	job := submitJob(t, mux, "echo", "hello e2e")
	final := waitForJobStatus(t, mux, job.ID, JobStatusCompleted, 2*time.Second)

	if final.NodeID != "agent-e2e" {
		t.Errorf("expected NodeID agent-e2e, got %s", final.NodeID)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 agent call, got %d", calls.Load())
	}
	if got := srv.metrics.JobsCompleted.Load(); got != 1 {
		t.Errorf("expected 1 completed metric, got %d", got)
	}
}

// TestEndToEndNoHealthyNodes verifies that submitting a job with no nodes
// leaves it QUEUED and does not flip metrics.
func TestEndToEndNoHealthyNodes(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	mux := srv.Mux()

	job := submitJob(t, mux, "echo", "no-nodes")

	// Give the dispatch goroutine a chance to run.
	time.Sleep(50 * time.Millisecond)

	got, ok := srv.jobs.Get(job.ID)
	if !ok {
		t.Fatalf("job missing")
	}
	if got.Status != JobStatusQueued {
		t.Errorf("expected QUEUED, got %s", got.Status)
	}
	if srv.metrics.JobsCompleted.Load() != 0 || srv.metrics.JobsFailed.Load() != 0 {
		t.Errorf("expected no completion/failure metrics, got %d/%d",
			srv.metrics.JobsCompleted.Load(), srv.metrics.JobsFailed.Load())
	}
}

// TestDispatchRetriesOn500ThenFails verifies that a 500-returning agent
// is retried up to MaxAttempts and then the job is marked FAILED.
func TestDispatchRetriesOn500ThenFails(t *testing.T) {
	fakeAgent, calls := startFakeAgent(t, http.StatusInternalServerError, 0)

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{
		Timeout:     500 * time.Millisecond,
		MaxAttempts: 3,
		BaseBackoff: 1 * time.Millisecond,
	}
	srv := NewServerWithConfig(reg, store, fakeAgent.Client(), cfg, nil)
	mux := srv.Mux()

	u, _ := url.Parse(fakeAgent.URL)
	registerNode(t, mux, "agent-flaky", u.Host)

	job := submitJob(t, mux, "echo", "boom")
	final := waitForJobStatus(t, mux, job.ID, JobStatusFailed, 2*time.Second)

	if final.NodeID != "agent-flaky" {
		t.Errorf("expected NodeID agent-flaky, got %s", final.NodeID)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 dispatch attempts, got %d", got)
	}
	if got := srv.metrics.DispatchErrors.Load(); got != 3 {
		t.Errorf("expected 3 dispatch_errors metric, got %d", got)
	}
	if got := srv.metrics.JobsFailed.Load(); got != 1 {
		t.Errorf("expected 1 jobs_failed metric, got %d", got)
	}
}

// TestDispatchNoRetryOn400 verifies that a 4xx response is not retried.
func TestDispatchNoRetryOn400(t *testing.T) {
	fakeAgent, calls := startFakeAgent(t, http.StatusBadRequest, 0)

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 3, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(reg, store, fakeAgent.Client(), cfg, nil)
	mux := srv.Mux()

	u, _ := url.Parse(fakeAgent.URL)
	registerNode(t, mux, "agent-400", u.Host)

	job := submitJob(t, mux, "echo", "bad")
	waitForJobStatus(t, mux, job.ID, JobStatusFailed, 1*time.Second)

	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 attempt for 4xx, got %d", calls.Load())
	}
}

// TestDispatchTimeoutTriggersRetry verifies that an agent that hangs longer
// than the per-attempt timeout produces a retry.
func TestDispatchTimeoutTriggersRetry(t *testing.T) {
	fakeAgent, calls := startFakeAgent(t, http.StatusOK, 200*time.Millisecond)

	reg := NewNodeRegistry()
	store := NewJobStore()
	cfg := DispatchConfig{
		Timeout:     20 * time.Millisecond, // shorter than agent delay
		MaxAttempts: 2,
		BaseBackoff: time.Millisecond,
	}
	srv := NewServerWithConfig(reg, store, fakeAgent.Client(), cfg, nil)
	mux := srv.Mux()

	u, _ := url.Parse(fakeAgent.URL)
	registerNode(t, mux, "agent-slow", u.Host)

	job := submitJob(t, mux, "echo", "slow")
	waitForJobStatus(t, mux, job.ID, JobStatusFailed, 2*time.Second)

	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts due to timeout retries, got %d", got)
	}
}

// TestStaleHeartbeatTransitionsState directly drives UpdateHealthStates to
// confirm a node moves from HEALTHY -> SUSPECT -> OFFLINE as heartbeats stale.
// This complements the unit test by going through the registry the same way
// the background checker does.
func TestStaleHeartbeatTransitionsState(t *testing.T) {
	reg := NewNodeRegistry()
	reg.Register("n", ":1")

	// freshly registered: HEALTHY
	n := reg.List()[0]
	if n.State != NodeStateHealthy {
		t.Fatalf("expected HEALTHY initially, got %s", n.State)
	}

	// pretend 20s passed -> SUSPECT
	reg.UpdateHealthStates(time.Now().Add(20*time.Second).UTC(), 15*time.Second, 30*time.Second)
	if got := reg.List()[0].State; got != NodeStateSuspect {
		t.Errorf("expected SUSPECT, got %s", got)
	}

	// pretend 40s passed -> OFFLINE
	reg.UpdateHealthStates(time.Now().Add(40*time.Second).UTC(), 15*time.Second, 30*time.Second)
	if got := reg.List()[0].State; got != NodeStateOffline {
		t.Errorf("expected OFFLINE, got %s", got)
	}
}

// TestMetricsEndpointExposesCounters submits one job, lets it complete, and
// verifies /metrics contains the expected counter lines.
func TestMetricsEndpointExposesCounters(t *testing.T) {
	fakeAgent, _ := startFakeAgent(t, http.StatusOK, 0)

	srv := NewServerWithConfig(NewNodeRegistry(), NewJobStore(), fakeAgent.Client(), DefaultDispatchConfig(), nil)
	mux := srv.Mux()

	u, _ := url.Parse(fakeAgent.URL)
	registerNode(t, mux, "agent-metrics", u.Host)
	job := submitJob(t, mux, "echo", "metrics")
	waitForJobStatus(t, mux, job.ID, JobStatusCompleted, 2*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	text := string(body)
	for _, want := range []string{
		"planetary_jobs_created_total 1",
		"planetary_jobs_completed_total 1",
		`planetary_nodes{state="HEALTHY"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected metrics to contain %q, got:\n%s", want, text)
		}
	}
}
