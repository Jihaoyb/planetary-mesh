package main

import (
	"fmt"
	"sync"
	"time"
)

// JobStatus represents the lifecycle state of a job.
// For v0 we only use QUEUED, but we define a few more
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCancelled JobStatus = "CANCELLED"
)

// Job is the coordinator's view of a unit of work
// Payload is an opaque string for now and change to JSON later
type Job struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`

	Status JobStatus `json:"status"`

	// is the ID of the node executing / that executed the job
	NodeID string `json:"node_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JobStore is an in-memory, concurrency-safe job registry
// It mirrors NodeRegistry: a map protected by a mutex
type JobStore struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	nextID uint64
}

// Creates an empty job store
func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
	}
}

// Allocates a new job, assigns it an ID, stores it, and return a copy
func (s *JobStore) Create(jobType, payload string) Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := fmt.Sprintf("job-%d", s.nextID)
	now := time.Now().UTC()

	j := &Job{
		ID:        id,
		Type:      jobType,
		Payload:   payload,
		Status:    JobStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.jobs[id] = j

	return *j
}

// Returns a slice of Job values (copies) for all jobs currently known to the coordinator
func (s *JobStore) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, *j)
	}
	return result
}

// Updates the status (and optionally NodeID) of a job
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

// Get returns a copy of the job for the given ID, or an error if it does not exist.
func (s *JobStore) Get(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("job %q not found", id)
	}

	return *j, nil
}
