package coordinator

import "testing"

func TestJobStoreCreateAndList(t *testing.T) {
	store := NewJobStore()

	j1, err := store.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	if j1.ID == "" {
		t.Fatalf("expected non-empty job ID")
	}
	if j1.Type != "command" {
		t.Fatalf("expected type command, got %s", j1.Type)
	}
	if j1.Command != "echo" {
		t.Fatalf("expected command echo, got %s", j1.Command)
	}
	if len(j1.Args) != 1 || j1.Args[0] != "hello" {
		t.Fatalf("unexpected args: %#v", j1.Args)
	}
	if j1.Status != JobStatusQueued {
		t.Fatalf("expected status %s, got %s", JobStatusQueued, j1.Status)
	}

	j2, err := store.Create(JobCreateInput{Type: "echo", Payload: "world"})
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	if j2.ID == j1.ID {
		t.Fatalf("expected different job IDs, got %s and %s", j1.ID, j2.ID)
	}

	jobs, err := store.List()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestJobStoreGet(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "echo", Payload: "hello"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, ok, err := store.Get(j.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !ok {
		t.Fatalf("expected to find job %s", j.ID)
	}
	if got.ID != j.ID || got.Payload != "hello" {
		t.Fatalf("unexpected job returned: %+v", got)
	}

	if _, ok, err := store.Get("nope"); err != nil {
		t.Fatalf("get missing job: %v", err)
	} else if ok {
		t.Fatalf("expected Get on missing id to return false")
	}
}

func TestJobStoreExecutionLifecycle(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"data"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	started, err := store.StartAttempt(j.ID, "node-1")
	if err != nil {
		t.Fatalf("unexpected error starting attempt: %v", err)
	}
	if started.Status != JobStatusRunning {
		t.Fatalf("expected RUNNING, got %s", started.Status)
	}
	if started.Attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", started.Attempts)
	}
	if started.StartedAt == nil {
		t.Fatalf("expected started_at to be set")
	}

	exitCode := 0
	completed, err := store.Complete(j.ID, "node-1", JobResult{
		ExitCode: &exitCode,
		Stdout:   "hello\n",
	})
	if err != nil {
		t.Fatalf("unexpected error completing job: %v", err)
	}
	if completed.Status != JobStatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", completed.Status)
	}
	if completed.CompletedAt == nil {
		t.Fatalf("expected completed_at to be set")
	}
	if completed.Stdout != "hello\n" {
		t.Fatalf("unexpected stdout: %q", completed.Stdout)
	}
}

func TestJobStoreFail(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	if _, err := store.StartAttempt(j.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	exitCode := 1
	failed, err := store.Fail(j.ID, "node-1", JobResult{
		ExitCode:  &exitCode,
		LastError: "command exited with code 1",
	})
	if err != nil {
		t.Fatalf("unexpected error failing job: %v", err)
	}
	if failed.Status != JobStatusFailed {
		t.Fatalf("expected FAILED, got %s", failed.Status)
	}
	if failed.LastError == "" {
		t.Fatalf("expected last_error to be set")
	}
}
