package coordinator

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

func TestCoordinatorAPIContractHealthEndpointIsUnversioned(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected unversioned /healthz to return 200, got %d", w.Result().StatusCode)
	}
}

func TestCoordinatorAPIContractVersionedEndpointsRequireProtocolHeader(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{name: "status", method: http.MethodGet, path: "/status"},
		{name: "register", method: http.MethodPost, path: "/register", body: strings.NewReader(`{}`)},
		{name: "nodes", method: http.MethodGet, path: "/nodes"},
		{name: "jobs create", method: http.MethodPost, path: "/jobs", body: strings.NewReader(`{}`)},
		{name: "command jobs create", method: http.MethodPost, path: "/jobs/command", body: strings.NewReader(`{}`)},
		{name: "jobs list", method: http.MethodGet, path: "/jobs"},
		{name: "job inspect", method: http.MethodGet, path: "/jobs/job-1"},
		{name: "job result", method: http.MethodPost, path: "/jobs/job-1/result", body: strings.NewReader(`{}`)},
		{name: "metrics", method: http.MethodGet, path: "/metrics"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
			req := httptest.NewRequest(tc.method, tc.path, tc.body)
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusConflict {
				t.Fatalf("expected %s %s without protocol header to return 409, got %d", tc.method, tc.path, w.Result().StatusCode)
			}
		})
	}
}

func TestCoordinatorAPIContractRouteMethodExpectations(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "healthz", method: http.MethodPost, path: "/healthz"},
		{name: "status", method: http.MethodPost, path: "/status"},
		{name: "register", method: http.MethodGet, path: "/register"},
		{name: "nodes", method: http.MethodPost, path: "/nodes"},
		{name: "jobs collection", method: http.MethodPut, path: "/jobs"},
		{name: "command jobs create", method: http.MethodGet, path: "/jobs/command"},
		{name: "job inspect", method: http.MethodPost, path: "/jobs/job-1"},
		{name: "job result", method: http.MethodGet, path: "/jobs/job-1/result"},
		{name: "metrics", method: http.MethodPost, path: "/metrics"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)
			req := newVersionedRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("expected unsupported %s %s to return 405, got %d", tc.method, tc.path, w.Result().StatusCode)
			}
		})
	}
}

func TestCoordinatorAPIContractSuccessfulRouteInventory(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) (*Server, *http.Request)
		want  int
	}{
		{
			name: "GET /healthz",
			build: func(t *testing.T) (*Server, *http.Request) {
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), httptest.NewRequest(http.MethodGet, "/healthz", nil)
			},
			want: http.StatusOK,
		},
		{
			name: "GET /status",
			build: func(t *testing.T) (*Server, *http.Request) {
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), newVersionedRequest(http.MethodGet, "/status", nil)
			},
			want: http.StatusOK,
		},
		{
			name: "POST /register",
			build: func(t *testing.T) (*Server, *http.Request) {
				body := mustJSONReader(t, registerRequest{ID: "agent-1", Address: "http://agent.local:8081"})
				req := newVersionedRequest(http.MethodPost, "/register", body)
				req.Header.Set("Content-Type", "application/json")
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), req
			},
			want: http.StatusOK,
		},
		{
			name: "GET /nodes",
			build: func(t *testing.T) (*Server, *http.Request) {
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), newVersionedRequest(http.MethodGet, "/nodes", nil)
			},
			want: http.StatusOK,
		},
		{
			name: "POST /jobs",
			build: func(t *testing.T) (*Server, *http.Request) {
				body := mustJSONReader(t, createJobRequest{Type: "command", Command: "echo", Args: []string{"hello"}})
				req := newVersionedRequest(http.MethodPost, "/jobs", body)
				req.Header.Set("Content-Type", "application/json")
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), req
			},
			want: http.StatusCreated,
		},
		{
			name: "GET /jobs",
			build: func(t *testing.T) (*Server, *http.Request) {
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), newVersionedRequest(http.MethodGet, "/jobs", nil)
			},
			want: http.StatusOK,
		},
		{
			name: "POST /jobs/command",
			build: func(t *testing.T) (*Server, *http.Request) {
				body := mustJSONReader(t, createCommandJobRequest{
					Type:                 "command",
					Command:              "echo",
					Args:                 []string{"hello"},
					RequiredCapabilities: []string{"role:worker"},
				})
				req := newVersionedRequest(http.MethodPost, "/jobs/command", body)
				req.Header.Set("Content-Type", "application/json")
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), req
			},
			want: http.StatusCreated,
		},
		{
			name: "GET /jobs/{id}",
			build: func(t *testing.T) (*Server, *http.Request) {
				store := NewJobStore()
				job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
				if err != nil {
					t.Fatalf("create job: %v", err)
				}
				return NewServer(NewNodeRegistry(), store, nil), newVersionedRequest(http.MethodGet, "/jobs/"+job.ID, nil)
			},
			want: http.StatusOK,
		},
		{
			name: "POST /jobs/{id}/result",
			build: func(t *testing.T) (*Server, *http.Request) {
				store := NewJobStore()
				job, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
				if err != nil {
					t.Fatalf("create job: %v", err)
				}
				if _, err := store.StartAttempt(job.ID, "agent-1"); err != nil {
					t.Fatalf("start attempt: %v", err)
				}
				body := mustJSONReader(t, protocol.JobResultReportRequest{
					NodeID: "agent-1",
					Status: string(JobStatusCompleted),
				})
				req := newVersionedRequest(http.MethodPost, "/jobs/"+job.ID+"/result", body)
				req.Header.Set("Content-Type", "application/json")
				return NewServer(NewNodeRegistry(), store, nil), req
			},
			want: http.StatusOK,
		},
		{
			name: "GET /metrics",
			build: func(t *testing.T) (*Server, *http.Request) {
				return NewServer(NewNodeRegistry(), NewJobStore(), nil), newVersionedRequest(http.MethodGet, "/metrics", nil)
			},
			want: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, req := tc.build(t)
			w := httptest.NewRecorder()

			srv.Mux().ServeHTTP(w, req)

			if w.Result().StatusCode != tc.want {
				t.Fatalf("expected %s to return %d, got %d", tc.name, tc.want, w.Result().StatusCode)
			}
		})
	}
}

func TestCoordinatorAPIContractStatusJSONFields(t *testing.T) {
	schema := protocol.SchemaStatus{Ready: true, Version: 3, ExpectedVersion: 3}
	srv := NewServerWithRuntime(
		NewNodeRegistry(),
		NewJobStore(),
		nil,
		DispatchConfig{Timeout: 10 * time.Second, MaxAttempts: 3, BaseBackoff: 500 * time.Millisecond},
		SecurityConfig{AllowedNodeIdentities: map[string][]string{"agent-1": {"dns:agent.local"}}},
		RuntimeConfig{StorageBackend: "postgres", Schema: &schema, SecureMode: true, ReconciliationGrace: 30 * time.Second},
		nil,
	)
	req := newVersionedRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	srv.Mux().ServeHTTP(w, req)

	var got map[string]any
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	assertJSONKeys(t, got, "status", "protocol_version", "storage_backend", "schema", "secure_mode", "node_allowlist_enabled", "dispatch", "reconciliation")
	assertJSONKeys(t, got["schema"].(map[string]any), "ready", "version", "expected_version")
	assertJSONKeys(t, got["dispatch"].(map[string]any), "timeout", "max_attempts", "base_backoff")
	assertJSONKeys(t, got["reconciliation"].(map[string]any), "grace", "pending_running_jobs")
}

func TestCoordinatorAPIContractNodeAndJobJSONFields(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	node := Node{
		ID:           "agent-1",
		Address:      "http://agent.local:8081",
		LastSeen:     now,
		State:        NodeStateHealthy,
		Capabilities: []string{"role:worker"},
		Load:         protocol.NodeLoad{ActiveExecutions: 1},
		Certificate: security.CertificateMetadata{
			Subject:           "CN=agent-1",
			DNSNames:          []string{"agent.local"},
			IPAddresses:       []string{"127.0.0.1"},
			URIs:              []string{"spiffe://example/agent-1"},
			SHA256Fingerprint: "abcdef",
			NotAfter:          &now,
		},
	}
	nodeMap := marshalMap(t, node)
	assertJSONKeys(t, nodeMap, "id", "address", "last_seen", "state", "capabilities", "load", "certificate")
	assertJSONKeys(t, nodeMap["load"].(map[string]any), "active_executions")
	assertJSONKeys(t, nodeMap["certificate"].(map[string]any), "certificate_subject", "certificate_dns_names", "certificate_ip_addresses", "certificate_uris", "certificate_sha256_fingerprint", "certificate_not_after")

	exitCode := 0
	job := Job{
		ID:                   "job-1",
		Type:                 "command",
		Payload:              "",
		Command:              "echo",
		Args:                 []string{"hello"},
		RequiredCapabilities: []string{"role:worker"},
		Status:               JobStatusCompleted,
		NodeID:               "agent-1",
		Attempts:             1,
		StartedAt:            &now,
		CompletedAt:          &now,
		ExitCode:             &exitCode,
		Stdout:               "hello\n",
		Stderr:               "",
		StdoutTruncated:      false,
		StderrTruncated:      false,
		LastError:            "",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	jobMap := marshalMap(t, job)
	assertJSONKeys(t, jobMap, "id", "type", "payload", "command", "args", "required_capabilities", "status", "node_id", "attempts", "started_at", "completed_at", "exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "last_error", "created_at", "updated_at")
}

func TestCoordinatorAPIContractMetricsInventory(t *testing.T) {
	schema := protocol.SchemaStatus{Ready: true, Version: 3, ExpectedVersion: 3}
	registry := NewNodeRegistry()
	if _, err := registry.Register(NodeRegistration{ID: "agent-1", Address: "http://agent.local:8081"}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	srv := NewServerWithRuntime(
		registry,
		NewJobStore(),
		nil,
		DefaultDispatchConfig(),
		SecurityConfig{},
		RuntimeConfig{StorageBackend: "postgres", Schema: &schema, ReconciliationGrace: 30 * time.Second},
		nil,
	)

	req := newVersionedRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	text := string(body)
	for metric, typ := range map[string]string{
		"planetary_jobs_created_total":                "counter",
		"planetary_jobs_completed_total":              "counter",
		"planetary_jobs_failed_total":                 "counter",
		"planetary_jobs_recovered_on_startup_total":   "counter",
		"planetary_dispatch_attempts_total":           "counter",
		"planetary_dispatch_errors_total":             "counter",
		"planetary_job_result_reports_accepted_total": "counter",
		"planetary_job_result_reports_ignored_total":  "counter",
		"planetary_jobs_reconciliation_pending":       "gauge",
		"planetary_nodes":                             "gauge",
		"planetary_postgres_schema_ready":             "gauge",
		"planetary_postgres_schema_version":           "gauge",
		"planetary_postgres_schema_expected_version":  "gauge",
	} {
		want := "# TYPE " + metric + " " + typ
		if !strings.Contains(text, want) {
			t.Fatalf("expected metrics to contain %q, got:\n%s", want, text)
		}
	}
	for _, want := range []string{
		`planetary_nodes{state="HEALTHY"}`,
		`planetary_nodes{state="SUSPECT"}`,
		`planetary_nodes{state="OFFLINE"}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected metrics to contain %q, got:\n%s", want, text)
		}
	}
}

func mustJSONReader(t *testing.T, value any) io.Reader {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return bytes.NewReader(data)
}

func marshalMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal JSON map: %v", err)
	}
	return out
}

func assertJSONKeys(t *testing.T, got map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected JSON key %q in %+v", key, got)
		}
	}
}
