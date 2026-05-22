# Postgres Durability Runbook

This runbook covers optional Postgres-backed coordinator storage for private
mesh operation. It documents the current durable nodes/jobs behavior and the
bounded startup reconciliation flow. It does not add a migration framework or
change runtime behavior.

## When to Use Postgres

Use Postgres when coordinator node and job history should survive coordinator
restart. Without `COORDINATOR_DATABASE_URL`, the coordinator uses in-memory
storage and loses state when the process exits.

Postgres persists:

- nodes, including reported capabilities and active execution counts
- jobs and job result fields

Postgres does not persist:

- metrics counters
- agent-side result history
- task fanout, attempt history, or a separate result-history table

## Quick Smoke

From the repository root:

```bash
./examples/postgres_smoke.sh
```

The script requires Docker Compose. It:

- starts Postgres, one coordinator, and two agents from `compose.yaml`
- builds a temporary `pmctl` binary
- verifies coordinator status reports Postgres storage
- verifies schema readiness version `2`
- submits a durable command job
- restarts the coordinator while a `sleep` job is `RUNNING`
- verifies reconciliation grace and the restart recovery error
- verifies `/metrics`
- submits another command after restart

Expected outcome:

- `pmctl status` reports `storage_backend=postgres`.
- Schema readiness reports `ready=true`, `version=2`, and
  `expected_version=2`.
- Reconciliation grace in the Compose workflow is `3s`.
- The restart-recovered long-running job eventually becomes `FAILED` with:

```text
coordinator restarted before result was recorded
```

If the smoke fails, it writes Compose logs to the printed log directory unless
`KEEP_POSTGRES_SMOKE=1` is set.

## Manual Coordinator Configuration

Set `COORDINATOR_DATABASE_URL` when starting the coordinator:

```bash
COORDINATOR_DATABASE_URL='postgres://planetary:planetary@localhost:5432/planetary_mesh?sslmode=disable' \
go run ./cmd/coordinator
```

The URL is provider-neutral. The current Compose defaults use:

```text
postgres://planetary:planetary@postgres:5432/planetary_mesh?sslmode=disable
```

The default reconciliation grace is `30s`:

```bash
COORDINATOR_RECONCILIATION_GRACE=30s
```

Set `COORDINATOR_RECONCILIATION_GRACE=0s` only when intentionally preserving
the immediate startup failure behavior for persisted `RUNNING` jobs.

## Compose Workflow

Start the local Postgres-backed stack:

```bash
docker compose up
```

In another terminal:

```bash
go run ./cmd/pmctl status
go run ./cmd/pmctl nodes list
go run ./cmd/pmctl submit command echo "hello from compose"
go run ./cmd/pmctl jobs inspect job-1
```

Default host ports are:

| Service | Port |
|---|---|
| Postgres | `5432` |
| Coordinator | `8080` |
| Agent 1 | `8081` |
| Agent 2 | `8082` |

Override ports when another local stack is using them:

```bash
POSTGRES_HOST_PORT=15432 \
COORDINATOR_HOST_PORT=18080 \
AGENT1_HOST_PORT=18081 \
AGENT2_HOST_PORT=18082 \
docker compose up
```

The Postgres smoke script uses non-default host ports by default to reduce
collisions with manually running services.

## Schema Readiness

The current Postgres schema readiness metadata version is `2`.

Version `2` represents:

- nodes/jobs schema
- node capability/load columns
- schema readiness metadata

It is not a full migration framework. The coordinator applies embedded schema
initialization at startup, records readiness metadata, and rejects a database
marked with a newer schema version than the binary expects.

Inspect schema status through `pmctl`:

```bash
go run ./cmd/pmctl --json status
```

Or through `/status`:

```bash
curl http://localhost:8080/status \
  -H 'X-Planetary-Protocol-Version: 1'
```

Postgres-backed coordinators include:

```json
{
  "schema": {
    "ready": true,
    "version": 2,
    "expected_version": 2
  }
}
```

## Metrics

`/metrics` is versioned and requires the protocol header:

```bash
curl http://localhost:8080/metrics \
  -H 'X-Planetary-Protocol-Version: 1'
```

Postgres readiness and reconciliation metrics include:

- `planetary_postgres_schema_ready`
- `planetary_postgres_schema_version`
- `planetary_postgres_schema_expected_version`
- `planetary_jobs_reconciliation_pending`
- `planetary_jobs_recovered_on_startup_total`

Metrics are process-local and reset on coordinator restart.

## Startup Reconciliation

When a Postgres-backed coordinator starts, it captures persisted `RUNNING` job
IDs and starts a bounded reconciliation grace window.

During grace:

- captured jobs remain `RUNNING`
- the coordinator serves HTTP
- matching agent result reports can complete or fail those jobs
- captured jobs are not re-dispatched

After grace expires, any remaining captured startup `RUNNING` jobs become
`FAILED` with:

```text
coordinator restarted before result was recorded
```

The coordinator exposes pending startup-running jobs through `pmctl status`,
`/status`, and `planetary_jobs_reconciliation_pending`.

## Remaining Limits

- In-memory coordinator restart still loses all coordinator state.
- Agent result history is a bounded in-memory cache; agent restart loses cached
  result reports.
- Command execution remains tied to the `/execute` request context, so this is
  not full in-progress execution recovery after a dropped coordinator
  connection.
- Postgres persists nodes and jobs only.
- Postgres schema readiness version `2` is not a general migration framework.
- Durable storage does not change scheduling, command execution, mTLS, or API
  compatibility behavior.

## Validation

Minimum docs-only validation:

```bash
git diff --check
```

Postgres operator validation when Docker Compose is available:

```bash
./examples/postgres_smoke.sh
```

Opt-in Postgres tests are reserved for Postgres behavior changes or explicit
durable-storage validation:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-postgres go test -tags postgres ./internal/coordinator
```
