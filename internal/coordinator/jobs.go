package coordinator

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"planetary-mesh/internal/protocol"
)

// JobStatus represents the lifecycle state of a job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"

	// JobStatusCancelled is reserved for a future cancellation model. No current
	// coordinator path emits it or transitions jobs into it.
	JobStatusCancelled JobStatus = "CANCELLED"
)

const QueuedJobExpiredError = "queued job expired before an eligible healthy node became available"

// Job is the coordinator's view of a unit of work.
// Payload is an opaque string for now; may become structured JSON later.
type Job struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Payload string   `json:"payload"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	// RequiredCapabilities is coordinator-owned placement metadata.
	RequiredCapabilities []string `json:"required_capabilities"`

	Status JobStatus `json:"status"`

	// NodeID is the ID of the node executing / that executed the job.
	NodeID          string     `json:"node_id,omitempty"`
	Attempts        int        `json:"attempts"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Stdout          string     `json:"stdout"`
	Stderr          string     `json:"stderr"`
	StdoutTruncated bool       `json:"stdout_truncated"`
	StderrTruncated bool       `json:"stderr_truncated"`
	LastError       string     `json:"last_error"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type JobCreateInput struct {
	Type                 string
	Payload              string
	Command              string
	Args                 []string
	RequiredCapabilities []string
}

type JobResult struct {
	ExitCode        *int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	LastError       string
}

type ReportedResultOutcome string

const (
	ReportedResultAccepted          ReportedResultOutcome = "accepted"
	ReportedResultDuplicateTerminal ReportedResultOutcome = "duplicate_terminal"
	ReportedResultNotFound          ReportedResultOutcome = "not_found"
	ReportedResultWrongNode         ReportedResultOutcome = "wrong_node"
	ReportedResultWrongState        ReportedResultOutcome = "wrong_state"
	ReportedResultUnsupportedStatus ReportedResultOutcome = "unsupported_status"
)

// JobStorage is the coordinator's narrow job persistence contract.
type JobStorage interface {
	Create(in JobCreateInput) (Job, error)
	List() ([]Job, error)
	ListQueuedIDs() ([]string, error)
	ListRunningIDs() ([]string, error)
	Get(id string) (Job, bool, error)
	StartAttempt(id, nodeID string) (Job, error)
	Complete(id, nodeID string, result JobResult) (Job, error)
	Fail(id, nodeID string, result JobResult) (Job, error)
	AcceptReportedResult(id, nodeID string, status JobStatus, result JobResult) (Job, ReportedResultOutcome, error)
	ExpireQueuedJobs(now time.Time, maxAge time.Duration, lastError string) (int64, error)
	FailRunningJobs(lastError string) (int64, error)
	FailRunningJobIDs(ids []string, lastError string) (int64, error)
}

// JobStore is an in-memory, concurrency-safe job registry.
type JobStore struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	nextID uint64
}

// NewJobStore creates an empty job store.
func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
	}
}

// Create allocates a new job, assigns it an ID, stores it, and returns a copy.
func (s *JobStore) Create(in JobCreateInput) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	requiredCapabilities, err := protocol.NormalizeRequiredCapabilities(in.RequiredCapabilities)
	if err != nil {
		return Job{}, err
	}

	s.nextID++
	id := fmt.Sprintf("job-%d", s.nextID)
	now := time.Now().UTC()

	j := &Job{
		ID:                   id,
		Type:                 in.Type,
		Payload:              in.Payload,
		Command:              in.Command,
		Args:                 append([]string(nil), in.Args...),
		RequiredCapabilities: requiredCapabilities,
		Status:               JobStatusQueued,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	s.jobs[id] = j

	return cloneJob(*j), nil
}

// List returns all jobs as a slice of copies.
func (s *JobStore) List() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, cloneJob(*j))
	}
	return result, nil
}

// ListQueuedIDs returns queued job IDs in deterministic creation order.
func (s *JobStore) ListQueuedIDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queued := make([]Job, 0)
	for _, j := range s.jobs {
		if j.Status == JobStatusQueued {
			queued = append(queued, *j)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].CreatedAt.Equal(queued[j].CreatedAt) {
			return queued[i].ID < queued[j].ID
		}
		return queued[i].CreatedAt.Before(queued[j].CreatedAt)
	})

	ids := make([]string, 0, len(queued))
	for _, j := range queued {
		ids = append(ids, j.ID)
	}
	return ids, nil
}

func (s *JobStore) ListRunningIDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	running := make([]Job, 0)
	for _, j := range s.jobs {
		if j.Status == JobStatusRunning {
			running = append(running, *j)
		}
	}
	sort.Slice(running, func(i, j int) bool {
		if running[i].CreatedAt.Equal(running[j].CreatedAt) {
			return running[i].ID < running[j].ID
		}
		return running[i].CreatedAt.Before(running[j].CreatedAt)
	})

	ids := make([]string, 0, len(running))
	for _, j := range running {
		ids = append(ids, j.ID)
	}
	return ids, nil
}

// Get returns a single job by ID. The boolean is false if not found.
func (s *JobStore) Get(id string) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false, nil
	}
	return cloneJob(*j), true, nil
}

func (s *JobStore) StartAttempt(id, nodeID string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}
	if err := validateJobTransition(id, j.Status, JobStatusRunning); err != nil {
		return Job{}, fmt.Errorf("job %q is %s, not dispatchable: %w", id, j.Status, err)
	}

	now := time.Now().UTC()
	j.Status = JobStatusRunning
	j.NodeID = nodeID
	j.Attempts++
	if j.StartedAt == nil {
		j.StartedAt = &now
	}
	j.UpdatedAt = now

	return cloneJob(*j), nil
}

func (s *JobStore) Complete(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusCompleted, result)
}

func (s *JobStore) Fail(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusFailed, result)
}

func (s *JobStore) AcceptReportedResult(id, nodeID string, status JobStatus, result JobResult) (Job, ReportedResultOutcome, error) {
	if !isReportedTerminalStatus(status) {
		return Job{}, ReportedResultUnsupportedStatus, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ReportedResultNotFound, nil
	}
	if j.Status != JobStatusRunning {
		current := cloneJob(*j)
		return current, classifyReportedResultMiss(current, nodeID), nil
	}
	if j.NodeID != nodeID {
		return cloneJob(*j), ReportedResultWrongNode, nil
	}

	now := time.Now().UTC()
	applyJobResult(j, nodeID, status, result, now)
	return cloneJob(*j), ReportedResultAccepted, nil
}

// ExpireQueuedJobs marks queued jobs older than maxAge as failed.
func (s *JobStore) ExpireQueuedJobs(now time.Time, maxAge time.Duration, lastError string) (int64, error) {
	if maxAge <= 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now = now.UTC()
	cutoff := now.Add(-maxAge)
	var count int64
	for _, j := range s.jobs {
		if j.Status != JobStatusQueued || j.CreatedAt.After(cutoff) {
			continue
		}
		j.Status = JobStatusFailed
		j.LastError = lastError
		j.CompletedAt = &now
		j.UpdatedAt = now
		count++
	}
	return count, nil
}

// FailRunningJobs marks jobs left RUNNING across a coordinator restart as failed.
func (s *JobStore) FailRunningJobs(lastError string) (int64, error) {
	ids, err := s.ListRunningIDs()
	if err != nil {
		return 0, err
	}
	return s.FailRunningJobIDs(ids, lastError)
}

func (s *JobStore) FailRunningJobIDs(ids []string, lastError string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		targets[id] = struct{}{}
	}

	now := time.Now().UTC()
	var count int64
	for id, j := range s.jobs {
		if _, ok := targets[id]; !ok {
			continue
		}
		if j.Status != JobStatusRunning {
			continue
		}
		j.Status = JobStatusFailed
		j.LastError = lastError
		j.CompletedAt = &now
		j.UpdatedAt = now
		count++
	}
	return count, nil
}

func (s *JobStore) finish(id, nodeID string, status JobStatus, result JobResult) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}
	if err := validateJobTransition(id, j.Status, status); err != nil {
		return Job{}, err
	}

	now := time.Now().UTC()
	applyJobResult(j, nodeID, status, result, now)

	return cloneJob(*j), nil
}

func cloneJob(job Job) Job {
	job.Args = append([]string(nil), job.Args...)
	job.RequiredCapabilities = append([]string(nil), job.RequiredCapabilities...)
	if job.Args == nil {
		job.Args = []string{}
	}
	if job.RequiredCapabilities == nil {
		job.RequiredCapabilities = []string{}
	}
	return job
}

func applyJobResult(j *Job, nodeID string, status JobStatus, result JobResult, now time.Time) {
	j.Status = status
	if nodeID != "" {
		j.NodeID = nodeID
	}
	j.CompletedAt = &now
	j.ExitCode = result.ExitCode
	j.Stdout = result.Stdout
	j.Stderr = result.Stderr
	j.StdoutTruncated = result.StdoutTruncated
	j.StderrTruncated = result.StderrTruncated
	j.LastError = result.LastError
	j.UpdatedAt = now
}

func validateJobTransition(id string, from, to JobStatus) error {
	if !isCurrentJobStatus(to) {
		return fmt.Errorf("job %q cannot transition to unsupported status %q", id, to)
	}
	if !isCurrentJobStatus(from) {
		return fmt.Errorf("job %q has unsupported status %q", id, from)
	}
	if canTransitionJobStatus(from, to) {
		return nil
	}
	return fmt.Errorf("job %q cannot transition from %s to %s", id, from, to)
}

func canTransitionJobStatus(from, to JobStatus) bool {
	switch from {
	case JobStatusQueued:
		return to == JobStatusRunning || to == JobStatusFailed
	case JobStatusRunning:
		return to == JobStatusRunning || to == JobStatusCompleted || to == JobStatusFailed
	default:
		return false
	}
}

func isCurrentJobStatus(status JobStatus) bool {
	switch status {
	case JobStatusQueued, JobStatusRunning, JobStatusCompleted, JobStatusFailed:
		return true
	default:
		return false
	}
}

func isReportedTerminalStatus(status JobStatus) bool {
	return status == JobStatusCompleted || status == JobStatusFailed
}

func classifyReportedResultMiss(job Job, nodeID string) ReportedResultOutcome {
	switch job.Status {
	case JobStatusCompleted, JobStatusFailed:
		if job.NodeID == nodeID {
			return ReportedResultDuplicateTerminal
		}
		return ReportedResultWrongNode
	case JobStatusRunning:
		if job.NodeID != nodeID {
			return ReportedResultWrongNode
		}
		return ReportedResultWrongState
	default:
		return ReportedResultWrongState
	}
}
