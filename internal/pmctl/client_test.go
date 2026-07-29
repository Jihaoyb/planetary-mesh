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
	var gotRaw map[string]any

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHeader = r.Header.Get(protocol.HeaderName)
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		if err := json.Unmarshal(body, &gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if err := json.Unmarshal(body, &gotRaw); err != nil {
			t.Fatalf("decode raw request: %v", err)
		}
		return jsonResponse(http.StatusCreated, Job{ID: "job-1", Type: "command", Command: "echo", Args: []string{"hello"}, Status: "QUEUED"}), nil
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	job, err := client.CreateCommandJob(context.Background(), "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("create command job: %v", err)
	}

	if gotHeader != protocol.Version {
		t.Fatalf("expected protocol header %q, got %q", protocol.Version, gotHeader)
	}
	if gotRequest.Type != "command" || gotRequest.Command != "echo" || len(gotRequest.Args) != 1 || gotRequest.Args[0] != "hello" {
		t.Fatalf("unexpected create request: %+v", gotRequest)
	}
	if _, ok := gotRaw["required_capabilities"]; ok {
		t.Fatalf("legacy unconstrained request unexpectedly included required_capabilities: %+v", gotRaw)
	}
	if job.ID != "job-1" || job.Status != "QUEUED" {
		t.Fatalf("unexpected job response: %+v", job)
	}
	if job.RequiredCapabilities == nil || len(job.RequiredCapabilities) != 0 {
		t.Fatalf("expected old response to normalize requirements to [], got %+v", job.RequiredCapabilities)
	}
}

func TestClientUsesPlacementEndpointForRequiredCapabilities(t *testing.T) {
	var calls int
	var got createJobRequest
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/jobs/command" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return jsonResponse(http.StatusCreated, Job{
			ID:                   "job-1",
			Type:                 "command",
			Command:              "text-stats",
			Status:               "QUEUED",
			RequiredCapabilities: []string{"profile:local", "role:text-worker"},
		}), nil
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	job, err := client.CreateCommandJob(
		context.Background(),
		"text-stats",
		[]string{"input.txt"},
		[]string{" role:text-worker ", "profile:local", "role:text-worker"},
	)
	if err != nil {
		t.Fatalf("create constrained job: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one request, got %d", calls)
	}
	if strings.Join(got.RequiredCapabilities, ",") != "profile:local,role:text-worker" {
		t.Fatalf("unexpected request requirements: %+v", got.RequiredCapabilities)
	}
	if strings.Join(job.RequiredCapabilities, ",") != "profile:local,role:text-worker" {
		t.Fatalf("unexpected response requirements: %+v", job.RequiredCapabilities)
	}
}

func TestClientFailsClosedWhenCoordinatorDoesNotSupportRequirements(t *testing.T) {
	var calls int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/jobs/command" {
			t.Fatalf("unexpected fallback request to %s", r.URL.Path)
		}
		return textResponse(http.StatusMethodNotAllowed, "method not allowed"), nil
	})}

	client := NewClientWithHTTPClient("http://coordinator.test", httpClient)
	_, err := client.CreateCommandJob(context.Background(), "echo", nil, []string{"role:worker"})
	if err == nil || err.Error() != "coordinator does not support required capabilities; upgrade the coordinator" {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one fail-closed request without fallback, got %d", calls)
	}
}

func TestClientRejectsInvalidRequirementsWithoutRequest(t *testing.T) {
	var calls int
	client := NewClientWithHTTPClient("http://coordinator.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected request")
		}),
	})

	_, err := client.CreateCommandJob(context.Background(), "echo", nil, []string{"-bad"})
	if err == nil || !strings.Contains(err.Error(), `invalid required capability "-bad"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected local validation to make no request, got %d", calls)
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
	} else if nodes[0].Capabilities == nil || nodes[0].Load.ActiveExecutions != 0 {
		t.Fatalf("expected node metadata defaults, got %+v", nodes[0])
	}
	if jobs, err := client.ListJobs(context.Background()); err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	} else if jobs[0].RequiredCapabilities == nil {
		t.Fatalf("expected old list response requirements to normalize to []")
	}
	if job, err := client.GetJob(context.Background(), "job-1"); err != nil || job.ID != "job-1" {
		t.Fatalf("job = %+v, %v", job, err)
	} else if job.RequiredCapabilities == nil {
		t.Fatalf("expected old inspect response requirements to normalize to []")
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

func TestDoctorClientRejectsRedirectsAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name       string
		response   *http.Response
		listNodes  bool
		wantStatus int
		wantDecode bool
	}{
		{
			name: "redirect",
			response: &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Status:     "307 Temporary Redirect",
				Header:     http.Header{"Location": []string{"http://private-target.invalid/status"}},
				Body:       io.NopCloser(strings.NewReader("redirect-secret")),
			},
			wantStatus: http.StatusTemporaryRedirect,
		},
		{
			name: "trailing JSON",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok"} {"secret":"trailing"}`)),
			},
			wantDecode: true,
		},
		{
			name: "null nodes collection",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`null`)),
			},
			listNodes:  true,
			wantDecode: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClientWithHTTPClient("http://coordinator.test", &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return tc.response, nil
				}),
			})
			client.requireSingleJSON = true
			client.httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			}

			var err error
			if tc.listNodes {
				_, err = client.ListNodes(context.Background())
			} else {
				_, err = client.Status(context.Background())
			}
			if tc.wantStatus != 0 {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.wantStatus {
					t.Fatalf("expected HTTP %d error, got %v", tc.wantStatus, err)
				}
			}
			if tc.wantDecode {
				var decodeErr *DecodeError
				if !errors.As(err, &decodeErr) {
					t.Fatalf("expected decode error, got %v", err)
				}
			}
		})
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
