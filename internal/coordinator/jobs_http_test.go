package coordinator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"planetary-mesh/internal/protocol"
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

func TestHandleCreateCommandJobCanonicalizesRequirements(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	bodyBytes, err := json.Marshal(createCommandJobRequest{
		Type:                 "command",
		Command:              "text-stats",
		Args:                 []string{"input.txt"},
		RequiredCapabilities: []string{" role:text-worker ", "profile:local", "role:text-worker"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := newVersionedRequest(http.MethodPost, "/jobs/command", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Mux().ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Result().StatusCode, w.Body.String())
	}
	var got Job
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if len(got.RequiredCapabilities) != 2 ||
		got.RequiredCapabilities[0] != "profile:local" ||
		got.RequiredCapabilities[1] != "role:text-worker" {
		t.Fatalf("unexpected requirements: %+v", got.RequiredCapabilities)
	}
}

func TestHandleCreateCommandJobNormalizesEmptyRequirements(t *testing.T) {
	for _, body := range []string{
		`{"type":"command","command":"echo"}`,
		`{"type":"command","command":"echo","required_capabilities":null}`,
		`{"type":"command","command":"echo","required_capabilities":[]}`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
			req := newVersionedRequest(http.MethodPost, "/jobs/command", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusCreated {
				t.Fatalf("expected 201, got %d: %s", w.Result().StatusCode, w.Body.String())
			}
			var raw map[string]any
			if err := json.NewDecoder(w.Result().Body).Decode(&raw); err != nil {
				t.Fatalf("decode job: %v", err)
			}
			requirements, ok := raw["required_capabilities"].([]any)
			if !ok || len(requirements) != 0 {
				t.Fatalf("expected required_capabilities [], got %#v", raw["required_capabilities"])
			}
		})
	}
}

func TestHandleJobsRejectsPlacementFieldOnLegacyEndpoint(t *testing.T) {
	for _, value := range []string{"null", "[]", `["role:worker"]`} {
		t.Run(value, func(t *testing.T) {
			store := NewJobStore()
			srv := NewServer(NewNodeRegistry(), store, nil)
			body := `{"type":"command","command":"echo","required_capabilities":` + value + `}`
			req := newVersionedRequest(http.MethodPost, "/jobs", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Result().StatusCode)
			}
			if got := w.Body.String(); got != "required_capabilities are only supported by POST /jobs/command\n" {
				t.Fatalf("unexpected error: %q", got)
			}
			jobs, err := store.List()
			if err != nil {
				t.Fatalf("list jobs: %v", err)
			}
			if len(jobs) != 0 {
				t.Fatalf("legacy endpoint created a constrained job: %+v", jobs)
			}
		})
	}
}

func TestHandleCreateCommandJobRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing type", body: `{"command":"echo"}`, want: "type is required\n"},
		{name: "wrong type", body: `{"type":"other","command":"echo"}`, want: "type must be command for POST /jobs/command\n"},
		{name: "payload", body: `{"type":"command","command":"echo","payload":"no"}`, want: "payload is not supported for type=command\n"},
		{
			name: "malformed requirement",
			body: `{"type":"command","command":"echo","required_capabilities":["-bad"]}`,
			want: "invalid required_capabilities: invalid required capability \"-bad\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
			req := newVersionedRequest(http.MethodPost, "/jobs/command", strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Result().StatusCode)
			}
			if got := w.Body.String(); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestHandleJobResultAcceptsCompletedReport(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)

	exitCode := 0
	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID:   "node-1",
		Status:   string(JobStatusCompleted),
		ExitCode: &exitCode,
		Stdout:   "reported\n",
	})
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	var got Job
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if got.Status != JobStatusCompleted || got.Stdout != "reported\n" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("unexpected reported job: %+v", got)
	}
	if metric := srv.Metrics().JobsCompleted.Load(); metric != 1 {
		t.Fatalf("expected completed metric 1, got %d", metric)
	}
	if metric := srv.Metrics().ResultReportsAccepted.Load(); metric != 1 {
		t.Fatalf("expected accepted result report metric 1, got %d", metric)
	}
}

func TestHandleJobResultDuplicateTerminalReturnsCurrentJob(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	if _, err := store.Complete(job.ID, "node-1", JobResult{Stdout: "original\n"}); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)

	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID:    "node-1",
		Status:    string(JobStatusFailed),
		LastError: "overwrite",
	})
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	var got Job
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if got.Status != JobStatusCompleted || got.Stdout != "original\n" || got.LastError != "" {
		t.Fatalf("terminal result was mutated: %+v", got)
	}
	if metric := srv.Metrics().JobsCompleted.Load(); metric != 0 {
		t.Fatalf("duplicate report should not increment metrics, got completed=%d", metric)
	}
	if metric := srv.Metrics().ResultReportsIgnored.Load(); metric != 1 {
		t.Fatalf("expected ignored result report metric 1, got %d", metric)
	}
}

func TestHandleJobResultRejectsWrongNode(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)

	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID: "node-2",
		Status: string(JobStatusCompleted),
		Stdout: "wrong\n",
	})
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
	got, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusRunning || got.NodeID != "node-1" || got.Stdout != "" {
		t.Fatalf("wrong-node report mutated job: %+v", got)
	}
	if metric := srv.Metrics().ResultReportsIgnored.Load(); metric != 1 {
		t.Fatalf("expected ignored result report metric 1, got %d", metric)
	}
}

func TestHandleJobResultRejectsStaleReportAfterReassignment(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-a"); err != nil {
		t.Fatalf("start node-a attempt: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-b"); err != nil {
		t.Fatalf("start node-b attempt: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)

	w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
		NodeID: "node-a",
		Status: string(JobStatusCompleted),
		Stdout: "stale\n",
	})
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected stale report 409, got %d", w.Result().StatusCode)
	}
	got, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusRunning || got.NodeID != "node-b" || got.Stdout != "" {
		t.Fatalf("stale report mutated reassigned job: %+v", got)
	}
}

func TestHandleJobResultRejectsUnknownJob(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	w := postJobResult(t, srv, "job-missing", protocol.JobResultReportRequest{
		NodeID: "node-1",
		Status: string(JobStatusCompleted),
	})
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestHandleJobResultRejectsUnsupportedStatus(t *testing.T) {
	store := NewJobStore()
	job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(job.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	srv := NewServer(NewNodeRegistry(), store, nil)

	for _, status := range []JobStatus{JobStatusQueued, JobStatusRunning, JobStatusCancelled, JobStatus("PAUSED")} {
		w := postJobResult(t, srv, job.ID, protocol.JobResultReportRequest{
			NodeID: "node-1",
			Status: string(status),
		})
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for status %s, got %d", status, w.Result().StatusCode)
		}
	}
	got, _, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusRunning || got.CompletedAt != nil {
		t.Fatalf("unsupported status report mutated job: %+v", got)
	}
	if metric := srv.Metrics().ResultReportsIgnored.Load(); metric != uint64(4) {
		t.Fatalf("expected ignored result report metric 4, got %d", metric)
	}
}

func TestHandleJobResultRequiresProtocolVersion(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	bodyBytes, _ := json.Marshal(protocol.JobResultReportRequest{
		NodeID: "node-1",
		Status: string(JobStatusCompleted),
	})
	req := httptest.NewRequest(http.MethodPost, "/jobs/job-1/result", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Mux().ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
}

func TestHandleJobResultRejectsMissingNodeID(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
	w := postJobResult(t, srv, "job-1", protocol.JobResultReportRequest{
		Status: string(JobStatusCompleted),
	})
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func postJobResult(t *testing.T, srv *Server, jobID string, payload protocol.JobResultReportRequest) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := newVersionedRequest(http.MethodPost, "/jobs/"+jobID+"/result", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)
	return w
}
