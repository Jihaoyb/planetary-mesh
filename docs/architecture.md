# Architecture

This document describes the current Planetary Mesh architecture and separates it
from future directions. Planetary Mesh is currently a trusted LAN/private-network
prototype for allowlisted command-job execution across machines a user/team owns
or controls.

For product framing and sequencing, see:

- [product-positioning.md](product-positioning.md)
- [roadmap.md](roadmap.md)
- [current-limitations.md](current-limitations.md)
- [api-http-json-v0.md](api-http-json-v0.md)
- [runbooks/README.md](runbooks/README.md)
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

Task-oriented operator procedures for local runs, Postgres durability, mTLS,
command-execution safety, troubleshooting, and validation live in
[runbooks/README.md](runbooks/README.md). This architecture document remains
the component and boundary reference.

### Transport and Versioning

The v0 control plane uses HTTP/JSON with the Go standard library. ADR 0003
records this decision. The current manual API inventory and compatibility
policy are in [api-http-json-v0.md](api-http-json-v0.md), with ADR 0014
recording the decision to maintain that inventory before generated
OpenAPI/protobuf.

Versioned control-plane requests use:

```text
X-Planetary-Protocol-Version: 1
```

Coordinator endpoints:

- `GET /healthz` - unversioned basic health check
- `GET /status` - non-secret runtime status/config
- `POST /register` - agent registration and heartbeat
- `GET /nodes` - list known nodes
- `POST /jobs` - submit an unconstrained/legacy job
- `POST /jobs/command` - submit a placement-aware command job
- `GET /jobs` - list jobs
- `GET /jobs/{id}` - inspect a job
- `POST /jobs/{id}/result` - agent terminal result report
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
- filter one node snapshot to matching `HEALTHY` candidates, order it by
  reported active executions and node ID, and reuse it for initial dispatch
  and retryable cross-node reassignment
- dispatch to agent `/execute`
- retry retryable transport errors and agent `5xx` responses
- mark terminal job outcomes as `COMPLETED` or `FAILED`
- validate and accept matching agent terminal result reports
- expose `/status` and `/metrics`
- use in-memory storage by default or Postgres when configured
- with Postgres, capture startup `RUNNING` jobs for a bounded reconciliation
  grace window before failing unreconciled captured jobs

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
- keep a bounded in-memory cache of recent terminal command results and report
  them best-effort to the coordinator

The current registration payload includes node id, address, optional static
capabilities, and an approximate active command execution count. Agents do not
persist execution result history across agent restart, provide full in-progress
execution recovery, or report capacity, queue depth, GPU state, or task-level
progress today.

### pmctl

`pmctl` is the current operator interface. It is intentionally thin.

Supported operations:

- `pmctl status`
- `pmctl doctor [--strict] [--timeout <duration>]`
- `pmctl nodes list`
- `pmctl jobs list`
- `pmctl jobs inspect <job-id>`
- `pmctl submit command [--require-capability <label> ...] [--] <command> [args...]`
- `pmctl templates validate <template-file>`
- `pmctl templates inspect <template-file>`
- `pmctl templates preview <template-file> [--require-capability <label> ...] --set name=value`
- `pmctl submit template <template-file> [--require-capability <label> ...] --set name=value`

`pmctl` sends the protocol version header, supports JSON output, and can be
configured with a coordinator URL plus optional CA/cert/key files for secure
coordinator access. `pmctl nodes list` shows node state, active execution count,
capabilities, address, last seen time, and certificate fingerprint. Template
commands validate local JSON files and expand them into the existing command-job
request path. `pmctl doctor` is a read-only diagnostic composition over existing
`GET /status` and `GET /nodes`: it validates local client/TLS configuration,
classifies coordinator and protocol failures, summarizes coordinator-reported
node states, and produces secret-safe human or schema-versioned JSON output. It
does not create jobs, probe agents, read `/metrics`, inspect allowlists or
agent-local files, or prove production readiness. `pmctl` does not own
scheduling, lifecycle transitions, retries, storage, or result acceptance.

### Storage

Coordinator storage holds:

- nodes, including reported capabilities/load metadata
- jobs, including canonical all-of capability requirements

Default storage is in-memory. This keeps local development simple and ordinary
`go test ./...` DB-free.

When `COORDINATOR_DATABASE_URL` is configured, the coordinator uses Postgres for
durable nodes/jobs. ADR 0006 records the Postgres decision. ADR 0010 records the
lightweight schema readiness metadata. ADR 0011 records the node
capability/load reporting decision.

Postgres behavior today:

- embedded schema initialization at startup
- `schema_version` table with current version `3`
- node rows include `capabilities` and `active_executions`
- job rows include `required_capabilities`, with a non-null empty-array default
- backfill missing schema metadata on existing databases
- reject databases marked with a newer schema version than the running binary
  expects
- expose schema readiness through startup logs, `/status`, `/metrics`,
  `pmctl --json status`, tests, and the Postgres smoke workflow
- capture persisted startup `RUNNING` jobs for a bounded reconciliation grace
  window; matching result reports can complete or fail them during grace
- mark remaining captured startup `RUNNING` jobs as `FAILED` after grace with:
  `coordinator restarted before result was recorded`
- enforce the same job lifecycle transition rules as the in-memory store

ADR 0013 documents the accepted reconciliation strategy. Milestone 15 implements
the first runtime slice. ADR 0016 advances readiness metadata to version `3`
for job requirements. A version-2 coordinator rejects a database marked
version `3`; rollback requires restoration of a complete pre-upgrade database
backup. This is not a full migration framework.

### Command Execution Model

The current real workload type is `command`.

Submission:

```json
{
  "type": "command",
  "command": "echo",
  "args": ["hello mesh"],
  "required_capabilities": ["profile:local"]
}
```

Rules:

- `command` is a logical allowlist key.
- `args` is an argument vector.
- `required_capabilities` is optional all-of coordinator metadata, normalized
  with the same label grammar as node capabilities.
- `payload` is rejected for `type="command"`.
- Agents map logical command keys to local executable paths or reserved
  built-in validation targets through
  `AGENT_COMMAND_ALLOWLIST`.
- Placement requirements are not sent to agents and do not alter the
  agent `/execute` request.
- Agents execute external command targets with `exec.CommandContext`.
- Built-in targets use explicit `builtin:<name>` allowlist values and are
  limited to portable validation helpers such as `builtin:echo`,
  `builtin:false`, `builtin:sleep`, and `builtin:line-count`.
- Built-ins are not the workflow extensibility model. Real private workflows
  should use explicit allowlisted external tools or wrapper scripts, optionally
  exposed through the implemented `pmctl` client-side template layer over
  logical command keys.
- `examples/workloads/text-stats` is the tracked example of that external
  executable/wrapper pattern. It is built and allowlisted on agent hosts; it is
  not a new agent built-in or protocol feature.
- Agents never invoke a shell.
- The execution timeout is fixed by agent config, default `30s`.
- Stdout and stderr are captured separately.
- Each stream is capped at `1 MiB` and reports a truncation flag when clipped.
- Non-zero command exit is terminal and is not retried by the coordinator.

This is allowlisted direct execution on trusted hosts. It is not strong
sandbox, container, VM, or multi-tenant isolation.

### Workflow Templates

The current template layer is local to `pmctl`.

Templates:

- are JSON `version: 1` files loaded from explicit operator-provided paths
- reference one logical allowlist command key
- define ordered literal and parameter argument tokens
- expand into one command and argument vector; invocation-time placement flags
  select the constrained command endpoint without changing the template file
- are not stored by the coordinator or agents

Template validation rejects unsupported JSON fields, unknown template versions,
duplicate parameters, unknown parameter references, unsafe command keys, missing
required `--set` values, unknown `--set` values, and duplicate `--set` values.
Template inspection and preview are local-only `pmctl` ergonomics: inspection
shows template metadata and argument token structure, and preview shows the
expanded command-job vector without creating a job, contacting the coordinator,
or checking agent allowlists. Successful submission still creates an ordinary
command job. The coordinator stores the expanded command, args, and
invocation-time capability requirements; it does not store the template name
or parameter map.

Templates do not transfer files, store artifacts, declare placement fields,
manage secrets, override timeouts, cancel jobs, or create multi-step workflows.
`--require-capability` is submission metadata and does not alter strict template
schema version `1`.

### Scheduling and Dispatch

Current dispatch behavior:

1. A client submits an unconstrained job to `POST /jobs` or a placement-aware
   command job to `POST /jobs/command`.
2. Coordinator validates and stores the job as `QUEUED`.
3. A goroutine attempts dispatch immediately.
4. Coordinator reads one node snapshot, keeps only `HEALTHY` nodes containing
   every required capability, and sorts candidates by
   `load.active_executions` ascending then node ID ascending.
5. Coordinator marks the job `RUNNING` for each attempt.
6. Coordinator sends an HTTP/JSON `POST /execute` request to the selected agent.
7. Retryable failures are retried on that node up to the configured attempt
   count, then reassigned to the next node in the same candidate snapshot.
8. Coordinator records the terminal result as `COMPLETED` or `FAILED`.

Queued scheduler behavior:

- the coordinator periodically lists jobs still in `QUEUED` state
- if no matching healthy node exists, queued jobs remain queued with no new
  attempt and no agent contact
- a later heartbeat that adds the required labels or makes a matching node
  healthy can make the job eligible on a later scheduler pass
- if a queued job is still waiting after 24 hours, the coordinator marks it
  `FAILED` with
  `queued job expired before an eligible healthy node became available`
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
  next matching node from the fixed candidate snapshot
- if all eligible healthy nodes fail retryably, the job fails with the last
  retryable error
- node state changes to `SUSPECT` or `OFFLINE` do not cancel an already
  in-flight execution attempt in v0

Unconstrained jobs use the same ordering across all `HEALTHY` nodes. Reported
load is a heartbeat snapshot, not a reservation or capacity guarantee;
concurrent dispatches can choose the same node before a later heartbeat.
Capability labels are operator assertions and do not prove hardware, software,
allowlist entries, agent-local files, identity, or actual capacity. The
scheduler adds no priorities, quotas, fairness, reservations, or queue-depth
model.

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
| `RUNNING` | successful synchronous or matching reported agent execution | `COMPLETED` |
| `RUNNING` | terminal synchronous or matching reported execution failure | `FAILED` |
| `RUNNING` | terminal dispatch failure | `FAILED` |
| `QUEUED` | queued expiration or pre-attempt coordinator failure | `FAILED` |
| `RUNNING` | Postgres reconciliation grace expires without a matching result report | `FAILED` |

If no healthy node exists, the job remains `QUEUED` with no attempts recorded.
Duplicate dispatch protection is process-local and skips concurrent dispatches
for the same job in one running coordinator. Terminal jobs are not overwritten
by later lifecycle methods.

### Restart Recovery and Reconciliation

Current runtime behavior:

- in-memory coordinator state is lost on restart
- Postgres startup captures persisted `RUNNING` job ids and leaves those jobs
  `RUNNING` during a bounded reconciliation grace window
- the default grace is `30s` and can be configured with
  `COORDINATOR_RECONCILIATION_GRACE`
- the coordinator starts serving HTTP during the grace window
- a matching `POST /jobs/{id}/result` report can complete or fail a captured
  `RUNNING` job during grace
- when grace expires, only the remaining captured startup `RUNNING` job ids are
  marked `FAILED` with
  `coordinator restarted before result was recorded`
- `COORDINATOR_RECONCILIATION_GRACE=0s` preserves immediate startup failure
  behavior for Postgres-backed coordinators
- agents keep only bounded in-memory result history, so agent restart loses
  cached reports
- commands are still tied to the `/execute` request context; this is not full
  in-progress execution recovery after a dropped coordinator connection

ADR 0013 records the reconciliation strategy. Milestone 15 implements its first
runtime slice while keeping protocol version `1`, public job JSON fields,
nodes/jobs-only storage, and Postgres schema readiness version `2` unchanged.
Terminal `COMPLETED` or `FAILED` jobs remain immutable.

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

### Linux Managed-Service Deployment

Linux release archives add a pre-release systemd deployment layer around the
existing coordinator and agent entrypoints. It does not add a runtime
component, endpoint, protocol field, storage type, or job state.

- Coordinator and agent install independently as
  `planetary-mesh-coordinator.service` and
  `planetary-mesh-agent.service`.
- Fixed binaries live under `/opt/planetary-mesh/`; coordinator installation
  also provides `/usr/local/bin/pmctl` and templates under `/usr/share`, while
  agent installation provides the packaged `text-stats` example workload.
- Separate stable system users and groups run the roles from
  `/var/lib/planetary-mesh/{coordinator,agent}`.
- Operator-provided env-style configuration is copied to role-specific `0640`
  files under `/etc/planetary-mesh`; credentials are not embedded in unit
  files. TLS files remain manually provisioned.
- Both units invoke the existing binary with `--config <managed-path>`, emit
  existing JSON logs to journald, use `Restart=on-failure`, and receive
  `SIGTERM` with a 15-second systemd stop window around the existing ten-second
  graceful shutdown.
- Conservative systemd hardening reduces daemon privilege without restricting
  supported networking, certificate reads, agent-local inputs, `/tmp`, device
  access, or external allowlisted wrappers. It is not workload sandboxing.

Service supervision does not alter execution recovery. Agent stop can interrupt
active work; in-memory coordinator restart loses state; Postgres-backed restart
uses existing bounded reconciliation; and no command transparently continues
because systemd restarts a daemon.

## Current Limitations

Current private-mesh limitations:

- queued-job scheduling is periodic and uses a fixed 24-hour queued-job
  expiration window
- capability/load placement uses unverified heartbeat snapshots; there is no
  reservation, capacity guarantee, priority, quota, or fairness model
- reconciliation is best-effort only: agents cache recent terminal results in
  memory, agent restart loses cached reports, and dropped in-progress
  `/execute` requests can still leave no terminal result to report
- no strong sandbox/container/VM isolation
- no per-job timeout override
- no file upload/result download workflow
- no dashboard
- no direct agent diagnostics, allowlist discovery, executable/file checks, or
  log aggregation; `pmctl doctor` uses coordinator-reported snapshots only
- no generated API contract such as OpenAPI or protobuf; the current v0 API
  reference is a manual inventory
- no cancellation API or cancellation behavior
- pre-release local binary artifacts and Linux/systemd service installation
  exist, but there is no production image, signed distribution, package-manager
  delivery, GitHub Release artifact, automatic upgrade, non-Linux service
  installer, or captured real systemd activation evidence
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

- richer capacity, priority, quota, or fairness policy if operational evidence
  justifies it
- operator runbooks
- future generated API contract decision if the manual inventory proves
  insufficient
- production install/release workflow
- certificate/onboarding helper
- follow-up reconciliation hardening if private mesh operations show the need
  for durable agent-side result history or richer recovery semantics

Productized private mesh:

- richer CLI beyond the current template inspect/preview workflow, or dashboard
- additional approved template examples after their wrapper paths are explicit
- logs UX
- file/result handling for selected workflows
- private AI/batch demo pipelines
- stronger production packaging and release story

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
