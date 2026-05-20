package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"planetary-mesh/internal/protocol"
)

func TestRegisterWithCoordinatorSendsMetadata(t *testing.T) {
	var got registerPayload
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/register" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(protocol.HeaderName) != protocol.Version {
			t.Fatalf("expected protocol header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode register payload: %v", err)
		}
		return textResponse(http.StatusOK, `{}`), nil
	})}

	err := RegisterWithCoordinatorClientWithMetadata(client, "http://coordinator.test", "node-1", ":8081", RegistrationMetadata{
		Capabilities: []string{"role:worker", "profile:local", "role:worker"},
		Load:         protocol.NodeLoad{ActiveExecutions: 2},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if got.ID != "node-1" || got.Address != ":8081" {
		t.Fatalf("unexpected node identity: %+v", got)
	}
	if strings.Join(got.Capabilities, ",") != "profile:local,role:worker" {
		t.Fatalf("unexpected capabilities: %+v", got.Capabilities)
	}
	if got.Load.ActiveExecutions != 2 {
		t.Fatalf("unexpected load: %+v", got.Load)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
