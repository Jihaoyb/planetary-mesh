package main

import "testing"

func TestJobStoreCreateAndList(t *testing.T) {
	store := NewJobStore()

	j1 := store.Create("echo", "hello")
	if j1.ID == "" {
		t.Fatalf("expected non-empty job ID")
	}
	if j1.Type != "echo" {
		t.Fatalf("expected type echo, got %s", j1.Type)
	}
	if j1.Payload != "hello" {
		t.Fatalf("expected payload hello, got %s", j1.Payload)
	}
	if j1.Status != JobStatusQueued {
		t.Fatalf("expected status %s, got %s", JobStatusQueued, j1.Status)
	}

	j2 := store.Create("echo", "world")
	if j2.ID == j1.ID {
		t.Fatalf("expected different job IDs, got %s and %s", j1.ID, j2.ID)
	}

	jobs := store.List()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	byID := make(map[string]Job)
	for _, j := range jobs {
		byID[j.ID] = j
	}

	if byID[j1.ID].Payload != "hello" {
		t.Errorf("job1 payload mismatch; got %s", byID[j1.ID].Payload)
	}
	if byID[j2.ID].Payload != "world" {
		t.Errorf("job2 payload mismatch; got %s", byID[j2.ID].Payload)
	}
}

func TestJobStoreUpdateStatus(t *testing.T) {
	store := NewJobStore()

	j := store.Create("echo", "data")
	if j.Status != JobStatusQueued {
		t.Fatalf("expected initial status QUEUED, got %s", j.Status)
	}
	if j.NodeID != "" {
		t.Fatalf("expected initial NodeID to be empty, got %s", j.NodeID)
	}

	// first update: set RUNNING + NodeID.
	updated, err := store.UpdateStatus(j.ID, JobStatusRunning, "node-1")
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}
	if updated.Status != JobStatusRunning {
		t.Fatalf("expected status RUNNING, got %s", updated.Status)
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected NodeID node-1, got %s", updated.NodeID)
	}

	// second update: set COMPLETED, but keep existing NodeID (empty nodeID argument).
	updated2, err := store.UpdateStatus(j.ID, JobStatusCompleted, "")
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}
	if updated2.Status != JobStatusCompleted {
		t.Fatalf("expected status COMPLETED, got %s", updated2.Status)
	}
	if updated2.NodeID != "node-1" {
		t.Fatalf("expected NodeID to remain node-1, got %s", updated2.NodeID)
	}

	// updating a non-existent job should return an error.
	if _, err := store.UpdateStatus("does-not-exist", JobStatusFailed, "node-x"); err == nil {
		t.Fatalf("expected error when updating non-existent job, got nil")
	}
}

func TestJobStoreGet(t *testing.T) {
	store := NewJobStore()

	created := store.Create("echo", "payload")

	found, err := store.Get(created.ID)
	if err != nil {
		t.Fatalf("unexpected error getting job: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected job ID %s, got %s", created.ID, found.ID)
	}
	if found.Payload != "payload" {
		t.Fatalf("expected payload payload, got %s", found.Payload)
	}

	if _, err := store.Get("missing-id"); err == nil {
		t.Fatalf("expected error for missing job ID, got nil")
	}
}
