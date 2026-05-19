package coordinator

import (
	"strings"
	"testing"
	"time"
)

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

func TestJobStoreListQueuedIDs(t *testing.T) {
	store := NewJobStore()

	j1, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	j2, err := store.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	j3, err := store.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create job 3: %v", err)
	}
	if _, err := store.StartAttempt(j2.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	ids, err := store.ListQueuedIDs()
	if err != nil {
		t.Fatalf("list queued ids: %v", err)
	}
	want := []string{j1.ID, j3.ID}
	if len(ids) != len(want) {
		t.Fatalf("expected queued ids %v, got %v", want, ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("expected queued ids %v, got %v", want, ids)
		}
	}
}

func TestJobStoreExpireQueuedJobs(t *testing.T) {
	store := NewJobStore()
	expired, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create expired job: %v", err)
	}
	fresh, err := store.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create fresh job: %v", err)
	}
	running, err := store.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create running job: %v", err)
	}
	if _, err := store.StartAttempt(running.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	now := time.Now().UTC()
	store.mu.Lock()
	store.jobs[expired.ID].CreatedAt = now.Add(-25 * time.Hour)
	store.jobs[expired.ID].UpdatedAt = now.Add(-25 * time.Hour)
	store.jobs[fresh.ID].CreatedAt = now.Add(-time.Hour)
	store.jobs[fresh.ID].UpdatedAt = now.Add(-time.Hour)
	store.jobs[running.ID].CreatedAt = now.Add(-25 * time.Hour)
	store.jobs[running.ID].UpdatedAt = now.Add(-25 * time.Hour)
	store.mu.Unlock()

	count, err := store.ExpireQueuedJobs(now, 24*time.Hour, QueuedJobExpiredError)
	if err != nil {
		t.Fatalf("expire queued jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one expired queued job, got %d", count)
	}

	gotExpired, _, err := store.Get(expired.ID)
	if err != nil {
		t.Fatalf("get expired job: %v", err)
	}
	if gotExpired.Status != JobStatusFailed || gotExpired.LastError != QueuedJobExpiredError || gotExpired.CompletedAt == nil {
		t.Fatalf("unexpected expired job: %+v", gotExpired)
	}
	gotFresh, _, err := store.Get(fresh.ID)
	if err != nil {
		t.Fatalf("get fresh job: %v", err)
	}
	if gotFresh.Status != JobStatusQueued {
		t.Fatalf("expected fresh job to remain QUEUED, got %s", gotFresh.Status)
	}
	gotRunning, _, err := store.Get(running.ID)
	if err != nil {
		t.Fatalf("get running job: %v", err)
	}
	if gotRunning.Status != JobStatusRunning {
		t.Fatalf("expected running job to remain RUNNING, got %s", gotRunning.Status)
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

func TestJobStoreStartAttemptRejectsNonQueuedJob(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.Fail(j.ID, "", JobResult{LastError: "expired"}); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if _, err := store.StartAttempt(j.ID, "node-1"); err == nil {
		t.Fatalf("expected terminal job start attempt to fail")
	} else if !strings.Contains(err.Error(), "not dispatchable") {
		t.Fatalf("expected not dispatchable error, got %v", err)
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
