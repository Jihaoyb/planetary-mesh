# Architecture

This document describes the current Planetary Mesh architecture and separates it
from future directions. Planetary Mesh is currently a trusted LAN/private-network
prototype for allowlisted command-job execution across machines a user/team owns
or controls.

For product framing and sequencing, see:

- [product-positioning.md](product-positioning.md)
- [roadmap.md](roadmap.md)
- [current-limitations.md](current-limitations.md)
- [tech-choices.md](tech-choices.md)
- [adr/](adr/)

## Current Architecture

### System View

```text
+---------+        HTTP/JSON         +-------------+       HTTP/JSON        +---------+
|  pmctl  +------------------------->+ Coordinator +---------------------->+ Agent   |
| or curl |                          |             |       POST /execute    | daemon  |
+---------+                          +------+------+                        +----+----+
                                             |
                                             | optional
                                             v
                                        +----------+
                                        | Postgres |
                                        +----------+
```

Current runtime components:

- **Coordinator**: single v0 control plane for node registry, health state, job
  validation, node capability/load metadata, job lifecycle transitions,
  dispatch, retry policy, storage, status, and metrics.
- **Agent**: daemon on a trusted machine. It registers with the coordinator,
  sends heartbeats with node metadata, and executes allowlisted command jobs
  through `/execute`.
- **pmctl**: thin operator CLI over the coordinator HTTP/JSON API.
- **Postgres**: optional durable store for coordinator nodes/jobs. In-memory
  storage remains the default for local runs and ordinary unit tests.

There is no dashboard today.

### Transport and Versioning

The v0 control plane uses HTTP/JSON with the Go standard library. ADR 0003
records this decision.

Versioned control-plane requests use:

```text
X-Planetary-Protocol-Version: 1
```

Coordinator endpoints:

- `GET /healthz` - unversioned basic health check
- `GET /status` - non-secret runtime status/config
- `POST /register` - agent registration and heartbeat
- `GET /nodes` - list known nodes
- `POST /jobs` - submit a job
- `GET /jobs` - list jobs
- `GET /jobs/{id}` - inspect a job
- `GET /metrics` - Prometheus-style text metrics

Agent endpoints:

- `GET /healthz` - unversioned basic health check
- `POST /execute` - execute an assigned job

All coordinator endpoints except `/healthz` require the protocol header. Agent
`/execute` also requires it. Missing or mismatched versions return
`409 Conflict`.

### Coordinator

Coordinator responsibilities:

- load listen address, storage, TLS, and node allowlist settings from defaults,
  optional env-style config files, environment variables, and supported flags
- accept agent registration and heartbeat requests
- store node id, address, last seen timestamp, health state, reported
  capabilities, active execution count, and certificate metadata when present
- update node health states based on heartbeat age
- validate job submissions
- store job metadata and execution result fields
- enforce explicit job lifecycle transitions
- select the first healthy node for initial dispatch and reassign to other
  healthy nodes after retryable dispatch failures
- dispatch to agent `/execute`
- retry retryable transport errors and agent `5xx` responses
- mark terminal job outcomes as `COMPLETED` or `FAILED`
- expose `/status` and `/metrics`
- use in-memory storage by default or Postgres when configured

The coordinator owns validation, scheduling, retry policy, and state
transitions. These responsibilities should not move into agents or `pmctl`.

### Agent

Agent responsibilities:

- load coordinator URL, node id, listen address, advertised address, optional
  static capabilities, TLS files, execution timeout, and command allowlist
  settings
- register with the coordinator on startup
- continue sending registration requests as heartbeats, including the current
  active command execution count
- expose `/execute`
- execute only locally allowlisted command jobs
- enforce fixed execution timeout and bounded output capture
- return execution result fields to the coordinator

The current registration payload includes node id, address, optional static
capabilities, and an approximate active command execution count. Agents do not
report capacity, queue depth, GPU state, or task-level progress today.

### pmctl

`pmctl` is the current operator interface. It is intentionally thin.

Supported operations:

- `pmctl status`
- `pmctl nodes list`
- `pmctl jobs list`
- `pmctl jobs inspect <job-id>`
- `pmctl submit command <command> [args...]`

`pmctl` sends the protocol version header, supports JSON output, and can be
configured with a coordinator URL plus optional CA/cert/key files for secure
coordinator access. `pmctl nodes list` shows node state, active execution count,
capabilities, address, last seen time, and certificate fingerprint. It does not
own scheduling, validation, retries, storage, or state transitions.

### Storage

Coordinator storage holds:

- nodes, including reported capabilities/load metadata
- jobs

Default storage is in-memory. This keeps local development simple and ordinary
`go test ./...` DB-free.

When `COORDINATOR_DATABASE_URL` is configured, the coordinator uses Postgres for
durable nodes/jobs. ADR 0006 records the Postgres decision. ADR 0010 records the
lightweight schema readiness metadata. ADR 0011 records the node
capability/load reporting decision.

Postgres behavior today:

- embedded schema initialization at startup
- `schema_version` table with current version `2`
- node rows include `capabilities` and `active_executions`
- backfill missing schema metadata on existing databases
- reject databases marked with a newer schema version than the running binary
  expects
- expose schema readiness through startup logs, `/status`, `/metrics`,
  `pmctl --json status`, tests, and the Postgres smoke workflow
- mark persisted `RUNNING` jobs as `FAILED` during coordinator startup with:
  `coordinator restarted before result was recorded`
- enforce the same job lifecycle transition rules as the in-memory store

This is not a full migration framework.

### Command Execution Model

The current real workload type is `command`.

Submission:

```json
{
  "type": "command",
  "command": "echo",
  "args": ["hello mesh"]
}
```

Rules:

- `command` is a logical allowlist key.
- `args` is an argument vector.
- `payload` is rejected for `type="command"`.
- Agents map logical command keys to local executable paths through
  `AGENT_COMMAND_ALLOWLIST`.
- Agents execute with `exec.CommandContext`.
- Agents never invoke a shell.
- The execution timeout is fixed by agent config, default `30s`.
- Stdout and stderr are captured separately.
- Each stream is capped at `1 MiB` and reports a truncation flag when clipped.
- Non-zero command exit is terminal and is not retried by the coordinator.

This is allowlisted direct process execution. It is not strong sandbox,
container, VM, or multi-tenant isolation.

### Scheduling and Dispatch

Current dispatch behavior:

1. Client submits a job to `POST /jobs`.
2. Coordinator validates and stores the job as `QUEUED`.
3. A goroutine attempts dispatch immediately.
4. Coordinator lists nodes and picks the first `HEALTHY` node.
5. Coordinator marks the job `RUNNING` for each attempt.
6. Coordinator sends an HTTP/JSON `POST /execute` request to the selected agent.
7. Retryable failures are retried on that node up to the configured attempt
   count, then reassigned to another eligible `HEALTHY` node.
8. Coordinator records the terminal result as `COMPLETED` or `FAILED`.

Queued scheduler behavior:

- the coordinator periodically lists jobs still in `QUEUED` state
- if no healthy node exists, queued jobs remain queued
- if a healthy node exists, the coordinator starts re-dispatch for queued jobs
- if a queued job is still waiting after 24 hours, the coordinator marks it
  `FAILED` with `queued job expired before a healthy node became available`
- duplicate concurrent dispatch of the same job is skipped within the running
  coordinator process
- persisted `QUEUED` jobs can be picked up after coordinator restart when
  Postgres storage is enabled

Retry behavior:

- transport errors, coordinator request timeout, and agent `5xx` responses are
  retryable under the dispatch policy
- validation errors, allowlist rejection, protocol mismatch, and non-zero
  command exit are terminal
- retry attempts use exponential backoff per selected node
- when a selected node exhausts retryable attempts, the coordinator tries the
  next healthy node not already attempted in that dispatch cycle
- if all eligible healthy nodes fail retryably, the job fails with the last
  retryable error
- node state changes to `SUSPECT` or `OFFLINE` do not cancel an already
  in-flight execution attempt in v0

The scheduler is still simple first-healthy-node initial dispatch with
cross-node retry. Reported capabilities and active execution counts are
operator-visible only; they do not affect node selection, priority,
reassignment, or queue fairness.

### Job Lifecycle State Model

Current job statuses are:

- `QUEUED` - accepted by the coordinator and waiting for dispatch
- `RUNNING` - at least one dispatch attempt has started
- `COMPLETED` - terminal success
- `FAILED` - terminal failure

`CANCELLED` is reserved in code for a future cancellation model, but no current
API or coordinator path emits it.

Allowed coordinator-owned transitions:

| Current state | Trigger | Next state |
|---|---|---|
| none | accepted `POST /jobs` | `QUEUED` |
| `QUEUED` | dispatch attempt starts | `RUNNING` |
| `RUNNING` | retry or cross-node reassignment attempt starts | `RUNNING` |
| `RUNNING` | successful agent execution | `COMPLETED` |
| `RUNNING` | terminal execution/dispatch failure | `FAILED` |
| `QUEUED` | queued expiration or pre-attempt coordinator failure | `FAILED` |
| `RUNNING` | Postgres coordinator startup recovery | `FAILED` |

If no healthy node exists, the job remains `QUEUED` with no attempts recorded.
Duplicate dispatch protection is process-local and skips concurrent dispatches
for the same job in one running coordinator. Terminal jobs are not overwritten
by later lifecycle methods.

### Node Registration and Health

Registration flow:

1. Agent starts with coordinator URL, node id, listen address, and advertised
   address.
2. Agent sends `POST /register` with node id, address, optional capabilities,
   and current active execution count.
3. Coordinator validates the node metadata and creates or updates the node
   record.
4. The same request is repeated as heartbeat.

Registration payload:

```json
{
  "id": "local-agent-1",
  "address": "http://localhost:8081",
  "capabilities": ["profile:local", "role:worker"],
  "load": {
    "active_executions": 1
  }
}
```

Older agents that omit `capabilities` and `load` remain compatible. The
coordinator records empty capabilities and zero active executions for those
heartbeats. Capability labels are static operator-provided strings; active
executions are a last-heartbeat snapshot and may be stale for `SUSPECT` or
`OFFLINE` nodes.

Health flow:

- registration/heartbeat sets the node to `HEALTHY`
- a background health checker marks stale nodes `SUSPECT`
- nodes stale for longer are marked `OFFLINE`

Current thresholds are implementation details. Health state affects new dispatch
selection but does not cancel already in-flight execution.

### Security Model

Planetary Mesh supports opt-in mTLS and node allowlists today, but plain HTTP
remains available for local development unless configuration changes.

Current secure mode:

- coordinator and agents are configured manually with CA, certificate, and key
  files
- coordinator secure mode requires allowed node identities or fingerprints
- agents validate the coordinator certificate against the configured CA
- coordinator validates agent client certificates against the configured CA
- coordinator rejects `/register` unless node id matches an allowlisted
  certificate identity or fingerprint
- coordinator dispatch to agent `/execute` uses HTTPS and presents the
  coordinator certificate as a client certificate
- node inspection includes certificate subject, DNS/IP/URI identities,
  fingerprint, and expiration metadata when present

Certificate issuance, distribution, enrollment, and rotation are manual.

## Current Limitations

Current private-mesh limitations:

- queued-job scheduling is periodic, first-healthy-node initial dispatch, and
  uses a fixed 24-hour queued-job expiration window
- reported node capabilities/load are visibility fields only; there is no
  load-aware or capability-aware scheduling
- no agent reconciliation after coordinator restart
- no strong sandbox/container/VM isolation
- no per-job timeout override
- no file upload/result download workflow
- no dashboard
- no generated API contract such as OpenAPI or protobuf
- no cancellation API or cancellation behavior
- no production image or packaged release workflow
- no automated mTLS certificate lifecycle
- no multi-tenant authorization model

Current product-scope limitations:

- no public marketplace
- no payment, payout, dispute, reputation, or transaction-fee system
- no approved shared compute pool
- no remote private mesh networking
- no implemented GPU/storage/bandwidth pooling

See [current-limitations.md](current-limitations.md) for a risk-oriented view.

## Future Architecture Directions

These are planned or possible directions, not current implementation.

Private mesh hardening:

- scheduler policy for reported node capabilities/load
- operator runbooks
- API inventory and contract decision
- better install/release workflow
- certificate/onboarding helper
- agent reconciliation strategy after coordinator restart

Productized private mesh:

- richer CLI or dashboard
- job templates
- logs UX
- file/result handling for selected workflows
- private AI/batch demo pipelines
- stronger packaging and release story

Remote private mesh:

- secure remote registration
- stronger node identity model
- access control
- remote health checks
- network failure handling
- TLS/cert lifecycle tooling

Trusted shared pool:

- admin-approved nodes
- trust levels
- usage accounting
- quotas/credits
- approved workload templates
- internal chargeback reports

Overflow marketplace exploration:

- verified provider onboarding
- benchmarking
- pricing and metering
- payouts and platform fee
- reputation/uptime scoring
- disputes/refunds
- abuse prevention
- stronger sandboxing and tenant isolation
- strict acceptable-use controls

Marketplace and payment systems should not be implemented until the private mesh
and trusted shared-pool foundations are mature and explicitly planned.
