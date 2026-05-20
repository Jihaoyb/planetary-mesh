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

func TestPostgresJobAttemptReassignmentPersistence(t *testing.T) {
	store := openPostgresTestStore(t)
	status := store.SchemaStatus()
	if !status.Ready || status.Version != PostgresExpectedSchemaVersion || status.ExpectedVersion != PostgresExpectedSchemaVersion {
		t.Fatalf("unexpected schema status: %+v", status)
	}

	jobs := store.Jobs()
	created, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	first, err := jobs.StartAttempt(created.ID, "node-a")
	if err != nil {
		t.Fatalf("start first attempt: %v", err)
	}
	if first.Attempts != 1 || first.NodeID != "node-a" || first.StartedAt == nil {
		t.Fatalf("unexpected first attempt state: %+v", first)
	}
	startedAt := *first.StartedAt

	second, err := jobs.StartAttempt(created.ID, "node-a")
	if err != nil {
		t.Fatalf("start second attempt: %v", err)
	}
	if second.Attempts != 2 || second.NodeID != "node-a" || second.StartedAt == nil || !second.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected second attempt state: %+v", second)
	}

	third, err := jobs.StartAttempt(created.ID, "node-b")
	if err != nil {
		t.Fatalf("start reassigned attempt: %v", err)
	}
	if third.Attempts != 3 || third.NodeID != "node-b" || third.StartedAt == nil || !third.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected reassigned attempt state: %+v", third)
	}

	failed, err := jobs.Fail(created.ID, "node-b", JobResult{LastError: "node-b retryable failure"})
	if err != nil {
		t.Fatalf("fail reassigned job: %v", err)
	}
	if failed.Status != JobStatusFailed || failed.NodeID != "node-b" || failed.Attempts != 3 {
		t.Fatalf("unexpected failed job state: %+v", failed)
	}
	if failed.StartedAt == nil || !failed.StartedAt.Equal(startedAt) {
		t.Fatalf("expected started_at to remain %s, got %+v", startedAt, failed.StartedAt)
	}
	if failed.CompletedAt == nil {
		t.Fatalf("expected completed_at to be set")
	}
	if failed.LastError != "node-b retryable failure" {
		t.Fatalf("expected final retryable error, got %q", failed.LastError)
	}
}

func TestPostgresListQueuedIDs(t *testing.T) {
	store := openPostgresTestStore(t)
	jobs := store.Jobs()

	j1, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job 1: %v", err)
	}
	j2, err := jobs.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	j3, err := jobs.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create job 3: %v", err)
	}
	if _, err := jobs.StartAttempt(j2.ID, "node-pg"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	ids, err := jobs.ListQueuedIDs()
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

func TestPostgresExpireQueuedJobs(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	store := openPostgresTestStoreForDSN(t, dsn)
	t.Cleanup(func() { _ = store.Close() })
	jobs := store.Jobs()

	expired, err := jobs.Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create expired job: %v", err)
	}
	fresh, err := jobs.Create(JobCreateInput{Type: "command", Command: "sleep"})
	if err != nil {
		t.Fatalf("create fresh job: %v", err)
	}
	running, err := jobs.Create(JobCreateInput{Type: "command", Command: "false"})
	if err != nil {
		t.Fatalf("create running job: %v", err)
	}
	if _, err := jobs.StartAttempt(running.ID, "node-pg"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	now := time.Now().UTC()
	db := openPostgresTestDB(t, dsn)
	if _, err := db.Exec(`UPDATE jobs SET created_at = $1, updated_at = $1 WHERE id = $2`, now.Add(-25*time.Hour), expired.ID); err != nil {
		t.Fatalf("age expired job: %v", err)
	}
	if _, err := db.Exec(`UPDATE jobs SET created_at = $1, updated_at = $1 WHERE id = $2`, now.Add(-time.Hour), fresh.ID); err != nil {
		t.Fatalf("age fresh job: %v", err)
	}
	if _, err := db.Exec(`UPDATE jobs SET created_at = $1, updated_at = $1 WHERE id = $2`, now.Add(-25*time.Hour), running.ID); err != nil {
		t.Fatalf("age running job: %v", err)
	}

	count, err := jobs.ExpireQueuedJobs(now, 24*time.Hour, QueuedJobExpiredError)
	if err != nil {
		t.Fatalf("expire queued jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one expired queued job, got %d", count)
	}

	gotExpired, ok, err := jobs.Get(expired.ID)
	if err != nil {
		t.Fatalf("get expired job: %v", err)
	}
	if !ok {
		t.Fatalf("expected expired job")
	}
	if gotExpired.Status != JobStatusFailed || gotExpired.LastError != QueuedJobExpiredError || gotExpired.CompletedAt == nil {
		t.Fatalf("unexpected expired job: %+v", gotExpired)
	}
	gotFresh, ok, err := jobs.Get(fresh.ID)
	if err != nil {
		t.Fatalf("get fresh job: %v", err)
	}
	if !ok {
		t.Fatalf("expected fresh job")
	}
	if gotFresh.Status != JobStatusQueued {
		t.Fatalf("expected fresh job to remain QUEUED, got %s", gotFresh.Status)
	}
	gotRunning, ok, err := jobs.Get(running.ID)
	if err != nil {
		t.Fatalf("get running job: %v", err)
	}
	if !ok {
		t.Fatalf("expected running job")
	}
	if gotRunning.Status != JobStatusRunning {
		t.Fatalf("expected running job to remain RUNNING, got %s", gotRunning.Status)
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
	if status := store.SchemaStatus(); !status.Ready || status.Version != PostgresExpectedSchemaVersion || status.ExpectedVersion != PostgresExpectedSchemaVersion {
		t.Fatalf("unexpected schema status: %+v", status)
	}

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

func TestPostgresSchemaVersionInitialized(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	store := openPostgresTestStoreForDSN(t, dsn)
	t.Cleanup(func() { _ = store.Close() })

	status := store.SchemaStatus()
	if !status.Ready || status.Version != PostgresExpectedSchemaVersion || status.ExpectedVersion != PostgresExpectedSchemaVersion {
		t.Fatalf("unexpected schema status: %+v", status)
	}
	if got := querySchemaVersion(t, dsn); got != PostgresExpectedSchemaVersion {
		t.Fatalf("expected stored schema version %d, got %d", PostgresExpectedSchemaVersion, got)
	}
}

func TestPostgresSchemaVersionBackfilledWithoutLosingState(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	db := openPostgresTestDB(t, dsn)
	if _, err := db.Exec(oldPostgresSchemaSQL); err != nil {
		t.Fatalf("initialize old schema: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO nodes (id, address, last_seen, state, created_at)
VALUES ('old-node', 'http://agent:8081', now(), 'HEALTHY', now())
`); err != nil {
		t.Fatalf("insert old node: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO jobs (
  id, type, payload, command, args, status, node_id, attempts,
  stdout, stderr, stdout_truncated, stderr_truncated, last_error,
  created_at, updated_at
)
VALUES (
  'job-1', 'command', '', 'echo', '[]'::jsonb, 'COMPLETED', 'old-node', 1,
  'ok', '', false, false, '',
  now(), now()
)
`); err != nil {
		t.Fatalf("insert old job: %v", err)
	}
	if _, err := db.Exec(`SELECT setval('job_id_seq', 1, true)`); err != nil {
		t.Fatalf("set old sequence: %v", err)
	}

	store := openPostgresTestStoreForDSN(t, dsn)
	t.Cleanup(func() { _ = store.Close() })

	if got := querySchemaVersion(t, dsn); got != PostgresExpectedSchemaVersion {
		t.Fatalf("expected backfilled schema version %d, got %d", PostgresExpectedSchemaVersion, got)
	}
	nodes, err := store.Nodes().List()
	if err != nil {
		t.Fatalf("list backfilled nodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "old-node" {
		t.Fatalf("unexpected nodes after backfill: %+v", nodes)
	}
	jobs, err := store.Jobs().List()
	if err != nil {
		t.Fatalf("list backfilled jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-1" || jobs[0].Stdout != "ok" {
		t.Fatalf("unexpected jobs after backfill: %+v", jobs)
	}
	next, err := store.Jobs().Create(JobCreateInput{Type: "command", Command: "echo"})
	if err != nil {
		t.Fatalf("create job after backfill: %v", err)
	}
	if next.ID != "job-2" {
		t.Fatalf("expected job sequence to continue at job-2, got %s", next.ID)
	}
}

func TestPostgresSchemaVersionRejectsNewerDatabase(t *testing.T) {
	dsn := newPostgresTestDSN(t)
	db := openPostgresTestDB(t, dsn)
	if _, err := db.Exec(`
CREATE TABLE schema_version (
  id TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO schema_version (id, version) VALUES ($1, $2);
`, postgresSchemaVersionID, PostgresExpectedSchemaVersion+1); err != nil {
		t.Fatalf("seed newer schema version: %v", err)
	}

	store, err := OpenPostgresStore(context.Background(), dsn)
	if err == nil {
		_ = store.Close()
		t.Fatalf("expected newer schema version to be rejected")
	}
	if !strings.Contains(err.Error(), "newer than expected version") {
		t.Fatalf("expected newer schema version error, got %v", err)
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

func openPostgresTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func querySchemaVersion(t *testing.T, dsn string) int {
	t.Helper()

	db := openPostgresTestDB(t, dsn)
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version WHERE id = $1`, postgresSchemaVersionID).Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	return version
}

const oldPostgresSchemaSQL = `
CREATE SEQUENCE IF NOT EXISTS job_id_seq;

CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  address TEXT NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL,
  state TEXT NOT NULL,
  certificate_subject TEXT NOT NULL DEFAULT '',
  certificate_dns_names JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_ip_addresses JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_uris JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_sha256_fingerprint TEXT NOT NULL DEFAULT '',
  certificate_not_after TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  command TEXT NOT NULL DEFAULT '',
  args JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  exit_code INTEGER,
  stdout TEXT NOT NULL DEFAULT '',
  stderr TEXT NOT NULL DEFAULT '',
  stdout_truncated BOOLEAN NOT NULL DEFAULT false,
  stderr_truncated BOOLEAN NOT NULL DEFAULT false,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
`
