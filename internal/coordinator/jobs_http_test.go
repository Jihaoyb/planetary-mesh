package coordinator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleJobsCreateAndList(t *testing.T) {
	reg := NewNodeRegistry()
	jobStore := NewJobStore()
	srv := NewServer(reg, jobStore, nil)

	createPayload := createJobRequest{
		Type:    "command",
		Command: "echo",
		Args:    []string{"hello jobs"},
	}
	bodyBytes, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := newVersionedRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleJobs(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.StatusCode)
	}

	var jobResp Job
	if err := json.NewDecoder(res.Body).Decode(&jobResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if jobResp.Command != "echo" {
		t.Fatalf("expected command echo, got %s", jobResp.Command)
	}
	if jobResp.Status != JobStatusQueued || jobResp.NodeID != "" || jobResp.Attempts != 0 || jobResp.StartedAt != nil || jobResp.CompletedAt != nil {
		t.Fatalf("new job should be returned as queued and unattempted, got %+v", jobResp)
	}

	reqList := newVersionedRequest(http.MethodGet, "/jobs", nil)
	wList := httptest.NewRecorder()
	srv.handleJobs(wList, reqList)

	if wList.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from /jobs, got %d", wList.Result().StatusCode)
	}
}

func TestHandleJobByID(t *testing.T) {
	reg := NewNodeRegistry()
	jobStore := NewJobStore()
	srv := NewServer(reg, jobStore, nil)

	created, err := jobStore.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"hello detail"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	req := newVersionedRequest(http.MethodGet, "/jobs/"+created.ID, nil)
	w := httptest.NewRecorder()
	srv.handleJobByID(w, req)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var got Job
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != created.ID || got.Command != "echo" {
		t.Fatalf("unexpected job: %+v", got)
	}
}

func TestHandleCommandJobRejectsPayload(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)

	createPayload := createJobRequest{
		Type:    "command",
		Command: "echo",
		Payload: "should fail",
	}
	bodyBytes, _ := json.Marshal(createPayload)
	req := newVersionedRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleJobs(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}
