package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

type staticNodeStore struct {
	nodes     []Node
	listCalls *int
}

func (s staticNodeStore) Register(NodeRegistration) (Node, error) {
	return Node{}, nil
}

func (s staticNodeStore) List() ([]Node, error) {
	if s.listCalls != nil {
		*s.listCalls = *s.listCalls + 1
	}
	nodes := make([]Node, len(s.nodes))
	copy(nodes, s.nodes)
	return nodes, nil
}

func (s staticNodeStore) UpdateHealthStates(time.Time, time.Duration, time.Duration) error {
	return nil
}

func (s staticNodeStore) CountByState() (NodeStateCounts, error) {
	var counts NodeStateCounts
	for _, node := range s.nodes {
		switch node.State {
		case NodeStateHealthy:
			counts.Healthy++
		case NodeStateSuspect:
			counts.Suspect++
		case NodeStateOffline:
			counts.Offline++
		}
	}
	return counts, nil
}

func TestDispatchJobSuccess(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	reg := NewNodeRegistry()

	var called bool
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if !protocol.HasExpectedVersion(r.Header) {
				t.Errorf("expected protocol header")
			}

			var req protocol.ExecuteRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode execute request: %v", err)
			}
			if req.Command != "echo" {
				t.Errorf("expected command echo, got %s", req.Command)
			}

			called = true
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{
				Status: "ok",
				Stdout: "hello\n",
			}), nil
		}),
	}

	reg.mu.Lock()
	reg.nodes["node-1"] = &Node{
		ID:       "node-1",
		Address:  "agent.local:8081",
		LastSeen: time.Now().UTC(),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	srv := NewServer(reg, jobStore, client)
	srv.dispatchJob(job.ID)

	if !called {
		t.Fatalf("expected fake agent to be called, but it was not")
	}

	updated, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updated.Status != JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", updated.Status)
	}
	if updated.Stdout != "hello\n" {
		t.Fatalf("expected stdout to be captured, got %q", updated.Stdout)
	}
}

func TestDispatchJobNoHealthyNodes(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	reg := NewNodeRegistry()
	srv := NewServer(reg, jobStore, nil)

	srv.dispatchJob(job.ID)

	unchanged, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if unchanged.Status != JobStatusQueued {
		t.Fatalf("expected job status QUEUED, got %s", unchanged.Status)
	}
	if unchanged.Attempts != 0 || unchanged.NodeID != "" || unchanged.StartedAt != nil || unchanged.CompletedAt != nil {
		t.Fatalf("expected no healthy nodes to leave job unattempted, got %+v", unchanged)
	}
}

func TestDispatchReassignsAfterRetryableNodeFailures(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{
		Type:                 "command",
		Command:              "echo",
		Args:                 []string{"hello"},
		RequiredCapabilities: []string{"role:worker"},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	calls := map[string]int{}
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls[r.URL.Host]++
			switch r.URL.Host {
			case "node-a.local:8081":
				return jsonResponse(http.StatusInternalServerError, protocol.ExecuteResponse{
					Status:    "error",
					LastError: "node-a retryable failure",
				}), nil
			case "node-b.local:8081":
				return jsonResponse(http.StatusOK, protocol.ExecuteResponse{
					Status: "ok",
					Stdout: "node-b completed\n",
				}), nil
			default:
				t.Fatalf("unexpected agent host %q", r.URL.Host)
				return nil, nil
			}
		}),
	}
	listCalls := 0
	nodes := staticNodeStore{nodes: []Node{
		{ID: "node-offline", Address: "node-offline.local:8081", State: NodeStateOffline},
		{ID: "node-nonmatching", Address: "node-nonmatching.local:8081", State: NodeStateHealthy},
		{ID: "node-a", Address: "node-a.local:8081", State: NodeStateHealthy, Capabilities: []string{"role:worker"}},
		{ID: "node-b", Address: "node-b.local:8081", State: NodeStateHealthy, Capabilities: []string{"role:worker"}},
	}, listCalls: &listCalls}
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 2, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(nodes, jobStore, client, cfg, nil)

	srv.dispatchJob(job.ID)

	final, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if final.Status != JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", final.Status)
	}
	if final.NodeID != "node-b" {
		t.Fatalf("expected final node node-b, got %q", final.NodeID)
	}
	if final.Attempts != 3 {
		t.Fatalf("expected 3 total attempts, got %d", final.Attempts)
	}
	if final.Stdout != "node-b completed\n" {
		t.Fatalf("expected node-b stdout, got %q", final.Stdout)
	}
	if calls["node-a.local:8081"] != 2 {
		t.Fatalf("expected node-a to exhaust 2 attempts, got %d", calls["node-a.local:8081"])
	}
	if calls["node-b.local:8081"] != 1 {
		t.Fatalf("expected node-b to be called once, got %d", calls["node-b.local:8081"])
	}
	if calls["node-offline.local:8081"] != 0 {
		t.Fatalf("expected offline node to be skipped, got %d calls", calls["node-offline.local:8081"])
	}
	if calls["node-nonmatching.local:8081"] != 0 {
		t.Fatalf("expected nonmatching node to be skipped, got %d calls", calls["node-nonmatching.local:8081"])
	}
	if listCalls != 1 {
		t.Fatalf("expected one frozen node snapshot, got %d list calls", listCalls)
	}
}

func TestDispatchPrefersLowestReportedActiveExecutionsForUnconstrainedJob(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	var calledHost string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calledHost = r.URL.Host
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{
			ID:      "node-a",
			Address: "node-a.local:8081",
			State:   NodeStateHealthy,
			Load:    protocol.NodeLoad{ActiveExecutions: 99},
		},
		{
			ID:           "node-b",
			Address:      "node-b.local:8081",
			State:        NodeStateHealthy,
			Capabilities: []string{"role:worker"},
			Load:         protocol.NodeLoad{ActiveExecutions: 0},
		},
	}}
	srv := NewServerWithConfig(nodes, jobStore, client, DispatchConfig{Timeout: time.Second, MaxAttempts: 1, BaseBackoff: time.Millisecond}, nil)

	srv.dispatchJob(job.ID)

	if calledHost != "node-b.local:8081" {
		t.Fatalf("expected least-active node to be selected, got %q", calledHost)
	}
}

func TestDispatchBreaksLoadTiesByNodeID(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	var calledHost string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calledHost = r.URL.Host
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{ID: "node-b", Address: "node-b.local:8081", State: NodeStateHealthy, Load: protocol.NodeLoad{ActiveExecutions: 2}},
		{ID: "node-a", Address: "node-a.local:8081", State: NodeStateHealthy, Load: protocol.NodeLoad{ActiveExecutions: 2}},
	}}
	srv := NewServerWithConfig(nodes, jobStore, client, DispatchConfig{Timeout: time.Second, MaxAttempts: 1}, nil)

	srv.dispatchJob(job.ID)

	if calledHost != "node-a.local:8081" {
		t.Fatalf("expected node ID tie-breaker to select node-a, got %q", calledHost)
	}
}

func TestDispatchRequiresAllCapabilitiesAndHealthyState(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{
		Type:                 "command",
		Command:              "text-stats",
		RequiredCapabilities: []string{"profile:local", "role:text-worker"},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	var calledHost string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calledHost = r.URL.Host
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{
			ID:           "node-one-label",
			Address:      "node-one-label.local:8081",
			State:        NodeStateHealthy,
			Capabilities: []string{"role:text-worker"},
			Load:         protocol.NodeLoad{ActiveExecutions: 0},
		},
		{
			ID:           "node-suspect",
			Address:      "node-suspect.local:8081",
			State:        NodeStateSuspect,
			Capabilities: []string{"profile:local", "role:text-worker"},
			Load:         protocol.NodeLoad{ActiveExecutions: 0},
		},
		{
			ID:           "node-match",
			Address:      "node-match.local:8081",
			State:        NodeStateHealthy,
			Capabilities: []string{"profile:local", "role:text-worker"},
			Load:         protocol.NodeLoad{ActiveExecutions: 8},
		},
	}}
	srv := NewServerWithConfig(nodes, jobStore, client, DispatchConfig{Timeout: time.Second, MaxAttempts: 1}, nil)

	srv.dispatchJob(job.ID)

	if calledHost != "node-match.local:8081" {
		t.Fatalf("expected all-of healthy match, got %q", calledHost)
	}
}

func TestDispatchLeavesConstrainedJobQueuedWithoutMatchingNode(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{
		Type:                 "command",
		Command:              "echo",
		RequiredCapabilities: []string{"role:gpu-worker"},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	calls := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{ID: "node-healthy", Address: "node-healthy.local:8081", State: NodeStateHealthy, Capabilities: []string{"role:cpu-worker"}},
	}}
	srv := NewServerWithConfig(nodes, jobStore, client, DispatchConfig{Timeout: time.Second, MaxAttempts: 1}, nil)

	srv.dispatchJob(job.ID)

	final, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if final.Status != JobStatusQueued || final.Attempts != 0 || final.NodeID != "" || final.StartedAt != nil {
		t.Fatalf("expected constrained job to remain unattempted and queued, got %+v", final)
	}
	if calls != 0 {
		t.Fatalf("expected no agent contacts, got %d", calls)
	}
}

func TestDispatchUsesCapabilitiesAddedByLaterHeartbeat(t *testing.T) {
	registry := NewNodeRegistry()
	if _, err := registry.Register(NodeRegistration{
		ID:           "node-1",
		Address:      "node-1.local:8081",
		Capabilities: []string{"profile:local"},
	}); err != nil {
		t.Fatalf("register initial node: %v", err)
	}
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{
		Type:                 "command",
		Command:              "echo",
		RequiredCapabilities: []string{"role:text-worker"},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	calls := 0
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return jsonResponse(http.StatusOK, protocol.ExecuteResponse{Status: "ok"}), nil
		}),
	}
	srv := NewServerWithConfig(registry, jobStore, client, DispatchConfig{Timeout: time.Second, MaxAttempts: 1}, nil)

	srv.dispatchJob(job.ID)
	if calls != 0 {
		t.Fatalf("expected no initial dispatch, got %d calls", calls)
	}
	if _, err := registry.Register(NodeRegistration{
		ID:           "node-1",
		Address:      "node-1.local:8081",
		Capabilities: []string{"profile:local", "role:text-worker"},
	}); err != nil {
		t.Fatalf("register matching heartbeat: %v", err)
	}
	srv.dispatchJob(job.ID)

	final, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if final.Status != JobStatusCompleted || final.NodeID != "node-1" || final.Attempts != 1 {
		t.Fatalf("expected later matching heartbeat to enable dispatch, got %+v", final)
	}
	if calls != 1 {
		t.Fatalf("expected one dispatch after matching heartbeat, got %d", calls)
	}
}

func TestDispatchFailsAfterAllHealthyNodesFailRetryably(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	calls := map[string]int{}
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls[r.URL.Host]++
			return jsonResponse(http.StatusInternalServerError, protocol.ExecuteResponse{
				Status:    "error",
				LastError: r.URL.Host + " retryable failure",
			}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{ID: "node-a", Address: "node-a.local:8081", State: NodeStateHealthy},
		{ID: "node-b", Address: "node-b.local:8081", State: NodeStateHealthy},
	}}
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 2, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(nodes, jobStore, client, cfg, nil)

	srv.dispatchJob(job.ID)

	final, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if final.Status != JobStatusFailed {
		t.Fatalf("expected job status FAILED, got %s", final.Status)
	}
	if final.NodeID != "node-b" {
		t.Fatalf("expected final node node-b, got %q", final.NodeID)
	}
	if final.Attempts != 4 {
		t.Fatalf("expected 4 total attempts, got %d", final.Attempts)
	}
	if final.LastError != "node-b.local:8081 retryable failure" {
		t.Fatalf("expected last retryable error from node-b, got %q", final.LastError)
	}
	if calls["node-a.local:8081"] != 2 || calls["node-b.local:8081"] != 2 {
		t.Fatalf("expected each healthy node to receive 2 attempts, got calls=%v", calls)
	}
	if got := srv.Metrics().DispatchAttempts.Load(); got != 4 {
		t.Fatalf("expected 4 dispatch attempts metric, got %d", got)
	}
	if got := srv.Metrics().DispatchErrors.Load(); got != 4 {
		t.Fatalf("expected 4 dispatch errors metric, got %d", got)
	}
	if got := srv.Metrics().JobsFailed.Load(); got != 1 {
		t.Fatalf("expected 1 failed job metric, got %d", got)
	}
}

func TestDispatchDoesNotReassignTerminalFailure(t *testing.T) {
	jobStore := NewJobStore()
	job, err := jobStore.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	exitCode := 2
	calls := map[string]int{}
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls[r.URL.Host]++
			if r.URL.Host != "node-a.local:8081" {
				t.Fatalf("terminal failure should not reassign to %q", r.URL.Host)
			}
			return jsonResponse(http.StatusUnprocessableEntity, protocol.ExecuteResponse{
				Status:    "error",
				ExitCode:  &exitCode,
				Stderr:    "boom\n",
				LastError: "command exited with code 2",
			}), nil
		}),
	}
	nodes := staticNodeStore{nodes: []Node{
		{ID: "node-a", Address: "node-a.local:8081", State: NodeStateHealthy},
		{ID: "node-b", Address: "node-b.local:8081", State: NodeStateHealthy},
	}}
	cfg := DispatchConfig{Timeout: 500 * time.Millisecond, MaxAttempts: 3, BaseBackoff: time.Millisecond}
	srv := NewServerWithConfig(nodes, jobStore, client, cfg, nil)

	srv.dispatchJob(job.ID)

	final, _, err := jobStore.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if final.Status != JobStatusFailed {
		t.Fatalf("expected job status FAILED, got %s", final.Status)
	}
	if final.NodeID != "node-a" {
		t.Fatalf("expected final node node-a, got %q", final.NodeID)
	}
	if final.Attempts != 1 {
		t.Fatalf("expected 1 attempt for terminal failure, got %d", final.Attempts)
	}
	if final.ExitCode == nil || *final.ExitCode != 2 {
		t.Fatalf("expected exit code 2, got %#v", final.ExitCode)
	}
	if calls["node-a.local:8081"] != 1 || calls["node-b.local:8081"] != 0 {
		t.Fatalf("expected only node-a to be called once, got calls=%v", calls)
	}
}
