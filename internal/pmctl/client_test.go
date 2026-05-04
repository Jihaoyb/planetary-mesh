package pmctl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

func TestClientSendsProtocolHeaderAndCreatesCommandJob(t *testing.T) {
	var gotHeader string
	var gotRequest createJobRequest

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHeader = r.Header.Get(protocol.HeaderName)
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusCreated, Job{ID: "job-1", Type: "command", Command: "echo", Args: []string{"hello"}, Status: "QUEUED"}), nil
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	job, err := client.CreateCommandJob(context.Background(), "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("create command job: %v", err)
	}

	if gotHeader != protocol.Version {
		t.Fatalf("expected protocol header %q, got %q", protocol.Version, gotHeader)
	}
	if gotRequest.Type != "command" || gotRequest.Command != "echo" || len(gotRequest.Args) != 1 || gotRequest.Args[0] != "hello" {
		t.Fatalf("unexpected create request: %+v", gotRequest)
	}
	if job.ID != "job-1" || job.Status != "QUEUED" {
		t.Fatalf("unexpected job response: %+v", job)
	}
}

func TestClientMethodsDecodeResponses(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get(protocol.HeaderName) != protocol.Version {
			return textResponse(http.StatusConflict, "missing protocol header"), nil
		}
		switch r.URL.Path {
		case "/status":
			return jsonResponse(http.StatusOK, protocol.CoordinatorStatusResponse{Status: "ok", ProtocolVersion: protocol.Version}), nil
		case "/nodes":
			return jsonResponse(http.StatusOK, []Node{{ID: "node-1", State: "HEALTHY"}}), nil
		case "/jobs":
			return jsonResponse(http.StatusOK, []Job{{ID: "job-1", Status: "COMPLETED"}}), nil
		case "/jobs/job-1":
			return jsonResponse(http.StatusOK, Job{ID: "job-1", Status: "COMPLETED"}), nil
		default:
			return textResponse(http.StatusNotFound, "not found"), nil
		}
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	if status, err := client.Status(context.Background()); err != nil || status.Status != "ok" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if nodes, err := client.ListNodes(context.Background()); err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, %v", nodes, err)
	}
	if jobs, err := client.ListJobs(context.Background()); err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	if job, err := client.GetJob(context.Background(), "job-1"); err != nil || job.ID != "job-1" {
		t.Fatalf("job = %+v, %v", job, err)
	}
}

func TestClientReturnsHTTPErrorForNonSuccess(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return textResponse(http.StatusConflict, "protocol version mismatch"), nil
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	_, err := client.Status(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T %v", err, err)
	}
	if httpErr.StatusCode != http.StatusConflict || httpErr.Body == "" {
		t.Fatalf("unexpected HTTPError: %+v", httpErr)
	}
}

func TestNewClientDefaultsAndRejectsPartialTLSConfig(t *testing.T) {
	client, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("default client: %v", err)
	}
	if client.baseURL != DefaultCoordinatorURL {
		t.Fatalf("expected default URL %q, got %q", DefaultCoordinatorURL, client.baseURL)
	}

	_, err = NewClient(Config{TLSFiles: security.TLSFiles{CAFile: "ca.pem"}})
	if err == nil {
		t.Fatalf("expected partial TLS config to fail")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body any) *http.Response {
	var b strings.Builder
	_ = json.NewEncoder(&b).Encode(body)
	resp := textResponse(status, b.String())
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
