package main

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
	srv := &server{
		registry: reg,
		jobs:     jobStore,
	}

	// create a job via POST /jobs
	createPayload := createJobRequest{
		Type:    "echo",
		Payload: "hello jobs",
	}
	bodyBytes, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyBytes))
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

	if jobResp.ID == "" {
		t.Fatalf("expected non-empty job ID")
	}
	if jobResp.Type != "echo" {
		t.Fatalf("expected type echo, got %s", jobResp.Type)
	}
	if jobResp.Payload != "hello jobs" {
		t.Fatalf("expected payload hello jobs, got %s", jobResp.Payload)
	}
	if jobResp.Status != JobStatusQueued {
		t.Fatalf("expected status QUEUED, got %s", jobResp.Status)
	}

	// list jobs via GET /jobs
	reqList := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	wList := httptest.NewRecorder()

	srv.handleJobs(wList, reqList)

	resList := wList.Result()
	defer resList.Body.Close()

	if resList.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from /jobs, got %d", resList.StatusCode)
	}

	var jobs []Job
	if err := json.NewDecoder(resList.Body).Decode(&jobs); err != nil {
		t.Fatalf("failed to decode jobs response: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != jobResp.ID {
		t.Fatalf("expected job ID %s in list, got %s", jobResp.ID, jobs[0].ID)
	}
}
