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
