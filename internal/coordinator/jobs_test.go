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

func TestJobStoreRunningAttemptPreservesStartedAtAndUpdatesNode(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	first, err := store.StartAttempt(j.ID, "node-1")
	if err != nil {
		t.Fatalf("start first attempt: %v", err)
	}
	if first.StartedAt == nil {
		t.Fatalf("expected started_at on first attempt")
	}
	startedAt := *first.StartedAt

	second, err := store.StartAttempt(j.ID, "node-2")
	if err != nil {
		t.Fatalf("start second attempt: %v", err)
	}
	if second.Status != JobStatusRunning || second.NodeID != "node-2" || second.Attempts != 2 {
		t.Fatalf("unexpected second attempt state: %+v", second)
	}
	if second.StartedAt == nil || !second.StartedAt.Equal(startedAt) {
		t.Fatalf("expected started_at to remain %s, got %+v", startedAt, second.StartedAt)
	}
}

func TestJobStoreCanFailQueuedJob(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	failed, err := store.Fail(j.ID, "", JobResult{LastError: "queued failure"})
	if err != nil {
		t.Fatalf("fail queued job: %v", err)
	}
	if failed.Status != JobStatusFailed || failed.Attempts != 0 || failed.StartedAt != nil || failed.CompletedAt == nil {
		t.Fatalf("unexpected failed queued job: %+v", failed)
	}
	if failed.NodeID != "" {
		t.Fatalf("expected queued failure to preserve empty node id, got %q", failed.NodeID)
	}
	if failed.LastError != "queued failure" {
		t.Fatalf("expected queued failure error, got %q", failed.LastError)
	}
}

func TestJobStoreRejectsCompleteFromQueued(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	if _, err := store.Complete(j.ID, "node-1", JobResult{Stdout: "should not persist"}); err == nil {
		t.Fatalf("expected completing queued job to fail")
	} else if !strings.Contains(err.Error(), "cannot transition from QUEUED to COMPLETED") {
		t.Fatalf("expected transition error, got %v", err)
	}

	got, _, err := store.Get(j.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusQueued || got.Stdout != "" || got.CompletedAt != nil {
		t.Fatalf("queued job was mutated after invalid completion: %+v", got)
	}
}

func TestJobStoreRejectsTerminalMutation(t *testing.T) {
	store := NewJobStore()
	completedJob, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create completed job: %v", err)
	}
	if _, err := store.StartAttempt(completedJob.ID, "node-1"); err != nil {
		t.Fatalf("start completed job: %v", err)
	}
	exitCode := 0
	completed, err := store.Complete(completedJob.ID, "node-1", JobResult{ExitCode: &exitCode, Stdout: "ok\n"})
	if err != nil {
		t.Fatalf("complete job: %v", err)
	}

	if _, err := store.Fail(completedJob.ID, "node-2", JobResult{LastError: "overwrite"}); err == nil {
		t.Fatalf("expected failing completed job to be rejected")
	}
	if _, err := store.StartAttempt(completedJob.ID, "node-2"); err == nil {
		t.Fatalf("expected starting completed job to be rejected")
	}
	gotCompleted, _, err := store.Get(completedJob.ID)
	if err != nil {
		t.Fatalf("get completed job: %v", err)
	}
	if gotCompleted.Status != JobStatusCompleted || gotCompleted.NodeID != completed.NodeID || gotCompleted.Stdout != "ok\n" || gotCompleted.LastError != "" {
		t.Fatalf("completed job was mutated: %+v", gotCompleted)
	}

	failedJob, err := store.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create failed job: %v", err)
	}
	if _, err := store.StartAttempt(failedJob.ID, "node-1"); err != nil {
		t.Fatalf("start failed job: %v", err)
	}
	failed, err := store.Fail(failedJob.ID, "node-1", JobResult{LastError: "exit status 1"})
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if _, err := store.Complete(failedJob.ID, "node-2", JobResult{Stdout: "overwrite"}); err == nil {
		t.Fatalf("expected completing failed job to be rejected")
	}
	gotFailed, _, err := store.Get(failedJob.ID)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if gotFailed.Status != JobStatusFailed || gotFailed.NodeID != failed.NodeID || gotFailed.LastError != "exit status 1" || gotFailed.Stdout != "" {
		t.Fatalf("failed job was mutated: %+v", gotFailed)
	}
}

func TestJobStoreRejectsUnsupportedStatusTransitions(t *testing.T) {
	for _, status := range []JobStatus{JobStatusCancelled, JobStatus("PAUSED")} {
		t.Run(string(status), func(t *testing.T) {
			store := NewJobStore()
			j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
			if err != nil {
				t.Fatalf("create job: %v", err)
			}

			store.mu.Lock()
			store.jobs[j.ID].Status = status
			store.mu.Unlock()

			if _, err := store.StartAttempt(j.ID, "node-1"); err == nil {
				t.Fatalf("expected starting %s job to be rejected", status)
			}
			if _, err := store.Fail(j.ID, "node-1", JobResult{LastError: "overwrite"}); err == nil {
				t.Fatalf("expected failing %s job to be rejected", status)
			}

			got, _, err := store.Get(j.ID)
			if err != nil {
				t.Fatalf("get job: %v", err)
			}
			if got.Status != status || got.LastError != "" || got.NodeID != "" {
				t.Fatalf("unsupported-status job was mutated: %+v", got)
			}
		})
	}
}

func TestJobStoreAcceptReportedResult(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(j.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	exitCode := 0
	completed, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusCompleted, JobResult{
		ExitCode: &exitCode,
		Stdout:   "reported\n",
	})
	if err != nil {
		t.Fatalf("accept reported result: %v", err)
	}
	if outcome != ReportedResultAccepted {
		t.Fatalf("expected accepted outcome, got %s", outcome)
	}
	if completed.Status != JobStatusCompleted || completed.Stdout != "reported\n" || completed.ExitCode == nil || *completed.ExitCode != 0 {
		t.Fatalf("unexpected completed reported result: %+v", completed)
	}
}

func TestJobStoreAcceptReportedFailure(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(j.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	exitCode := 1
	failed, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusFailed, JobResult{
		ExitCode:  &exitCode,
		Stderr:    "boom\n",
		LastError: "command exited with code 1",
	})
	if err != nil {
		t.Fatalf("accept reported failure: %v", err)
	}
	if outcome != ReportedResultAccepted {
		t.Fatalf("expected accepted outcome, got %s", outcome)
	}
	if failed.Status != JobStatusFailed || failed.Stderr != "boom\n" || failed.LastError == "" {
		t.Fatalf("unexpected failed reported result: %+v", failed)
	}
}

func TestJobStoreReportedResultDoesNotMutateTerminalJob(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(j.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	completed, err := store.Complete(j.ID, "node-1", JobResult{Stdout: "original\n"})
	if err != nil {
		t.Fatalf("complete job: %v", err)
	}

	got, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusFailed, JobResult{LastError: "overwrite"})
	if err != nil {
		t.Fatalf("accept duplicate reported result: %v", err)
	}
	if outcome != ReportedResultDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %s", outcome)
	}
	if got.Status != JobStatusCompleted || got.Stdout != completed.Stdout || got.LastError != "" {
		t.Fatalf("terminal job was mutated: %+v", got)
	}
}

func TestJobStoreRejectsWrongNodeReportedResult(t *testing.T) {
	store := NewJobStore()
	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := store.StartAttempt(j.ID, "node-1"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	got, outcome, err := store.AcceptReportedResult(j.ID, "node-2", JobStatusCompleted, JobResult{Stdout: "wrong\n"})
	if err != nil {
		t.Fatalf("accept wrong-node reported result: %v", err)
	}
	if outcome != ReportedResultWrongNode {
		t.Fatalf("expected wrong-node outcome, got %s", outcome)
	}
	if got.Status != JobStatusRunning || got.Stdout != "" || got.NodeID != "node-1" {
		t.Fatalf("wrong-node report mutated job: %+v", got)
	}
}

func TestJobStoreReportedResultOutcomes(t *testing.T) {
	store := NewJobStore()
	if _, outcome, err := store.AcceptReportedResult("missing", "node-1", JobStatusCompleted, JobResult{}); err != nil {
		t.Fatalf("accept missing reported result: %v", err)
	} else if outcome != ReportedResultNotFound {
		t.Fatalf("expected not-found outcome, got %s", outcome)
	}

	j, err := store.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusRunning, JobResult{}); err != nil {
		t.Fatalf("accept unsupported reported status: %v", err)
	} else if outcome != ReportedResultUnsupportedStatus {
		t.Fatalf("expected unsupported-status outcome, got %s", outcome)
	}
	if got, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusCompleted, JobResult{Stdout: "queued\n"}); err != nil {
		t.Fatalf("accept queued reported result: %v", err)
	} else if outcome != ReportedResultWrongState {
		t.Fatalf("expected wrong-state outcome, got %s", outcome)
	} else if got.Status != JobStatusQueued || got.Stdout != "" {
		t.Fatalf("queued report mutated job: %+v", got)
	}

	store.mu.Lock()
	store.jobs[j.ID].Status = JobStatusCancelled
	store.mu.Unlock()
	if got, outcome, err := store.AcceptReportedResult(j.ID, "node-1", JobStatusCompleted, JobResult{Stdout: "cancelled\n"}); err != nil {
		t.Fatalf("accept unsupported persisted state report: %v", err)
	} else if outcome != ReportedResultWrongState {
		t.Fatalf("expected wrong-state outcome for cancelled job, got %s", outcome)
	} else if got.Status != JobStatusCancelled || got.Stdout != "" {
		t.Fatalf("unsupported-state report mutated job: %+v", got)
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
