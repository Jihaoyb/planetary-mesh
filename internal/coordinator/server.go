package coordinator

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
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

type SecurityConfig struct {
	AllowedNodeIdentities   map[string][]string
	AllowedNodeFingerprints map[string][]string
}

func (c SecurityConfig) Enabled() bool {
	return len(c.AllowedNodeIdentities) > 0 || len(c.AllowedNodeFingerprints) > 0
}

type RuntimeConfig struct {
	StorageBackend string
	Schema         *protocol.SchemaStatus
	SecureMode     bool
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
	registry   NodeStore
	jobs       JobStorage
	httpClient *http.Client
	metrics    *Metrics
	dispatch   DispatchConfig
	security   SecurityConfig
	runtime    RuntimeConfig
	logger     *slog.Logger
}

// NewServer constructs a Server with default dispatch config.
// If httpClient is nil, http.DefaultClient is used.
// If logger is nil, slog.Default() is used.
func NewServer(registry NodeStore, jobs JobStorage, httpClient *http.Client) *Server {
	return NewServerWithConfig(registry, jobs, httpClient, DefaultDispatchConfig(), nil)
}

// NewServerWithConfig is the full constructor.
func NewServerWithConfig(
	registry NodeStore,
	jobs JobStorage,
	httpClient *http.Client,
	dispatch DispatchConfig,
	logger *slog.Logger,
) *Server {
	return NewServerWithSecurity(registry, jobs, httpClient, dispatch, SecurityConfig{}, logger)
}

func NewServerWithSecurity(
	registry NodeStore,
	jobs JobStorage,
	httpClient *http.Client,
	dispatch DispatchConfig,
	securityConfig SecurityConfig,
	logger *slog.Logger,
) *Server {
	return NewServerWithRuntime(registry, jobs, httpClient, dispatch, securityConfig, RuntimeConfig{}, logger)
}

func NewServerWithRuntime(
	registry NodeStore,
	jobs JobStorage,
	httpClient *http.Client,
	dispatch DispatchConfig,
	securityConfig SecurityConfig,
	runtimeConfig RuntimeConfig,
	logger *slog.Logger,
) *Server {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	if runtimeConfig.StorageBackend == "" {
		runtimeConfig.StorageBackend = "in_memory"
	}
	return &Server{
		registry:   registry,
		jobs:       jobs,
		httpClient: httpClient,
		metrics:    NewMetrics(),
		dispatch:   dispatch,
		security:   securityConfig,
		runtime:    runtimeConfig,
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
	mux.HandleFunc("/status", s.handleStatus)
	return mux
}

// registerRequest is the JSON payload agents send to /register.
type registerRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type createJobRequest struct {
	Type    string   `json:"type"`
	Payload string   `json:"payload,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
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

func requireProtocolVersion(w http.ResponseWriter, r *http.Request) bool {
	if protocol.HasExpectedVersion(r.Header) {
		return true
	}
	http.Error(w, "protocol version mismatch", http.StatusConflict)
	return false
}

// handleRegister handles POST /register from agents.
// We treat each call as both registration and heartbeat.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireProtocolVersion(w, r) {
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

	certificate := security.CertificateMetadata{}
	if s.security.Enabled() {
		peer := peerCertificate(r)
		if peer == nil {
			http.Error(w, "client certificate is required", http.StatusForbidden)
			return
		}
		if !security.AuthorizeNode(req.ID, peer, s.security.AllowedNodeIdentities, s.security.AllowedNodeFingerprints) {
			s.logger.Warn("node registration rejected", "node_id", req.ID, "fingerprint", security.Fingerprint(peer))
			http.Error(w, "node is not allowlisted", http.StatusForbidden)
			return
		}
		certificate = security.FromCertificate(peer)
	}

	node, err := s.registry.Register(NodeRegistration{
		ID:          req.ID,
		Address:     req.Address,
		Certificate: certificate,
	})
	if err != nil {
		s.logger.Error("register node failed", "node_id", req.ID, "err", err)
		http.Error(w, "register node failed", http.StatusInternalServerError)
		return
	}
	s.logger.Info("node register/heartbeat", "node_id", node.ID, "addr", node.Address)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(node); err != nil {
		s.logger.Warn("encode register response failed", "err", err)
	}
}

func peerCertificate(r *http.Request) *x509.Certificate {
	if r.TLS == nil {
		return nil
	}
	if len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
		return r.TLS.VerifiedChains[0][0]
	}
	if len(r.TLS.PeerCertificates) > 0 {
		return r.TLS.PeerCertificates[0]
	}
	return nil
}

// handleListNodes handles GET /nodes and returns all registered nodes.
func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireProtocolVersion(w, r) {
		return
	}

	nodes, err := s.registry.List()
	if err != nil {
		s.logger.Error("list nodes failed", "err", err)
		http.Error(w, "list nodes failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		s.logger.Warn("encode nodes response failed", "err", err)
	}
}

// handleJobs is the multiplexer for /jobs (no trailing path):
//   - POST /jobs -> create a new job
//   - GET  /jobs -> list all jobs
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if !requireProtocolVersion(w, r) {
		return
	}

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
	if !requireProtocolVersion(w, r) {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}

	job, ok, err := s.jobs.Get(id)
	if err != nil {
		s.logger.Error("get job failed", "job_id", id, "err", err)
		http.Error(w, "get job failed", http.StatusInternalServerError)
		return
	}
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
	if !requireProtocolVersion(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.metrics.WriteProm(w, s.registry, s.runtime.Schema)
}

// handleStatus serves non-secret coordinator runtime status for operators.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireProtocolVersion(w, r) {
		return
	}

	resp := protocol.CoordinatorStatusResponse{
		Status:               "ok",
		ProtocolVersion:      protocol.Version,
		StorageBackend:       s.runtime.StorageBackend,
		Schema:               s.runtime.Schema,
		SecureMode:           s.runtime.SecureMode,
		NodeAllowlistEnabled: s.security.Enabled(),
		Dispatch: protocol.DispatchStatus{
			Timeout:     s.dispatch.Timeout.String(),
			MaxAttempts: s.dispatch.MaxAttempts,
			BaseBackoff: s.dispatch.BaseBackoff.String(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode status response failed", "err", err)
	}
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
	if req.Type == "command" {
		if req.Command == "" {
			http.Error(w, "command is required for type=command", http.StatusBadRequest)
			return
		}
		if req.Payload != "" {
			http.Error(w, "payload is not supported for type=command", http.StatusBadRequest)
			return
		}
	} else if req.Command != "" || len(req.Args) > 0 {
		http.Error(w, "command and args are only supported for type=command", http.StatusBadRequest)
		return
	}

	job, err := s.jobs.Create(JobCreateInput{
		Type:    req.Type,
		Payload: req.Payload,
		Command: req.Command,
		Args:    req.Args,
	})
	if err != nil {
		s.logger.Error("create job failed", "err", err)
		http.Error(w, "create job failed", http.StatusInternalServerError)
		return
	}
	s.metrics.JobsCreated.Add(1)
	s.logger.Info("job created", "job_id", job.ID, "type", job.Type, "command", job.Command)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(job); err != nil {
		s.logger.Warn("encode job create response failed", "err", err)
	}

	go s.dispatchJob(job.ID)
}

// handleListJobs implements GET /jobs.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.jobs.List()
	if err != nil {
		s.logger.Error("list jobs failed", "err", err)
		http.Error(w, "list jobs failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(jobs); err != nil {
		s.logger.Warn("encode jobs list response failed", "err", err)
	}
}

// dispatchJob picks a healthy node, marks the job RUNNING, and POSTs to its
// /execute endpoint. It retries on transport or retryable server errors up to
// dispatch.MaxAttempts with exponential backoff (base = dispatch.BaseBackoff).
func (s *Server) dispatchJob(jobID string) {
	nodes, err := s.registry.List()
	if err != nil {
		s.logger.Error("list nodes during dispatch failed", "job_id", jobID, "err", err)
		return
	}

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

	job, ok, err := s.jobs.Get(jobID)
	if err != nil {
		s.logger.Error("get job during dispatch failed", "job_id", jobID, "err", err)
		return
	}
	if !ok {
		s.logger.Error("job missing during dispatch", "job_id", jobID)
		return
	}

	reqBody := protocol.ExecuteRequest{
		JobID:   jobID,
		Type:    job.Type,
		Payload: job.Payload,
		Command: job.Command,
		Args:    append([]string(nil), job.Args...),
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		s.logger.Error("marshal execute request failed", "job_id", jobID, "err", err)
		s.failJob(jobID, target.ID, JobResult{LastError: err.Error()})
		return
	}

	agentURL := buildAgentBaseURL(target.Address, s.security.Enabled()) + "/execute"
	logger := s.logger.With("job_id", jobID, "node_id", target.ID, "agent_url", agentURL)

	maxAttempts := s.dispatch.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	backoff := s.dispatch.BaseBackoff

	lastResult := JobResult{}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if _, err := s.jobs.StartAttempt(jobID, target.ID); err != nil {
			logger.Error("failed to mark job RUNNING", "err", err)
			return
		}

		s.metrics.DispatchAttempts.Add(1)
		logger.Info("dispatch attempt", "attempt", attempt, "max", maxAttempts)

		result, ok, retryable := s.tryDispatch(agentURL, bodyBytes, logger)
		lastResult = result
		if ok {
			if _, err := s.jobs.Complete(jobID, target.ID, result); err != nil {
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

		logger.Warn("dispatch attempt failed; backing off", "attempt", attempt, "backoff", backoff.String(), "last_error", result.LastError)
		time.Sleep(backoff)
		backoff *= 2
	}

	s.failJob(jobID, target.ID, lastResult)
	logger.Error("job failed after retries", "attempts", maxAttempts, "last_error", lastResult.LastError)
}

// tryDispatch performs one HTTP attempt. Returns (result, success, retryable).
func (s *Server) tryDispatch(agentURL string, bodyBytes []byte, logger *slog.Logger) (JobResult, bool, bool) {
	httpReq, err := http.NewRequest(http.MethodPost, agentURL, bytes.NewReader(bodyBytes))
	if err != nil {
		logger.Error("build request failed", "err", err)
		return JobResult{LastError: err.Error()}, false, false
	}
	httpReq.Header.Set("Content-Type", "application/json")
	protocol.SetVersionHeader(httpReq.Header)

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
		return JobResult{LastError: err.Error()}, false, true
	}
	defer resp.Body.Close()

	result := decodeExecuteResponse(resp.Body)
	if result.LastError == "" && resp.StatusCode >= 400 {
		result.LastError = fmt.Sprintf("agent returned status %d", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusOK {
		return result, true, false
	}

	logger.Warn("execute returned non-200", "status", resp.StatusCode, "last_error", result.LastError)
	return result, false, resp.StatusCode >= 500
}

func decodeExecuteResponse(body io.Reader) JobResult {
	var resp protocol.ExecuteResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return JobResult{}
	}
	return JobResult{
		ExitCode:        resp.ExitCode,
		Stdout:          resp.Stdout,
		Stderr:          resp.Stderr,
		StdoutTruncated: resp.StdoutTruncated,
		StderrTruncated: resp.StderrTruncated,
		LastError:       resp.LastError,
	}
}

func (s *Server) failJob(jobID, nodeID string, result JobResult) {
	if _, err := s.jobs.Fail(jobID, nodeID, result); err != nil {
		s.logger.Error("failed to mark job FAILED", "job_id", jobID, "err", err)
	}
	s.metrics.JobsFailed.Add(1)
}

// buildAgentBaseURL converts a node's Address into a usable base URL.
func buildAgentBaseURL(addr string, secure bool) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	if strings.HasPrefix(addr, ":") {
		return scheme + "://localhost" + addr
	}
	return scheme + "://" + addr
}
