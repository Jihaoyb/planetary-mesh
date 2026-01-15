package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// server holds dependencies for HTTP handlers.
type server struct {
	registry    *NodeRegistry
	jobs        *JobStore
	httpClient  *http.Client
	dispatchCfg dispatchConfig
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

// dispatchConfig tunes dispatch behavior to agents.
type dispatchConfig struct {
	timeout     time.Duration
	maxAttempts int
	backoff     time.Duration
}

// defaultDispatchConfig returns safe defaults for dispatch behavior.
func defaultDispatchConfig() dispatchConfig {
	return dispatchConfig{
		timeout:     5 * time.Second,
		maxAttempts: 2,
		backoff:     200 * time.Millisecond,
	}
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

// handleMetrics returns simple in-memory metrics about nodes and jobs.
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeCounts := map[NodeState]int{
		NodeStateHealthy: 0,
		NodeStateSuspect: 0,
		NodeStateOffline: 0,
	}
	for _, n := range s.registry.List() {
		nodeCounts[n.State]++
	}

	jobCounts := map[JobStatus]int{
		JobStatusQueued:    0,
		JobStatusRunning:   0,
		JobStatusCompleted: 0,
		JobStatusFailed:    0,
	}
	for _, j := range s.jobs.List() {
		jobCounts[j.Status]++
	}

	resp := map[string]interface{}{
		"nodes": nodeCounts,
		"jobs":  jobCounts,
		"time":  time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[coordinator] failed to encode metrics response: %v", err)
	}
}

// handleJobByID implements GET /jobs/{id}.
func (s *server) handleJobByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "job id not found", http.StatusNotFound)
		return
	}

	job, err := s.jobs.Get(id)
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		log.Printf("encode job response: %v", err)
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

// dispatchJob selects a healthy node and attempts to execute a job with retries/backoff.
func (s *server) dispatchJob(jobID string) {
	cfg := s.dispatchCfg
	if cfg.maxAttempts == 0 {
		cfg = defaultDispatchConfig()
	}

	nodes := s.registry.List()

	var target *Node
	for i := range nodes {
		if nodes[i].State == NodeStateHealthy {
			target = &nodes[i]
			break
		}
	}

	if target == nil {
		log.Printf("[coordinator] job_id=%s event=no_healthy_nodes msg=leaving_queued", jobID)
		return
	}

	job, err := s.jobs.UpdateStatus(jobID, JobStatusRunning, target.ID)
	if err != nil {
		log.Printf("[coordinator] job_id=%s node_id=%s event=update_status msg=failed_to_mark_running err=%v", jobID, target.ID, err)
		return
	}

	agentBase := buildAgentBaseURL(target.Address)
	agentURL := agentBase + "/execute"

	execReq := executeRequest{
		JobID:   jobID,
		Type:    job.Type,
		Payload: job.Payload,
	}

	bodyBytes, err := json.Marshal(execReq)
	if err != nil {
		log.Printf("failed to marshal execute request for job %s: %v", jobID, err)
		return
	}

	// Try up to two attempts with a small backoff for transient failures.
	for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
		if err := s.sendExecuteRequest(agentURL, bodyBytes, cfg.timeout); err != nil {
			log.Printf("[coordinator] job_id=%s node_id=%s event=dispatch attempt=%d/%d err=%v", jobID, target.ID, attempt, cfg.maxAttempts, err)
			if attempt < cfg.maxAttempts {
				time.Sleep(cfg.backoff)
				continue
			}
			_, _ = s.jobs.UpdateStatus(jobID, JobStatusFailed, target.ID)
			return
		}

		if _, err := s.jobs.UpdateStatus(jobID, JobStatusCompleted, target.ID); err != nil {
			log.Printf("[coordinator] job_id=%s node_id=%s event=update_status msg=failed_to_mark_completed err=%v", jobID, target.ID, err)
		}
		return
	}
}

// sendExecuteRequest posts the job execution request to the agent with a per-request timeout.
func (s *server) sendExecuteRequest(agentURL string, body []byte, timeout time.Duration) error {
	httpReq, err := http.NewRequest(http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq = httpReq.WithContext(ctx)

	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
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
