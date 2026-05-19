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

Current protocol rules:

- versioned requests use `X-Planetary-Protocol-Version: 1`
- coordinator `/healthz` and agent `/healthz` are simple health checks
- other coordinator endpoints and agent `/execute` require the protocol header

Future decisions:

- OpenAPI, protobuf, gRPC, streaming logs, WebSockets, or SSE are not current
  commitments. Add them only after an ADR or roadmap update.

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

Current constraints:

- persist nodes and jobs only
- no task fanout table today
- no SQLite or alternate durable backend today
- Postgres tests are opt-in
- embedded schema initialization remains current
- schema readiness metadata version `1` is current
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
- `compose.yaml` for local coordinator + Postgres + agents

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
- agents map allowlist keys to executable paths/names
- agents use `exec.CommandContext`
- agents do not invoke a shell
- timeout is fixed by agent config, default `30s`
- stdout/stderr are captured separately and capped at `1 MiB` each
- non-zero command exit is terminal

Important limitation:

- This is not strong sandboxing. There is no container, VM, microVM, or
  multi-tenant isolation today.

Future decisions:

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

Future decisions:

- richer CLI UX
- dashboard
- API contract generation
- operator auth model

## Observability

Choice: structured logs and basic coordinator metrics.

Current observability:

- coordinator and agent structured logs
- `/status` for non-secret runtime status/config
- `/metrics` for Prometheus-style counters and gauges
- Postgres schema readiness metrics when Postgres is enabled
- startup recovery metric for persisted `RUNNING` jobs

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
