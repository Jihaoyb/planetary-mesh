package coordinator

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"planetary-mesh/internal/protocol"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const RestartRecoveryError = "coordinator restarted before result was recorded"
const PostgresExpectedSchemaVersion = 2

const postgresSchemaVersionID = "coordinator"

//go:embed schema/postgres.sql
var postgresSchemaFS embed.FS

// PostgresStore persists coordinator nodes and jobs in Postgres.
type PostgresStore struct {
	db     *sql.DB
	schema protocol.SchemaStatus
}

type PostgresNodeStore struct {
	db *sql.DB
}

type PostgresJobStore struct {
	db *sql.DB
}

func (s *PostgresStore) Nodes() *PostgresNodeStore {
	return &PostgresNodeStore{db: s.db}
}

func (s *PostgresStore) Jobs() *PostgresJobStore {
	return &PostgresJobStore{db: s.db}
}

func (s *PostgresStore) SchemaStatus() protocol.SchemaStatus {
	return s.schema
}

// OpenPostgresStoreWithRetry connects to Postgres, applies the embedded schema,
// and retries briefly so Compose startup can wait for database readiness.
func OpenPostgresStoreWithRetry(ctx context.Context, dsn string) (*PostgresStore, error) {
	const attempts = 5
	backoff := 250 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		store, err := OpenPostgresStore(ctx, dsn)
		if err == nil {
			return store, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}

		select {
		case <-time.After(backoff):
			backoff *= 2
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("connect postgres after %d attempts: %w", attempts, lastErr)
}

// OpenPostgresStore connects to Postgres and applies the embedded schema.
func OpenPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	schema, err := initializePostgresSchema(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &PostgresStore{db: db, schema: schema}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func initializePostgresSchema(ctx context.Context, db *sql.DB) (protocol.SchemaStatus, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.SchemaStatus{}, err
	}
	defer tx.Rollback()

	schema, err := postgresSchemaFS.ReadFile("schema/postgres.sql")
	if err != nil {
		return protocol.SchemaStatus{}, err
	}
	if _, err := tx.ExecContext(ctx, string(schema)); err != nil {
		return protocol.SchemaStatus{}, err
	}

	var current int
	err = tx.QueryRowContext(ctx, `SELECT version FROM schema_version WHERE id = $1`, postgresSchemaVersionID).Scan(&current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		current = PostgresExpectedSchemaVersion
		if _, err := tx.ExecContext(ctx, `
INSERT INTO schema_version (id, version, updated_at)
VALUES ($1, $2, now())
`, postgresSchemaVersionID, current); err != nil {
			return protocol.SchemaStatus{}, err
		}
	case err != nil:
		return protocol.SchemaStatus{}, err
	case current > PostgresExpectedSchemaVersion:
		return protocol.SchemaStatus{}, fmt.Errorf("postgres schema version %d is newer than expected version %d", current, PostgresExpectedSchemaVersion)
	case current < PostgresExpectedSchemaVersion:
		current = PostgresExpectedSchemaVersion
		if _, err := tx.ExecContext(ctx, `
UPDATE schema_version
SET version = $2,
    updated_at = now()
WHERE id = $1
`, postgresSchemaVersionID, current); err != nil {
			return protocol.SchemaStatus{}, err
		}
	}

	status := protocol.SchemaStatus{
		Ready:           true,
		Version:         current,
		ExpectedVersion: PostgresExpectedSchemaVersion,
	}
	if err := tx.Commit(); err != nil {
		return protocol.SchemaStatus{}, err
	}
	return status, nil
}

func (s *PostgresNodeStore) Register(in NodeRegistration) (Node, error) {
	capabilities, err := protocol.NormalizeNodeCapabilities(in.Capabilities)
	if err != nil {
		return Node{}, err
	}
	if err := protocol.ValidateNodeLoad(in.Load); err != nil {
		return Node{}, err
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return Node{}, err
	}

	now := time.Now().UTC()
	dnsJSON, err := json.Marshal(in.Certificate.DNSNames)
	if err != nil {
		return Node{}, err
	}
	ipJSON, err := json.Marshal(in.Certificate.IPAddresses)
	if err != nil {
		return Node{}, err
	}
	uriJSON, err := json.Marshal(in.Certificate.URIs)
	if err != nil {
		return Node{}, err
	}

	row := s.db.QueryRow(`
INSERT INTO nodes (
  id, address, last_seen, state, created_at,
  certificate_subject, certificate_dns_names, certificate_ip_addresses,
  certificate_uris, certificate_sha256_fingerprint, certificate_not_after,
  capabilities, active_executions
)
VALUES ($1, $2, $3, $4, $3, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9, $10, $11::jsonb, $12)
ON CONFLICT (id) DO UPDATE
SET address = EXCLUDED.address,
    last_seen = EXCLUDED.last_seen,
    state = EXCLUDED.state,
    certificate_subject = EXCLUDED.certificate_subject,
    certificate_dns_names = EXCLUDED.certificate_dns_names,
    certificate_ip_addresses = EXCLUDED.certificate_ip_addresses,
    certificate_uris = EXCLUDED.certificate_uris,
    certificate_sha256_fingerprint = EXCLUDED.certificate_sha256_fingerprint,
    certificate_not_after = EXCLUDED.certificate_not_after,
    capabilities = EXCLUDED.capabilities,
    active_executions = EXCLUDED.active_executions
RETURNING `+nodeColumns,
		in.ID,
		in.Address,
		now,
		NodeStateHealthy,
		in.Certificate.Subject,
		string(dnsJSON),
		string(ipJSON),
		string(uriJSON),
		in.Certificate.SHA256Fingerprint,
		in.Certificate.NotAfter,
		string(capabilitiesJSON),
		in.Load.ActiveExecutions,
	)

	node, err := scanNode(row)
	if err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *PostgresNodeStore) List() ([]Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeColumns + ` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *PostgresNodeStore) UpdateHealthStates(now time.Time, suspectAfter, offlineAfter time.Duration) error {
	_, err := s.db.Exec(`
UPDATE nodes
SET state = CASE
  WHEN $1::timestamptz - last_seen > ($3::double precision * interval '1 second') THEN $4
  WHEN $1::timestamptz - last_seen > ($2::double precision * interval '1 second') THEN $5
  ELSE $6
END
`, now.UTC(), suspectAfter.Seconds(), offlineAfter.Seconds(), NodeStateOffline, NodeStateSuspect, NodeStateHealthy)
	return err
}

func (s *PostgresNodeStore) CountByState() (NodeStateCounts, error) {
	rows, err := s.db.Query(`
SELECT state, count(*)
FROM nodes
GROUP BY state
`)
	if err != nil {
		return NodeStateCounts{}, err
	}
	defer rows.Close()

	var counts NodeStateCounts
	for rows.Next() {
		var state NodeState
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return NodeStateCounts{}, err
		}
		switch state {
		case NodeStateHealthy:
			counts.Healthy = count
		case NodeStateSuspect:
			counts.Suspect = count
		case NodeStateOffline:
			counts.Offline = count
		}
	}
	if err := rows.Err(); err != nil {
		return NodeStateCounts{}, err
	}
	return counts, nil
}

const nodeColumns = `
id, address, last_seen, state, capabilities, active_executions,
certificate_subject, certificate_dns_names, certificate_ip_addresses,
certificate_uris, certificate_sha256_fingerprint, certificate_not_after`

func scanNode(row rowScanner) (Node, error) {
	var node Node
	var capabilitiesJSON []byte
	var dnsJSON []byte
	var ipJSON []byte
	var uriJSON []byte
	var notAfter sql.NullTime

	err := row.Scan(
		&node.ID,
		&node.Address,
		&node.LastSeen,
		&node.State,
		&capabilitiesJSON,
		&node.Load.ActiveExecutions,
		&node.Certificate.Subject,
		&dnsJSON,
		&ipJSON,
		&uriJSON,
		&node.Certificate.SHA256Fingerprint,
		&notAfter,
	)
	if err != nil {
		return Node{}, err
	}
	node.Capabilities = []string{}
	if len(capabilitiesJSON) > 0 {
		if err := json.Unmarshal(capabilitiesJSON, &node.Capabilities); err != nil {
			return Node{}, err
		}
	}
	capabilities, err := protocol.NormalizeNodeCapabilities(node.Capabilities)
	if err != nil {
		return Node{}, err
	}
	node.Capabilities = capabilities
	if err := protocol.ValidateNodeLoad(node.Load); err != nil {
		return Node{}, err
	}
	if len(dnsJSON) > 0 {
		if err := json.Unmarshal(dnsJSON, &node.Certificate.DNSNames); err != nil {
			return Node{}, err
		}
	}
	if len(ipJSON) > 0 {
		if err := json.Unmarshal(ipJSON, &node.Certificate.IPAddresses); err != nil {
			return Node{}, err
		}
	}
	if len(uriJSON) > 0 {
		if err := json.Unmarshal(uriJSON, &node.Certificate.URIs); err != nil {
			return Node{}, err
		}
	}
	if notAfter.Valid {
		t := notAfter.Time
		node.Certificate.NotAfter = &t
	}
	return node, nil
}

func (s *PostgresJobStore) Create(in JobCreateInput) (Job, error) {
	var seq int64
	if err := s.db.QueryRow(`SELECT nextval('job_id_seq')`).Scan(&seq); err != nil {
		return Job{}, err
	}

	now := time.Now().UTC()
	job := Job{
		ID:        fmt.Sprintf("job-%d", seq),
		Type:      in.Type,
		Payload:   in.Payload,
		Command:   in.Command,
		Args:      append([]string(nil), in.Args...),
		Status:    JobStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	argsJSON, err := json.Marshal(job.Args)
	if err != nil {
		return Job{}, err
	}

	_, err = s.db.Exec(`
INSERT INTO jobs (
  id, type, payload, command, args, status, node_id, attempts,
  stdout, stderr, stdout_truncated, stderr_truncated, last_error,
  created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, '', 0, '', '', false, false, '', $7, $7)
`, job.ID, job.Type, job.Payload, job.Command, string(argsJSON), job.Status, now)
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *PostgresJobStore) List() ([]Job, error) {
	rows, err := s.db.Query(jobSelectSQL + ` ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

func (s *PostgresJobStore) ListQueuedIDs() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM jobs WHERE status = $1 ORDER BY created_at, id`, JobStatusQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PostgresJobStore) Get(id string) (Job, bool, error) {
	row := s.db.QueryRow(jobSelectSQL+` WHERE id = $1`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (s *PostgresJobStore) StartAttempt(id, nodeID string) (Job, error) {
	now := time.Now().UTC()
	row := s.db.QueryRow(`
UPDATE jobs
SET status = $2,
    node_id = $3,
    attempts = attempts + 1,
    started_at = COALESCE(started_at, $4),
    updated_at = $4
WHERE id = $1
  AND status IN ($5, $6)
RETURNING `+jobColumns, id, JobStatusRunning, nodeID, now, JobStatusQueued, JobStatusRunning)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, s.transitionError(id, JobStatusRunning)
	}
	return job, err
}

func (s *PostgresJobStore) Complete(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusCompleted, result)
}

func (s *PostgresJobStore) Fail(id, nodeID string, result JobResult) (Job, error) {
	return s.finish(id, nodeID, JobStatusFailed, result)
}

func (s *PostgresJobStore) ExpireQueuedJobs(now time.Time, maxAge time.Duration, lastError string) (int64, error) {
	if maxAge <= 0 {
		return 0, nil
	}

	now = now.UTC()
	res, err := s.db.Exec(`
UPDATE jobs
SET status = $1,
    completed_at = $2,
    last_error = $3,
    updated_at = $2
WHERE status = $4
  AND created_at <= $5
`, JobStatusFailed, now, lastError, JobStatusQueued, now.Add(-maxAge))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *PostgresJobStore) finish(id, nodeID string, status JobStatus, result JobResult) (Job, error) {
	now := time.Now().UTC()
	sourceFilter, sourceArgs, err := jobTransitionSourceFilter(status)
	if err != nil {
		return Job{}, err
	}

	args := []any{
		id,
		status,
		nodeID,
		now,
		result.ExitCode,
		result.Stdout,
		result.Stderr,
		result.StdoutTruncated,
		result.StderrTruncated,
		result.LastError,
	}
	args = append(args, sourceArgs...)

	row := s.db.QueryRow(`
UPDATE jobs
SET status = $2,
    node_id = CASE WHEN $3 = '' THEN node_id ELSE $3 END,
    completed_at = $4,
    exit_code = $5,
    stdout = $6,
    stderr = $7,
    stdout_truncated = $8,
    stderr_truncated = $9,
    last_error = $10,
    updated_at = $4
WHERE id = $1
  `+sourceFilter+`
RETURNING `+jobColumns, args...)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, s.transitionError(id, status)
	}
	return job, err
}

func jobTransitionSourceFilter(to JobStatus) (string, []any, error) {
	switch to {
	case JobStatusCompleted:
		return `AND status = $11`, []any{JobStatusRunning}, nil
	case JobStatusFailed:
		return `AND status IN ($11, $12)`, []any{JobStatusQueued, JobStatusRunning}, nil
	default:
		return "", nil, fmt.Errorf("cannot finish job with unsupported target status %q", to)
	}
}

func (s *PostgresJobStore) transitionError(id string, to JobStatus) error {
	job, ok, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("get job %q during transition validation: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	if err := validateJobTransition(id, job.Status, to); err != nil {
		return err
	}
	return fmt.Errorf("job %q transition to %s did not update", id, to)
}

func (s *PostgresJobStore) FailRunningJobs(lastError string) (int64, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
UPDATE jobs
SET status = $1,
    completed_at = $2,
    last_error = $3,
    updated_at = $2
WHERE status = $4
`, JobStatusFailed, now, lastError, JobStatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const jobColumns = `
id, type, payload, command, args, status, node_id, attempts,
started_at, completed_at, exit_code, stdout, stderr, stdout_truncated,
stderr_truncated, last_error, created_at, updated_at`

const jobSelectSQL = `SELECT ` + jobColumns + ` FROM jobs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobs(rows *sql.Rows) ([]Job, error) {
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var argsJSON []byte
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var exitCode sql.NullInt64

	err := row.Scan(
		&job.ID,
		&job.Type,
		&job.Payload,
		&job.Command,
		&argsJSON,
		&job.Status,
		&job.NodeID,
		&job.Attempts,
		&startedAt,
		&completedAt,
		&exitCode,
		&job.Stdout,
		&job.Stderr,
		&job.StdoutTruncated,
		&job.StderrTruncated,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return Job{}, err
	}

	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &job.Args); err != nil {
			return Job{}, err
		}
	}
	if startedAt.Valid {
		t := startedAt.Time
		job.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		job.CompletedAt = &t
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		job.ExitCode = &code
	}

	return job, nil
}
