# HTTP/JSON v0 API Inventory

This document is the manual compatibility reference for the current
Planetary Mesh HTTP/JSON v0 control plane. It documents implemented behavior,
not future API goals. It is intentionally narrower than a generated OpenAPI or
protobuf contract.

Versioned endpoints require:

```text
X-Planetary-Protocol-Version: 1
```

Missing or mismatched protocol versions return `409 Conflict` on versioned
endpoints. Health endpoints are intentionally unversioned.

Error responses currently use Go `http.Error` text bodies. There is no JSON
error envelope in v0.

## Surface Overview

Coordinator operator/client-facing endpoints:

| Method | Path | Version header | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | no | Basic coordinator health check |
| `GET` | `/status` | yes | Non-secret runtime status/config metadata |
| `GET` | `/nodes` | yes | List registered nodes |
| `POST` | `/jobs` | yes | Submit a job |
| `GET` | `/jobs` | yes | List jobs |
| `GET` | `/jobs/{id}` | yes | Inspect one job |

Coordinator agent-facing endpoints:

| Method | Path | Version header | Purpose |
|---|---|---|---|
| `POST` | `/register` | yes | Agent registration and heartbeat |
| `POST` | `/jobs/{id}/result` | yes | Agent terminal result report |

Coordinator operational metrics endpoint:

| Method | Path | Version header | Purpose |
|---|---|---|---|
| `GET` | `/metrics` | yes | Prometheus-style text metrics |

Agent coordinator-facing endpoints:

| Method | Path | Version header | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | no | Basic agent health check |
| `POST` | `/execute` | yes | Execute an assigned job |

`pmctl` is a thin client over the coordinator API. It sends the protocol header
for coordinator API calls and mirrors coordinator JSON fields for JSON output.
It is not the authority for validation, scheduling, lifecycle transitions,
result acceptance, metrics, or storage behavior.

## Coordinator Endpoints

### `GET /healthz`

Basic coordinator health check.

- Protocol header: not required.
- Request body: none.
- Success: `200 OK`, plain text body `ok`.
- Important errors: `405 Method Not Allowed` for non-`GET` methods.
- Security: no mTLS node allowlist check at the handler level.
- Compatibility notes: stays unversioned so simple health probes do not need
  protocol headers.

### `GET /status`

Returns non-secret coordinator runtime status/config metadata.

- Protocol header: required.
- Request body: none.
- Success: `200 OK`, JSON `CoordinatorStatusResponse`.
- Important errors: `409 Conflict` for protocol mismatch, `405 Method Not
  Allowed` for non-`GET` methods.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler. No node allowlist check is
  performed by this handler.
- Compatibility notes: fields are additive. `schema` is omitted for in-memory
  storage. `reconciliation` is emitted for Postgres-backed coordinators.

Response shape:

```json
{
  "status": "ok",
  "protocol_version": "1",
  "storage_backend": "in_memory",
  "secure_mode": false,
  "node_allowlist_enabled": false,
  "dispatch": {
    "timeout": "10s",
    "max_attempts": 3,
    "base_backoff": "500ms"
  }
}
```

Postgres-backed coordinators may include:

```json
{
  "schema": {
    "ready": true,
    "version": 2,
    "expected_version": 2
  },
  "reconciliation": {
    "grace": "30s",
    "pending_running_jobs": 0
  }
}
```

### `POST /register`

Registers an agent or refreshes its heartbeat. Repeated registration is the
current heartbeat mechanism.

- Protocol header: required.
- Request body: node registration JSON.
- Success: `200 OK`, JSON node response/list-entry shape.
- Important errors: `400 Bad Request` for invalid JSON, missing `id`/`address`,
  invalid capability labels, or negative load; `403 Forbidden` in secure mode
  when a client certificate is missing or not allowlisted; `409 Conflict` for
  protocol mismatch; `405 Method Not Allowed` for non-`POST`; `500 Internal
  Server Error` for storage failure.
- Security: when node allowlists are configured, the request node id must match
  an allowed certificate identity or SHA-256 certificate fingerprint. Plain HTTP
  mode does not perform node allowlist authorization.
- Compatibility notes: `capabilities` and `load` are additive. Older agents that
  omit them register with empty capabilities and zero active executions.

Request shape:

```json
{
  "id": "agent-1",
  "address": "http://localhost:8081",
  "capabilities": ["profile:local", "role:worker"],
  "load": {
    "active_executions": 1
  }
}
```

### `GET /nodes`

Lists registered nodes.

- Protocol header: required.
- Request body: none.
- Success: `200 OK`, JSON array of node response/list-entry objects.
- Important errors: `409 Conflict` for protocol mismatch, `405 Method Not
  Allowed` for non-`GET`, `500 Internal Server Error` for storage failure.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler. No node allowlist check is
  performed by this handler.
- Compatibility notes: node capability/load metadata is visibility-only and does
  not affect scheduling in v0.

Node response/list-entry shape:

```json
{
  "id": "agent-1",
  "address": "http://localhost:8081",
  "last_seen": "2026-05-22T12:00:00Z",
  "state": "HEALTHY",
  "capabilities": ["profile:local", "role:worker"],
  "load": {
    "active_executions": 1
  },
  "certificate": {
    "certificate_subject": "CN=agent-1",
    "certificate_dns_names": ["agent-1.local"],
    "certificate_ip_addresses": ["127.0.0.1"],
    "certificate_uris": ["spiffe://example/agent-1"],
    "certificate_sha256_fingerprint": "hex",
    "certificate_not_after": "2026-06-22T12:00:00Z"
  }
}
```

`certificate` is omitted when no certificate metadata is stored. Individual
certificate fields are omitted when empty.

### `POST /jobs`

Submits a job. The current supported workload is `type="command"`.

- Protocol header: required.
- Request body: job create JSON.
- Success: `201 Created`, JSON job response/list-entry shape.
- Important errors: `400 Bad Request` for invalid JSON, missing `type`, missing
  command for `type="command"`, `payload` on `type="command"`, or
  `command`/`args` on non-command job types; `409 Conflict` for protocol
  mismatch; `405 Method Not Allowed` for unsupported methods on `/jobs`;
  `500 Internal Server Error` for storage failure.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler. No node allowlist check is
  performed by this handler.
- Compatibility notes: command jobs use logical allowlist keys, not executable
  paths. Non-command payload-style jobs are legacy-compatible behavior, but
  allowlisted command execution is the current workload model.

Command request shape:

```json
{
  "type": "command",
  "command": "echo",
  "args": ["hello mesh"]
}
```

### `GET /jobs`

Lists jobs.

- Protocol header: required.
- Request body: none.
- Success: `200 OK`, JSON array of job response/list-entry objects.
- Important errors: `409 Conflict` for protocol mismatch, `405 Method Not
  Allowed` for unsupported methods on `/jobs`, `500 Internal Server Error` for
  storage failure.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler.
- Compatibility notes: list entries use the same public job shape as job create
  and job inspect responses.

### `GET /jobs/{id}`

Inspects one job.

- Protocol header: required.
- Request body: none.
- Success: `200 OK`, JSON job response/list-entry shape.
- Important errors: `400 Bad Request` for invalid job id/path shape,
  `404 Not Found` for unknown jobs, `409 Conflict` for protocol mismatch,
  `405 Method Not Allowed` for non-`GET`, `500 Internal Server Error` for
  storage failure.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler.
- Compatibility notes: terminal jobs are immutable under current lifecycle
  rules.

Job response/list-entry shape:

```json
{
  "id": "job-1",
  "type": "command",
  "payload": "",
  "command": "echo",
  "args": ["hello mesh"],
  "status": "COMPLETED",
  "node_id": "agent-1",
  "attempts": 1,
  "started_at": "2026-05-22T12:00:00Z",
  "completed_at": "2026-05-22T12:00:01Z",
  "exit_code": 0,
  "stdout": "hello mesh\n",
  "stderr": "",
  "stdout_truncated": false,
  "stderr_truncated": false,
  "last_error": "",
  "created_at": "2026-05-22T12:00:00Z",
  "updated_at": "2026-05-22T12:00:01Z"
}
```

`command`, `args`, `node_id`, `started_at`, `completed_at`, and `exit_code` are
omitted when their Go `omitempty` conditions are met. Current job statuses are
`QUEUED`, `RUNNING`, `COMPLETED`, and `FAILED`. `CANCELLED` is reserved in code
but unsupported and not emitted by current coordinator paths.

### `POST /jobs/{id}/result`

Accepts best-effort terminal result reports from agents.

- Protocol header: required.
- Request body: agent result-report JSON.
- Success: `200 OK`, JSON job response/list-entry shape for accepted reports
  and same-node duplicate/late reports against terminal jobs.
- Important errors: `400 Bad Request` for invalid JSON, missing `node_id`, or
  unsupported report status; `403 Forbidden` in secure mode when a client
  certificate is missing or not allowlisted for the reported node id; `404 Not
  Found` for unknown jobs; `409 Conflict` for protocol mismatch, wrong-node
  reports, stale reports after reassignment, or jobs that are not accepting
  reported results; `405 Method Not Allowed` for non-`POST`; `500 Internal
  Server Error` for storage failure.
- Security: in secure mode, the reporting node id is authorized against the
  client certificate using the same node allowlist policy as registration.
- Compatibility notes: only `COMPLETED` and `FAILED` reports are accepted.
  Reports mutate only existing `RUNNING` jobs whose current node matches
  `node_id`. Unknown, wrong-node, unsupported, duplicate, or late reports do not
  create jobs or overwrite terminal job history.

Request shape:

```json
{
  "node_id": "agent-1",
  "status": "COMPLETED",
  "exit_code": 0,
  "stdout": "hello mesh\n",
  "stderr": "",
  "stdout_truncated": false,
  "stderr_truncated": false,
  "last_error": ""
}
```

### `GET /metrics`

Returns coordinator metrics in a minimal Prometheus text exposition format.

- Protocol header: required.
- Request body: none.
- Success: `200 OK`, `Content-Type: text/plain; version=0.0.4`.
- Important errors: `409 Conflict` for protocol mismatch, `405 Method Not
  Allowed` for non-`GET`.
- Security: if the coordinator server itself is running with mTLS, the TLS
  handshake policy applies before the handler.
- Compatibility notes: metric names and types are part of the current v0
  operational surface and should be kept in sync with this document when they
  change. This is not a long-term Prometheus stability guarantee.

Metrics:

| Metric | Type | Notes |
|---|---|---|
| `planetary_jobs_created_total` | counter | Jobs created through `POST /jobs` |
| `planetary_jobs_completed_total` | counter | Jobs that ended in `COMPLETED` |
| `planetary_jobs_failed_total` | counter | Jobs that ended in `FAILED` |
| `planetary_jobs_recovered_on_startup_total` | counter | Startup `RUNNING` jobs marked `FAILED` after reconciliation grace |
| `planetary_dispatch_attempts_total` | counter | Dispatch attempts, including retries |
| `planetary_dispatch_errors_total` | counter | Dispatch attempts that returned an error |
| `planetary_job_result_reports_accepted_total` | counter | Agent result reports accepted as terminal transitions |
| `planetary_job_result_reports_ignored_total` | counter | Agent result reports ignored without mutation |
| `planetary_jobs_reconciliation_pending` | gauge | Startup `RUNNING` jobs still awaiting reconciliation |
| `planetary_nodes{state="HEALTHY"}` | gauge | Known nodes by state |
| `planetary_nodes{state="SUSPECT"}` | gauge | Known nodes by state |
| `planetary_nodes{state="OFFLINE"}` | gauge | Known nodes by state |
| `planetary_postgres_schema_ready` | gauge | Emitted only when schema metadata is configured |
| `planetary_postgres_schema_version` | gauge | Emitted only when schema metadata is configured |
| `planetary_postgres_schema_expected_version` | gauge | Emitted only when schema metadata is configured |

## Agent Endpoints

### `GET /healthz`

Basic agent health check.

- Protocol header: not required.
- Request body: none.
- Success: `200 OK`, plain text body `ok`.
- Important errors: `405 Method Not Allowed` for non-`GET`.
- Security: if the agent server itself is running with mTLS, the TLS handshake
  policy applies before the handler.
- Compatibility notes: stays unversioned so simple health probes do not need
  protocol headers.

### `POST /execute`

Executes a job assigned by the coordinator.

- Protocol header: required.
- Request body: execute request JSON.
- Success: `200 OK`, JSON execute response. For command jobs, this means the
  command execution completed successfully. For legacy non-command types, the
  current agent returns a simple stub success.
- Important errors: `400 Bad Request` for invalid JSON, missing `job_id`,
  missing command for `type="command"`, or disallowed command; `409 Conflict`
  for protocol mismatch; `422 Unprocessable Entity` for non-zero command exit;
  `504 Gateway Timeout` for command timeout; `500 Internal Server Error` for
  request cancellation or internal execution errors; `405 Method Not Allowed`
  for non-`POST`.
- Security: when the agent server is configured for mTLS, the TLS handshake
  requires a client certificate. The coordinator presents its configured
  certificate for secure dispatch.
- Compatibility notes: command execution uses `exec.CommandContext` directly
  with an allowlisted executable mapping. The agent does not invoke a shell.
  Execution timeout is fixed by agent config; there is no per-job timeout
  override in v0.

Request shape:

```json
{
  "job_id": "job-1",
  "type": "command",
  "command": "echo",
  "args": ["hello mesh"]
}
```

The legacy `payload` field exists on the wire shape but is not supported for
current command job submissions.

Response shape:

```json
{
  "status": "ok",
  "exit_code": 0,
  "stdout": "hello mesh\n",
  "stderr": "",
  "stdout_truncated": false,
  "stderr_truncated": false,
  "last_error": ""
}
```

`exit_code` is omitted when nil. Failed command execution responses use
`"status": "error"` and set `last_error`. Stdout and stderr are capped at
`1 MiB` per stream and set the corresponding truncation flag when clipped.

## Compatibility Policy

- HTTP/JSON remains the v0 runtime protocol.
- Protocol version remains `X-Planetary-Protocol-Version: 1`.
- Coordinator `/healthz` and agent `/healthz` remain unversioned.
- Additive JSON fields may be introduced under protocol version `1` when older
  clients tolerate unknown fields.
- Removing or renaming public JSON fields requires a future milestone and
  likely an ADR.
- Changing job status strings or lifecycle semantics requires future
  lifecycle/API planning.
- New endpoints should be additive.
- Error responses currently use `http.Error` text bodies, not a JSON envelope.
- Generated OpenAPI/protobuf contracts are out of scope for Milestone 16 and
  require a future explicit decision.
- `pmctl` remains a thin API client. It must not become authoritative for
  coordinator validation, scheduling, lifecycle transitions, result acceptance,
  metrics, or storage behavior.
- Metrics names and types are part of the current v0 operational surface and
  should be documented when changed. This manual inventory does not promise
  permanent Prometheus compatibility.
- Postgres persists nodes/jobs only. Schema readiness metadata remains version
  `2`; this inventory does not add schema changes.
