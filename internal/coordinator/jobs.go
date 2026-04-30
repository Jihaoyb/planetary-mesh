package coordinator

import (
	"fmt"
	"sync"
	"time"
)

// JobStatus represents the lifecycle state of a job.
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCancelled JobStatus = "CANCELLED"
)

// Job is the coordinator's view of a unit of work.
// Payload is an opaque string for now; may become structured JSON later.
type Job struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Payload string   `json:"payload"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

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
	Type    string
	Payload string
	Command string
	Args    []string
}

type JobResult struct {
	ExitCode        *int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	LastError       string
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
func (s *JobStore) Create(in JobCreateInput) Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := fmt.Sprintf("job-%d", s.nextID)
	now := time.Now().UTC()

	j := &Job{
		ID:        id,
		Type:      in.Type,
		Payload:   in.Payload,
		Command:   in.Command,
		Args:      append([]string(nil), in.Args...),
		Status:    JobStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.jobs[id] = j

	return *j
}

// List returns all jobs as a slice of copies.
func (s *JobStore) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, *j)
	}
	return result
}

// Get returns a single job by ID. The boolean is false if not found.
func (s *JobStore) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// UpdateStatus updates the status (and optionally NodeID) of a job.
func (s *JobStore) UpdateStatus(id string, status JobStatus, nodeID string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}

	j.Status = status
	if nodeID != "" {
		j.NodeID = nodeID
	}
	j.UpdatedAt = time.Now().UTC()

	return *j, nil
}

func (s *JobStore) StartAttempt(id, nodeID string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}

	now := time.Now().UTC()
	j.Status = JobStatusRunning
	j.NodeID = nodeID
	j.Attempts++
	if j.StartedAt == nil {
		j.StartedAt = &now
	}
	j.UpdatedAt = now

	return *j, nil
}

func (s *JobStore) Complete(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusCompleted, result)
}

func (s *JobStore) Fail(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusFailed, result)
}

func (s *JobStore) finish(id, nodeID string, status JobStatus, result JobResult) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}

	now := time.Now().UTC()
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

	return *j, nil
}
