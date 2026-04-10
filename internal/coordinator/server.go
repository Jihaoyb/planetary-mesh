package coordinator

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// DispatchConfig controls how dispatchJob talks to an agent.
type DispatchConfig struct {
	// Timeout is the per-attempt HTTP timeout.
	Timeout time.Duration
	// MaxAttempts is the total number of attempts (1 = no retry).
	MaxAttempts int
	// BaseBackoff is the initial backoff between retries; doubles each attempt.
	BaseBackoff time.Duration
}

// DefaultDispatchConfig returns sensible defaults for v0.
func DefaultDispatchConfig() DispatchConfig {
	return DispatchConfig{
		Timeout:     10 * time.Second,
		MaxAttempts: 3,
		BaseBackoff: 500 * time.Millisecond,
	}
}

// Server holds dependencies for HTTP handlers.
type Server struct {
	registry   *NodeRegistry
	jobs       *JobStore
	httpClient *http.Client
	metrics    *Metrics
	dispatch   DispatchConfig
	logger     *slog.Logger
}

// NewServer constructs a Server with default dispatch config.
// If httpClient is nil, http.DefaultClient is used.
// If logger is nil, slog.Default() is used.
func NewServer(registry *NodeRegistry, jobs *JobStore, httpClient *http.Client) *Server {
	return NewServerWithConfig(registry, jobs, httpClient, DefaultDispatchConfig(), nil)
}

// NewServerWithConfig is the full constructor.
func NewServerWithConfig(
	registry *NodeRegistry,
	jobs *JobStore,
	httpClient *http.Client,
	dispatch DispatchConfig,
	logger *slog.Logger,
) *Server {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		registry:   registry,
		jobs:       jobs,
		httpClient: httpClient,
		metrics:    NewMetrics(),
		dispatch:   dispatch,
		logger:     logger.With("component", "coordinator"),
	}
}

// Metrics exposes the server's metrics counters (used by tests).
func (s *Server) Metrics() *Metrics { return s.metrics }

// Mux returns an http.ServeMux with all coordinator routes wired up.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", HealthHandler)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/nodes", s.handleListNodes)
	mux.HandleFunc("/jobs", s.handleJobs)
	mux.HandleFunc("/jobs/", s.handleJobByID)
	mux.HandleFunc("/metrics", s.handleMetrics)
	return mux
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

// HealthHandler is a basic health check.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleRegister handles POST /register from agents.
// We treat each call as both registration and heartbeat.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Warn("decode register request failed", "err", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Address == "" {
		http.Error(w, "id and address are required", http.StatusBadRequest)
		return
	}

	node := s.registry.Register(req.ID, req.Address)
	s.logger.Info("node register/heartbeat", "node_id", node.ID, "addr", node.Address)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(node); err != nil {
		s.logger.Warn("encode register response failed", "err", err)
	}
}

// handleListNodes handles GET /nodes and returns all registered nodes.
func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := s.registry.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		s.logger.Warn("encode nodes response failed", "err", err)
	}
}

// handleJobs is the multiplexer for /jobs (no trailing path):
//   - POST /jobs -> create a new job
//   - GET  /jobs -> list all jobs
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateJob(w, r)
	case http.MethodGet:
		s.handleListJobs(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleJobByID handles GET /jobs/{id}.
func (s *Server) handleJobByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}

	job, ok := s.jobs.Get(id)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		s.logger.Warn("encode job response failed", "job_id", id, "err", err)
	}
}

// handleMetrics serves /metrics in Prometheus text format.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.metrics.WriteProm(w, s.registry)
}

// handleCreateJob implements POST /jobs.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
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
	s.metrics.JobsCreated.Add(1)
	s.logger.Info("job created", "job_id", job.ID, "type", job.Type)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(job); err != nil {
		s.logger.Warn("encode job create response failed", "err", err)
	}

	go s.dispatchJob(job.ID)
}

// handleListJobs implements GET /jobs.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobs.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		s.logger.Warn("encode jobs list response failed", "err", err)
	}
}

// dispatchJob picks a healthy node, marks the job RUNNING, and POSTs to its
// /execute endpoint. It retries on transport or non-200 errors up to
// dispatch.MaxAttempts with exponential backoff (base = dispatch.BaseBackoff).
func (s *Server) dispatchJob(jobID string) {
	nodes := s.registry.List()

	var target *Node
	for i := range nodes {
		if nodes[i].State == NodeStateHealthy {
			target = &nodes[i]
			break
		}
	}

	if target == nil {
		s.logger.Warn("no healthy nodes; leaving job QUEUED", "job_id", jobID)
		return
	}

	job, err := s.jobs.UpdateStatus(jobID, JobStatusRunning, target.ID)
	if err != nil {
		s.logger.Error("failed to mark job RUNNING", "job_id", jobID, "err", err)
		return
	}

	agentURL := buildAgentBaseURL(target.Address) + "/execute"
	reqBody := executeRequest{
		JobID:   jobID,
		Type:    job.Type,
		Payload: job.Payload,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		s.logger.Error("marshal execute request failed", "job_id", jobID, "err", err)
		s.failJob(jobID, target.ID)
		return
	}

	logger := s.logger.With("job_id", jobID, "node_id", target.ID, "agent_url", agentURL)

	maxAttempts := s.dispatch.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	backoff := s.dispatch.BaseBackoff

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		s.metrics.DispatchAttempts.Add(1)
		logger.Info("dispatch attempt", "attempt", attempt, "max", maxAttempts)

		ok, retryable := s.tryDispatch(agentURL, bodyBytes, logger)
		if ok {
			if _, err := s.jobs.UpdateStatus(jobID, JobStatusCompleted, target.ID); err != nil {
				logger.Error("failed to mark job COMPLETED", "err", err)
			}
			s.metrics.JobsCompleted.Add(1)
			logger.Info("job completed")
			return
		}

		s.metrics.DispatchErrors.Add(1)

		if !retryable || attempt == maxAttempts {
			break
		}
		logger.Warn("dispatch attempt failed; backing off", "attempt", attempt, "backoff", backoff.String())
		time.Sleep(backoff)
		backoff *= 2
	}

	s.failJob(jobID, target.ID)
	logger.Error("job failed after retries", "attempts", maxAttempts)
}

// tryDispatch performs one HTTP attempt. Returns (success, retryable).
func (s *Server) tryDispatch(agentURL string, bodyBytes []byte, logger *slog.Logger) (bool, bool) {
	httpReq, err := http.NewRequest(http.MethodPost, agentURL, bytes.NewReader(bodyBytes))
	if err != nil {
		logger.Error("build request failed", "err", err)
		return false, false
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	// Wrap a per-attempt timeout if configured. We use a derived client to
	// avoid mutating the shared one.
	if s.dispatch.Timeout > 0 {
		client = &http.Client{
			Transport: client.Transport,
			Timeout:   s.dispatch.Timeout,
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		logger.Warn("execute request failed", "err", err)
		return false, true
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("execute returned non-200", "status", resp.StatusCode)
		// 5xx is retryable; 4xx is not.
		return false, resp.StatusCode >= 500
	}
	return true, false
}

func (s *Server) failJob(jobID, nodeID string) {
	if _, err := s.jobs.UpdateStatus(jobID, JobStatusFailed, nodeID); err != nil {
		s.logger.Error("failed to mark job FAILED", "job_id", jobID, "err", err)
	}
	s.metrics.JobsFailed.Add(1)
}

// buildAgentBaseURL converts a node's Address into a usable base URL.
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
