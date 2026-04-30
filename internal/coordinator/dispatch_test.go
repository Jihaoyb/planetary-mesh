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

func TestDispatchJobSuccess(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"hello"}})

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

	updated, _ := jobStore.Get(job.ID)
	if updated.Status != JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", updated.Status)
	}
	if updated.Stdout != "hello\n" {
		t.Fatalf("expected stdout to be captured, got %q", updated.Stdout)
	}
}

func TestDispatchJobNoHealthyNodes(t *testing.T) {
	jobStore := NewJobStore()
	job := jobStore.Create(JobCreateInput{Type: "command", Command: "echo"})

	reg := NewNodeRegistry()
	srv := NewServer(reg, jobStore, nil)

	srv.dispatchJob(job.ID)

	unchanged, _ := jobStore.Get(job.ID)
	if unchanged.Status != JobStatusQueued {
		t.Fatalf("expected job status QUEUED, got %s", unchanged.Status)
	}
}
