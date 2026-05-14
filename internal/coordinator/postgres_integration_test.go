//go:build postgres

package coordinator

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/security"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func openPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()

	dsn := newPostgresTestDSN(t)
	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newPostgresTestDSN(t *testing.T) string {
	t.Helper()

	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}

	schema := fmt.Sprintf("pm_test_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open base db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})

	return dsnWithSearchPath(baseDSN, schema)
}

func openPostgresTestStoreForDSN(t *testing.T, dsn string) *PostgresStore {
	t.Helper()

	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	return store
}

func dsnWithSearchPath(dsn, schema string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" && u.Host != "" {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	if strings.TrimSpace(dsn) == "" {
		return dsn
	}
	return dsn + " search_path=" + schema
}

func TestPostgresNodePersistence(t *testing.T) {
	store := openPostgresTestStore(t)
	nodes := store.Nodes()

	notAfter := time.Now().Add(time.Hour).UTC()
	registered, err := nodes.Register(NodeRegistration{
		ID:      "node-pg",
		Address: "http://agent:8081",
		Certificate: security.CertificateMetadata{
			Subject:           "CN=node-pg",
			DNSNames:          []string{"node-pg.local"},
			SHA256Fingerprint: "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			NotAfter:          &notAfter,
		},
	})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	if registered.State != NodeStateHealthy {
		t.Fatalf("expected HEALTHY, got %s", registered.State)
	}

	listed, err := nodes.List()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "node-pg" {
		t.Fatalf("unexpected nodes: %+v", listed)
	}
	if listed[0].Certificate.Subject != "CN=node-pg" || listed[0].Certificate.DNSNames[0] != "node-pg.local" {
		t.Fatalf("unexpected certificate metadata: %+v", listed[0].Certificate)
	}

	counts, err := nodes.CountByState()
	if err != nil {
		t.Fatalf("count by state: %v", err)
	}
	if counts.Healthy != 1 {
		t.Fatalf("expected one healthy node, got %+v", counts)
	}
}

func TestPostgresJobLifecyclePersistence(t *testing.T) {
	store := openPostgresTestStore(t)
	jobs := store.Jobs()

	created, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if created.ID != "job-1" {
		t.Fatalf("expected job-1, got %s", created.ID)
	}

	started, err := jobs.StartAttempt(created.ID, "node-pg")
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	if started.Status != JobStatusRunning || started.Attempts != 1 || started.StartedAt == nil {
		t.Fatalf("unexpected started job: %+v", started)
	}

	exitCode := 0
	completed, err := jobs.Complete(created.ID, "node-pg", JobResult{
		ExitCode: &exitCode,
		Stdout:   "hello\n",
	})
	if err != nil {
		t.Fatalf("complete job: %v", err)
	}
	if completed.Status != JobStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("unexpected completed job: %+v", completed)
	}

	got, ok, err := jobs.Get(created.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !ok {
		t.Fatalf("expected job to exist")
	}
	if got.Stdout != "hello\n" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("unexpected persisted result: %+v", got)
	}

	listed, err := jobs.List()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected jobs list: %+v", listed)
	}
}

func TestPostgresFailRunningJobs(t *testing.T) {
	store := openPostgresTestStore(t)
	jobs := store.Jobs()

	created, err := jobs.Create(JobCreateInput{Type: "command", Command: "sleep", Args: []string{"1"}})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := jobs.StartAttempt(created.ID, "node-pg"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	recovered, err := jobs.FailRunningJobs(RestartRecoveryError)
	if err != nil {
		t.Fatalf("fail running jobs: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered job, got %d", recovered)
	}

	got, ok, err := jobs.Get(created.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if !ok {
		t.Fatalf("expected job to exist")
	}
	if got.Status != JobStatusFailed || got.LastError != RestartRecoveryError || got.CompletedAt == nil {
		t.Fatalf("unexpected recovered job: %+v", got)
	}
}

func TestPostgresSchemaInitializationIsIdempotent(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	store := openPostgresTestStoreForDSN(t, dsn)

	if _, err := store.Nodes().Register(NodeRegistration{
		ID:      "node-idempotent",
		Address: "http://agent:8081",
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	if _, err := store.Jobs().Create(JobCreateInput{Type: "command", Command: "echo"}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	reopened := openPostgresTestStoreForDSN(t, dsn)
	t.Cleanup(func() { _ = reopened.Close() })

	nodes, err := reopened.Nodes().List()
	if err != nil {
		t.Fatalf("list nodes after schema reapply: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-idempotent" {
		t.Fatalf("unexpected nodes after schema reapply: %+v", nodes)
	}

	jobs, err := reopened.Jobs().List()
	if err != nil {
		t.Fatalf("list jobs after schema reapply: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("unexpected jobs after schema reapply: %+v", jobs)
	}
}

func TestPostgresRestartRecoveryAfterReopenPreservesTerminalJobs(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	store := openPostgresTestStoreForDSN(t, dsn)
	jobs := store.Jobs()

	running, err := jobs.Create(JobCreateInput{Type: "command", Command: "sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatalf("create running job: %v", err)
	}
	if _, err := jobs.StartAttempt(running.ID, "node-pg"); err != nil {
		t.Fatalf("start running job: %v", err)
	}

	completed, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create completed job: %v", err)
	}
	exitCode := 0
	if _, err := jobs.Complete(completed.ID, "node-pg", JobResult{ExitCode: &exitCode, Stdout: "ok\n"}); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	failed, err := jobs.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create failed job: %v", err)
	}
	if _, err := jobs.Fail(failed.ID, "node-pg", JobResult{LastError: "exit status 1"}); err != nil {
		t.Fatalf("fail job: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	reopened := openPostgresTestStoreForDSN(t, dsn)
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedJobs := reopened.Jobs()

	recovered, err := reopenedJobs.FailRunningJobs(RestartRecoveryError)
	if err != nil {
		t.Fatalf("recover running jobs: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered job, got %d", recovered)
	}

	gotRunning, ok, err := reopenedJobs.Get(running.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if !ok {
		t.Fatalf("expected recovered job")
	}
	if gotRunning.Status != JobStatusFailed || gotRunning.LastError != RestartRecoveryError || gotRunning.CompletedAt == nil {
		t.Fatalf("unexpected recovered job: %+v", gotRunning)
	}

	gotCompleted, ok, err := reopenedJobs.Get(completed.ID)
	if err != nil {
		t.Fatalf("get completed job: %v", err)
	}
	if !ok {
		t.Fatalf("expected completed job")
	}
	if gotCompleted.Status != JobStatusCompleted || gotCompleted.LastError != "" || gotCompleted.Stdout != "ok\n" {
		t.Fatalf("terminal completed job was changed: %+v", gotCompleted)
	}

	gotFailed, ok, err := reopenedJobs.Get(failed.ID)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if !ok {
		t.Fatalf("expected failed job")
	}
	if gotFailed.Status != JobStatusFailed || gotFailed.LastError != "exit status 1" {
		t.Fatalf("terminal failed job was changed: %+v", gotFailed)
	}

	next, err := reopenedJobs.Create(JobCreateInput{Type: "command", Command: "echo", Args: []string{"after-reopen"}})
	if err != nil {
		t.Fatalf("create post-reopen job: %v", err)
	}
	if next.ID != "job-4" {
		t.Fatalf("expected job sequence to continue at job-4, got %s", next.ID)
	}
}
