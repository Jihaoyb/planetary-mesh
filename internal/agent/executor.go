package agent

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// executeRequest is the JSON payload the coordinator sends to /execute.
type executeRequest struct {
	JobID   string `json:"job_id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// HealthHandler is a basic health check.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// ExecuteHandler implements POST /execute on the agent.
// For v0, "execution" just means: log the job, sleep for a bit, return ok.
func ExecuteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.JobID == "" {
		http.Error(w, "job_id is required", http.StatusBadRequest)
		return
	}

	slog.Info("execute start", "job_id", req.JobID, "type", req.Type)

	// dummy work to simulate doing something.
	time.Sleep(2 * time.Second)

	slog.Info("execute done", "job_id", req.JobID)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{
		"status": "ok",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Warn("encode /execute response failed", "err", err)
	}
}

// Mux returns an http.ServeMux with all agent routes wired up.
func Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", HealthHandler)
	mux.HandleFunc("/execute", ExecuteHandler)
	return mux
}
