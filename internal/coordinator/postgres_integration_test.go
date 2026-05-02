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

	store, err := OpenPostgresStore(context.Background(), dsnWithSearchPath(baseDSN, schema))
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
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
