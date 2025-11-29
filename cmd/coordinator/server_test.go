package main

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

	healthHandler(w, req)

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

// TestHandleRegisterAndListNodes verifies that POST /register creates a node
// and GET /nodes returns it.
func TestHandleRegisterAndListNodes(t *testing.T) {
	reg := NewNodeRegistry()
	srv := &server{registry: reg}

	// 1) Register a node via HTTP.
	payload := registerRequest{
		ID:      "agent-1",
		Address: ":8081",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
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

	if nodeResp.ID != "agent-1" {
		t.Fatalf("expected node id agent-1, got %s", nodeResp.ID)
	}
	if nodeResp.Address != ":8081" {
		t.Fatalf("expected node address :8081, got %s", nodeResp.Address)
	}

	// 2) List nodes via HTTP and ensure the registered node is present.
	reqList := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	wList := httptest.NewRecorder()

	srv.handleListNodes(wList, reqList)

	resList := wList.Result()
	defer resList.Body.Close()

	if resList.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from /nodes, got %d", resList.StatusCode)
	}

	var nodes []Node
	if err := json.NewDecoder(resList.Body).Decode(&nodes); err != nil {
		t.Fatalf("failed to decode nodes response: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "agent-1" {
		t.Fatalf("expected node id agent-1 in list, got %s", nodes[0].ID)
	}
}
