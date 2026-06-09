# Tech Choices

This document records current technology and pattern choices for Planetary Mesh.
Accepted decisions are backed by ADRs where appropriate.

Planetary Mesh is currently a private-first Go compute mesh prototype for
allowlisted command jobs across trusted machines. Future protocol, isolation,
marketplace, and payment systems require separate decisions.

## Process and Documentation

Choice: iterative/incremental development with lightweight documentation.

Why:

- The project is exploratory and benefits from small reviewed slices.
- Distributed execution and security-sensitive command handling require written
  decisions.
- ADRs keep the reason for major choices visible as the project evolves.

Related ADR:

- [ADR 0001](adr/0001-process-and-docs.md)

## Implementation Language

Choice: Go for coordinator, agent, and `pmctl`.

Go is the accepted current implementation language, not a tentative preference.

Why:

- simple single-binary deployment for coordinator and agents
- strong standard library for HTTP, TLS, JSON, concurrency, and process control
- good fit for long-running daemons
- straightforward CI and local development workflow

Related ADR:

- [ADR 0002](adr/0002-language-choice.md)

Future note:

- Workload code can still call external tools or scripts. That does not change
  the control-plane implementation choice.

## Control Plane Protocol

Choice: HTTP/JSON for v0 coordinator-agent and client-coordinator control plane.

Why:

- easy to debug with `curl`
- no code generation requirement
- stable enough for current agent, coordinator, and `pmctl` clients
- compatible with optional HTTPS/mTLS without changing the JSON wire shape

Related ADR:

- [ADR 0003](adr/0003-http-json-control-plane-for-v0.md)
- [ADR 0014](adr/0014-http-json-v0-api-inventory-and-compatibility.md)

Current protocol rules:

- versioned requests use `X-Planetary-Protocol-Version: 1`
- coordinator `/healthz` and agent `/healthz` are simple health checks
- other coordinator endpoints and agent `/execute` require the protocol header
- [api-http-json-v0.md](api-http-json-v0.md) is the authoritative manual v0 API
  inventory and compatibility policy

Future decisions:

- OpenAPI, protobuf, gRPC, streaming logs, WebSockets, or SSE are not current
  commitments. Add them only after an ADR or roadmap update, using the manual
  API inventory as the baseline.

## Storage

Choice: in-memory storage by default; optional Postgres for durable coordinator
nodes/jobs.

Why in-memory remains:

- fast default unit tests
- simple local development
- no external service requirement for ordinary `go test ./...`

Why Postgres:

- nodes and jobs are naturally relational
- durable job history matters after command execution exists
- transactions and constraints are useful for future operations/reporting
- provider-neutral `COORDINATOR_DATABASE_URL` keeps deployment flexible

Related ADRs:

- [ADR 0004](adr/0004-in-memory-storage-for-v0.md)
- [ADR 0006](adr/0006-postgres-coordinator-persistence.md)
- [ADR 0010](adr/0010-postgres-schema-readiness.md)
- [ADR 0011](adr/0011-node-capability-load-visibility.md)
- [ADR 0013](adr/0013-agent-reconciliation-strategy.md)

Current constraints:

- persist nodes and jobs only
- no task fanout table today
- no SQLite or alternate durable backend today
- Postgres tests are opt-in
- embedded schema initialization remains current
- schema readiness metadata version `2` is current
- node rows store reported capabilities and active execution count
- result reporting and Postgres startup reconciliation use existing job rows;
  Milestone 15 did not add schema changes
- schema readiness metadata is not a full migration framework

Future decisions:

- full schema migration framework
- task/fanout storage
- historical reporting tables
- hosted database provider choices

## Deployment and Local Development

Choice: local binaries are first-class; Docker Compose supports Postgres-backed
local demos.

Current shape:

- `go run ./cmd/coordinator`
- `go run ./cmd/agent`
- `go install ./cmd/pmctl`
- optional env-style config files
- `examples/demo.sh` for in-memory smoke workflow
- `examples/postgres_smoke.sh` for opt-in durable Postgres smoke workflow
- `examples/external_workload_smoke.sh` for the tracked external workload path
- `examples/workloads/text-stats` as a small cross-platform external helper
- `compose.yaml` for local coordinator + Postgres + agents
- task-oriented operator runbooks under [runbooks/](runbooks/README.md)

Related ADR:

- [ADR 0009](adr/0009-env-style-local-config-files.md)

Future decisions:

- production Dockerfile/image
- release packaging
- install scripts
- systemd or launchd service examples
- Kubernetes-style orchestration, if ever needed

Kubernetes is not a current product dependency or target. The current wedge is
simpler private job execution, not a Kubernetes replacement.

## Execution Model

Choice: allowlisted direct command execution for the first real workload.

Why:

- proves useful remote execution with minimal machinery
- keeps the first workload inspectable and testable
- avoids shell injection paths by never invoking a shell
- gives operators explicit local control through an allowlist

Related ADR:

- [ADR 0005](adr/0005-command-execution-v0.md)

Current rules:

- jobs use `type="command"`
- `command` is a logical allowlist key
- `args` is an argument vector
- `payload` is rejected for command jobs
- agents map allowlist keys to executable paths/names or reserved
  `builtin:<name>` validation targets
- agents use `exec.CommandContext` for external executable targets
- agents do not invoke a shell
- portable validation built-ins currently cover `echo`, `false`, `sleep`, and
  agent-local `line-count`
- built-ins are intentionally small, stable validation helpers and are not a
  generic plugin or workflow framework
- real private workflows should use explicit allowlisted external commands or
  wrapper executables; `examples/workloads/text-stats` is the tracked example
  of this pattern
- timeout is fixed by agent config, default `30s`
- stdout/stderr are captured separately and capped at `1 MiB` each
- non-zero command exit is terminal

Important limitation:

- This is not strong sandboxing. There is no container, VM, microVM, or
  multi-tenant isolation today.

Future decisions:

- workflow/job templates that expose approved private actions while still
  mapping to allowlisted commands or wrapper scripts
- container-based execution
- VM/microVM execution
- per-job resource limits
- approved workload templates
- stronger isolation for shared or marketplace compute

## Scheduling Strategy

Choice: simple first-healthy-node initial dispatch with cross-node retry for
retryable dispatch failures.

Why:

- enough to prove registration, heartbeat, dispatch, retries, command execution,
  persistence, mTLS, and CLI workflows
- keeps early behavior easy to inspect and test

Current behavior:

- a job is stored as `QUEUED`
- dispatch attempts immediately after submission
- coordinator selects the first node currently in `HEALTHY` state
- reported node capabilities and active execution counts are visible to
  operators but are not used for scheduling
- a coordinator-owned scheduler periodically revisits jobs that remain `QUEUED`
- retryable dispatch failures are retried against the selected node up to the
  configured attempt count
- after those retryable attempts are exhausted, the coordinator tries another
  `HEALTHY` node that has not already been attempted in that dispatch cycle
- if all eligible healthy nodes fail retryably, the job is marked `FAILED` with
  the last retryable error
- if no healthy node exists, the job remains queued until a later scheduler pass
  sees a healthy node
- if no healthy node becomes available within 24 hours, the queued job is marked
  `FAILED`
- duplicate concurrent dispatch of the same job is skipped within one running
  coordinator process

Future decisions:

- configurable queued-job expiration
- capability-aware scheduling
- load-aware scheduling
- priorities, quotas, and fairness

## Job Lifecycle State Model

Choice: explicit coordinator-owned state transitions for the current job
lifecycle.

Why:

- private mesh operators need job status to be predictable across memory and
  Postgres storage
- tests should protect retry, reassignment, queued expiration, and restart
  recovery from accidental state drift
- the current API is useful enough without adding cancellation or new statuses

Current behavior:

- accepted jobs start as `QUEUED`
- dispatch attempts transition `QUEUED` to `RUNNING`
- retry and cross-node reassignment attempts keep the job `RUNNING` while
  incrementing attempts and updating the current node
- successful execution transitions `RUNNING` to `COMPLETED`
- terminal execution or dispatch failures transition `RUNNING` to `FAILED`
- accepted matching terminal result reports can transition `RUNNING` jobs to
  `COMPLETED` or `FAILED`
- queued expiration can transition `QUEUED` to `FAILED`
- Postgres startup recovery captures persisted `RUNNING` job ids, leaves them
  `RUNNING` during reconciliation grace, and transitions only remaining
  captured ids to `FAILED` after grace expires
- terminal `COMPLETED` and `FAILED` jobs are not overwritten by lifecycle
  methods
- `CANCELLED` remains reserved/unsupported; there is no cancellation API today

Current constraints:

- public job JSON fields and status strings are unchanged
- HTTP/JSON protocol version remains `1`
- Postgres schema readiness metadata remains version `2`
- duplicate dispatch protection is process-local to one running coordinator

Future decisions:

- cancellation semantics
- durable agent result history, if best-effort in-memory reconciliation proves
  insufficient
- richer progress states, if a future workload model needs them

## Agent Reconciliation Strategy

Choice: explicit agent-to-coordinator terminal result reporting with bounded
Postgres startup reconciliation grace.

Related ADR:

- [ADR 0013](adr/0013-agent-reconciliation-strategy.md)

Current behavior:

- the coordinator records terminal results from the synchronous agent
  `/execute` response and can also accept matching terminal result reports at
  `POST /jobs/{id}/result`
- reported status is limited to `COMPLETED` and `FAILED`
- reported results are accepted only for existing `RUNNING` jobs whose current
  node id matches the reporting node
- duplicate, late, wrong-node, unknown-job, unsupported-status, and
  unsupported-state reports do not mutate jobs
- agents keep a bounded in-memory result cache for recent terminal command
  outcomes and report best-effort while still returning synchronous `/execute`
  responses
- agent restart loses cached reports
- Postgres startup captures persisted `RUNNING` job ids, serves during
  reconciliation grace, accepts matching reports during grace, and fails only
  remaining captured ids when grace expires
- in-memory storage has no restart recovery
- commands remain tied to the `/execute` request context, so this is not full
  in-progress execution recovery
- HTTP/JSON and `X-Planetary-Protocol-Version: 1` remain unchanged
- terminal job immutability remains absolute
- nodes/jobs-only storage and schema readiness version `2` remain unchanged

## Node Capability and Load Reporting

Choice: agents report optional static capabilities and approximate active
execution count through registration/heartbeat.

Why:

- private mesh operators need more useful node inspection than id/address/state
- static labels are simple to configure and do not require hardware discovery
- active command execution count is feasible with the current agent execution
  model
- keeping reporting separate from scheduling preserves current dispatch behavior

Current behavior:

- `AGENT_CAPABILITIES` is a comma-separated list of labels such as
  `profile:local,role:worker`
- labels are validated, deduplicated, sorted, and stored as node metadata
- `active_executions` counts accepted command executions currently running on
  the agent
- missing metadata from older agents defaults to empty capabilities and zero
  active executions
- `GET /nodes` and `pmctl nodes list` expose the metadata
- Postgres persists the metadata in the `nodes` table

Future decisions:

- capacity reporting
- queue depth reporting
- hardware or runtime discovery
- capability-aware or load-aware scheduler policy

## Security Model

Choice: opt-in mTLS and node allowlists for trusted LAN security.

Related ADR:

- [ADR 0007](adr/0007-mtls-trusted-lan-security.md)

Current reality:

- plain HTTP remains available for local development
- mTLS is enabled by configuring CA/cert/key files
- coordinator secure mode requires node identity or fingerprint allowlists
- registration enforces the allowlist in secure mode
- certificate lifecycle is manual

Future decisions:

- certificate generation helper
- enrollment/rotation workflow
- remote-node trust bootstrap
- user/operator auth model
- multi-tenant authorization
- stronger secret-management and deployment hardening

Do not describe the project as secure-by-default production infrastructure. It
is a trusted LAN/private-network prototype with opt-in mTLS.

## Operator Interface

Choice: thin CLI over coordinator APIs.

Related ADR:

- [ADR 0008](adr/0008-operator-cli.md)

Why:

- improves usability over raw `curl`
- keeps coordinator-owned behavior in the coordinator
- supports secure coordinator access with CA/cert/key files
- provides human-readable and JSON output

Current commands:

- `pmctl status`
- `pmctl nodes list`
- `pmctl jobs list`
- `pmctl jobs inspect <job-id>`
- `pmctl submit command <command> [args...]`

`pmctl nodes list` includes node state, active execution count, capabilities,
address, last seen time, and certificate fingerprint. JSON output includes the
same node metadata for automation.

Future decisions:

- richer CLI UX
- dashboard
- API contract generation
- operator auth model

## Observability

Choice: structured logs and basic coordinator metrics.

Current observability:

- coordinator and agent structured logs
- `/status` for non-secret runtime status/config, including additive
  reconciliation metadata when configured
- `/metrics` for Prometheus-style counters and gauges, including reported
  result counters and reconciliation pending-job gauge
- `/nodes` and `pmctl nodes list` for node capability/load visibility
- Postgres schema readiness metrics when Postgres is enabled
- startup recovery metric for persisted `RUNNING` jobs failed after
  reconciliation grace

Future decisions:

- richer logs UX
- tracing
- dashboard
- alerting/runbook conventions

## Product-System Boundaries

Current product boundary:

- private/local compute mesh
- machines are owned or controlled by the same user/team
- allowlisted command-job execution

Not current commitments:

- public compute marketplace
- crypto/token system
- payment or payout system
- arbitrary untrusted compute
- production multi-tenant platform
- GPU/storage/bandwidth marketplace
- Kubernetes/Ray/Airflow/Temporal replacement

Future product phases should be decided in the roadmap and supported by ADRs
when they introduce non-trivial architecture, security, storage, protocol, or
operational choices.
