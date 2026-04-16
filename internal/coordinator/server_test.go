package coordinator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthHandler verifies that /healthz returns 200 and body "ok".
func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	HealthHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	body := buf.String()
	if body != "ok" {
		t.Fatalf("expected body 'ok', got %q", body)
	}
}

func TestProtocolVersionRequired(t *testing.T) {
	srv := NewServer(NewNodeRegistry(), NewJobStore(), nil)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	srv.handleListNodes(w, req)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode)
	}
}

// TestHandleRegisterAndListNodes verifies that POST /register creates a node
// and GET /nodes returns it.
func TestHandleRegisterAndListNodes(t *testing.T) {
	reg := NewNodeRegistry()
	srv := NewServer(reg, NewJobStore(), nil)

	payload := registerRequest{
		ID:      "agent-1",
		Address: ":8081",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := newVersionedRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleRegister(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	var nodeResp Node
	if err := json.NewDecoder(res.Body).Decode(&nodeResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	reqList := newVersionedRequest(http.MethodGet, "/nodes", nil)
	wList := httptest.NewRecorder()
	srv.handleListNodes(wList, reqList)

	if wList.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from /nodes, got %d", wList.Result().StatusCode)
	}
}
