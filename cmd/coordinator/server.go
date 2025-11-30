package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// server holds dependencies for HTTP handlers.
type server struct {
	registry   *NodeRegistry
	jobs       *JobStore
	httpClient *http.Client
}

// registerRequest is the JSON payload agents send to /register.
type registerRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type createJobRequest struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

type executeRequest struct {
	JobID   string `json:"job_id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// healthHandler is a basic health check.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleRegister handles POST /register from agents.
// We treat each call as both registration and heartbeat.
func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[coordinator] failed to decode register request: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Address == "" {
		http.Error(w, "id and address are required", http.StatusBadRequest)
		return
	}

	node := s.registry.Register(req.ID, req.Address)
	log.Printf("[coordinator] node registered/heartbeat: id=%s addr=%s", node.ID, node.Address)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(node); err != nil {
		log.Printf("[coordinator] failed to encode register response: %v", err)
	}
}

// handleListNodes handles GET /nodes and returns all registered nodes.
func (s *server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := s.registry.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		log.Printf("[coordinator] failed to encode nodes: %v", err)
	}
}

// handleJobs is the multiplexer for /jobs:
//   - POST /jobs -> create a new job
//   - GET /jobs -> list all jobs
func (s *server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateJob(w, r)
	case http.MethodGet:
		s.handleListJobs(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateJob implements POST /jobs.
func (s *server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}

	job := s.jobs.Create(req.Type, req.Payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(job); err != nil {
		log.Printf("encode job response: %v", err)
	}

	go s.dispatchJob(job.ID)
}

// handleListJobs implements GET /jobs.
func (s *server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobs.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		log.Printf("encode jobs response: %v", err)
	}
}

func (s *server) dispatchJob(jobID string) {
	nodes := s.registry.List()

	var target *Node
	for i := range nodes {
		if nodes[i].State == NodeStateHealthy {
			target = &nodes[i]
			break
		}
	}

	if target == nil {
		log.Printf("no healthy nodes available for job %s; leaving as QUEUED", jobID)
		return
	}

	job, err := s.jobs.UpdateStatus(jobID, JobStatusRunning, target.ID)
	if err != nil {
		log.Printf("failed to update job %s to RUNNING: %v", jobID, err)
		return
	}

	agentBase := buildAgentBaseURL(target.Address)
	agentURL := agentBase + "/execute"

	reqBody := executeRequest{
		JobID:   jobID,
		Type:    job.Type,
		Payload: job.Payload,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("failed to marshal execute request for job %s: %v", jobID, err)
		_, _ = s.jobs.UpdateStatus(jobID, JobStatusFailed, target.ID)
		return
	}

	httpReq, err := http.NewRequest(http.MethodPost, agentURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("failed to create HTTP request for job %s: %v", jobID, err)
		_, _ = s.jobs.UpdateStatus(jobID, JobStatusFailed, target.ID)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("job %s execution request failed: %v", jobID, err)
		_, _ = s.jobs.UpdateStatus(jobID, JobStatusFailed, target.ID)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("job %s execution failed, status code: %d", jobID, resp.StatusCode)
		_, _ = s.jobs.UpdateStatus(jobID, JobStatusFailed, target.ID)
		return
	}

	if _, err := s.jobs.UpdateStatus(jobID, JobStatusCompleted, target.ID); err != nil {
		log.Printf("failed to update job %s to COMPLETED: %v", jobID, err)
	}
}

// Converts a node's Address into a usable base URL
func buildAgentBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}

	return "http://" + addr
}
