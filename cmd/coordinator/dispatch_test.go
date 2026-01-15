package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDispatchJobSuccess(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create("echo", "hello")

	reg := NewNodeRegistry()

	// fake agent server that simulates /execute behavior
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute" {
			t.Errorf("expected path /execute, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
		}

		var req executeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode execute request: %v", err)
		}
		if req.JobID != job.ID {
			t.Errorf("expected job ID %s, got %s", job.ID, req.JobID)
		}
		if req.Type != job.Type {
			t.Errorf("expected job type %s, got %s", job.Type, req.Type)
		}
		if req.Payload != job.Payload {
			t.Errorf("expected job payload %s, got %s", job.Payload, req.Payload)
		}

		called = true
		w.WriteHeader(http.StatusOK)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	// seed the registry with one healthy node pointing at the fake agent.
	reg.mu.Lock()
	reg.nodes["node-1"] = &Node{
		ID:       "node-1",
		Address:  u.Host, // host:port
		LastSeen: time.Now().UTC(),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	srv := &server{
		registry:   reg,
		jobs:       jobStore,
		httpClient: ts.Client(),
	}

	// run dispatcher synchronously (in production it's run as a goroutine).
	srv.dispatchJob(job.ID)

	if !called {
		t.Fatalf("expected fake agent to be called, but it was not")
	}

	// verify job has been updated to COMPLETED with the right NodeID.
	jobs := jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	updated := jobs[0]
	if updated.Status != JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", updated.Status)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected job NodeID node-1, got %s", updated.NodeID)
	}
}

func TestDispatchJobNoHealthyNodes(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create("echo", "hello")

	reg := NewNodeRegistry()
	// no nodes registered at all => no healthy nodes.

	srv := &server{
		registry: reg,
		jobs:     jobStore,
		// httpClient nil is fine; dispatchJob won't use it if no healthy nodes.
	}

	// run dispatcher.
	srv.dispatchJob(job.ID)

	// job should remain QUEUED because there was no node to dispatch to.
	jobs := jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	unchanged := jobs[0]
	if unchanged.Status != JobStatusQueued {
		t.Fatalf("expected job status QUEUED, got %s", unchanged.Status)
	}
	if unchanged.NodeID != "" {
		t.Fatalf("expected empty NodeID, got %s", unchanged.NodeID)
	}
}

func TestDispatchJobTimeout(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create("echo", "hello")

	reg := NewNodeRegistry()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	reg.mu.Lock()
	reg.nodes["node-1"] = &Node{
		ID:       "node-1",
		Address:  u.Host,
		LastSeen: time.Now().UTC(),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	httpClient := &http.Client{Timeout: 50 * time.Millisecond}

	srv := &server{
		registry:   reg,
		jobs:       jobStore,
		httpClient: httpClient,
	}

	srv.dispatchJob(job.ID)

	jobs := jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	updated := jobs[0]
	if updated.Status != JobStatusFailed {
		t.Fatalf("expected job status FAILED due to timeout, got %s", updated.Status)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected job NodeID node-1, got %s", updated.NodeID)
	}
}

func TestDispatchJobRetriesThenSucceeds(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create("echo", "hello")

	reg := NewNodeRegistry()

	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	reg.mu.Lock()
	reg.nodes["node-1"] = &Node{
		ID:       "node-1",
		Address:  u.Host,
		LastSeen: time.Now().UTC(),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	srv := &server{
		registry: reg,
		jobs:     jobStore,
	}

	srv.dispatchJob(job.ID)

	if calls != 2 {
		t.Fatalf("expected 2 calls (retry once), got %d", calls)
	}

	jobs := jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	updated := jobs[0]
	if updated.Status != JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED after retry, got %s", updated.Status)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected job NodeID node-1, got %s", updated.NodeID)
	}
}

func TestDispatchJobRetriesAndFails(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create("echo", "hello")

	reg := NewNodeRegistry()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	reg.mu.Lock()
	reg.nodes["node-1"] = &Node{
		ID:       "node-1",
		Address:  u.Host,
		LastSeen: time.Now().UTC(),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	srv := &server{
		registry: reg,
		jobs:     jobStore,
	}

	srv.dispatchJob(job.ID)

	jobs := jobStore.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	updated := jobs[0]
	if updated.Status != JobStatusFailed {
		t.Fatalf("expected job status FAILED after retries, got %s", updated.Status)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected job NodeID node-1, got %s", updated.NodeID)
	}
}
