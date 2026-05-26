package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

func TestAgentAPIContractHealthEndpointIsUnversioned(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	MuxWithConfig(ExecutorConfig{}).ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected unversioned /healthz to return 200, got %d", w.Result().StatusCode)
	}
}

func TestAgentAPIContractRouteMethodExpectations(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "healthz", method: http.MethodPost, path: "/healthz"},
		{name: "execute", method: http.MethodGet, path: "/execute"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			protocol.SetVersionHeader(req.Header)
			w := httptest.NewRecorder()

			MuxWithConfig(ExecutorConfig{}).ServeHTTP(w, req)

			if w.Result().StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("expected unsupported %s %s to return 405, got %d", tc.method, tc.path, w.Result().StatusCode)
			}
		})
	}
}

func TestAgentAPIContractExecuteRequiresProtocolHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/execute", nil)
	w := httptest.NewRecorder()

	MuxWithConfig(ExecutorConfig{}).ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected /execute without protocol header to return 409, got %d", w.Result().StatusCode)
	}
}

func TestAgentAPIContractExecuteResponseJSONFields(t *testing.T) {
	cfg := ExecutorConfig{
		Allowlist: map[string]string{"echo": "builtin:echo"},
		Timeout:   2 * time.Second,
	}
	req := newExecuteRequest(t, protocol.ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Command: "echo",
		Args:    []string{"hello"},
	})
	w := httptest.NewRecorder()

	MuxWithConfig(cfg).ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected /execute success to return 200, got %d", w.Result().StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	assertAgentJSONKeys(t, got, "status", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "last_error")
}

func assertAgentJSONKeys(t *testing.T, got map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected JSON key %q in %+v", key, got)
		}
	}
}
