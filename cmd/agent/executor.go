package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type executeRequest struct {
	JobID   string `json:"job_id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Implements POST /execute on the agent.
// For v1, "execution" just means: log the job, sleep for a bit
func executeHandler(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("[agent] job_id=%s event=execute_start type=%s payload=%q", req.JobID, req.Type, req.Payload)

	// dummy work to simulate doing something.
	time.Sleep(2 * time.Second)

	log.Printf("[agent] job_id=%s event=execute_complete", req.JobID)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{
		"status": "ok",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("agent: failed to encode /execute response: %v", err)
	}
}
