package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExecuteHandlerSuccess(t *testing.T) {
	payload := executeRequest{
		JobID:   "job-1",
		Type:    "echo",
		Payload: "hello",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	executeHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	var resp map[string]string
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %q", resp["status"])
	}
}

func TestExecuteHandlerInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()

	executeHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid JSON, got %d", res.StatusCode)
	}
}

func TestExecuteHandlerMissingJobID(t *testing.T) {
	payload := executeRequest{
		Type:    "echo",
		Payload: "hello",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	executeHandler(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for missing job_id, got %d", res.StatusCode)
	}
}
