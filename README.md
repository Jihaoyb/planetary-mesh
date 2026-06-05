# Planetary Mesh

Planetary Mesh is a lightweight private compute mesh for running
command-based jobs across machines you own or control, with a future path
toward trusted overflow compute.

The current project is a Go 1.25.4 LAN/private-network prototype. It is useful
for proving coordinator-agent job dispatch, allowlisted command execution,
optional durability, and operator workflows before expanding into remote or
shared compute scenarios.

## Current Status

- **Stage**: Phase 2 source-based onboarding has started after Milestone 21
  first-run private mesh onboarding. Phase 1 closed after the Milestone 20
  readiness review, Milestone 18 real multi-device LAN validation, and
  Milestone 19 portable agent validation built-ins.
- **Coordinator**: registers agents, tracks node health, accepts jobs, dispatches
  first to the first healthy node, reassigns after retryable dispatch failures,
  periodically revisits queued jobs, owns explicit job lifecycle transitions,
  accepts additive terminal result reports from agents, exposes metrics and
  status, stores reported node capabilities/load, and can persist nodes and jobs
  in Postgres.
- **Agent**: registers with the coordinator, sends heartbeats with optional
  static capabilities and current active execution count, executes allowlisted
  command jobs without invoking a shell, including explicit portable validation
  built-ins when configured, and best-effort reports terminal command results
  from a bounded in-memory cache.
- **CLI**: `pmctl` is a thin client for status, node listing, job listing, job
  inspection, and command job submission.
- **Security**: plain HTTP is available for local development; mTLS and node
  allowlists are supported but opt-in and manually configured.
- **Persistence**: in-memory storage is the default; Postgres durability is
  optional and includes lightweight schema readiness metadata version `2`.
  Postgres startup leaves persisted `RUNNING` jobs in a bounded reconciliation
  grace window before failing unreconciled jobs.

## What Works Today

- HTTP/JSON control plane with `X-Planetary-Protocol-Version: 1`.
- Manual HTTP/JSON v0 API inventory and compatibility policy.
- Coordinator `GET /healthz`, `GET /status`, `POST /register`, `GET /nodes`,
  `POST /jobs`, `GET /jobs`, `GET /jobs/{id}`, `POST /jobs/{id}/result`, and
  `GET /metrics`.
- Agent `GET /healthz` and `POST /execute`.
- Node registration and heartbeat through repeated `POST /register` calls.
- Node health states: `HEALTHY`, `SUSPECT`, and `OFFLINE`.
- Operator-visible node capabilities and active execution counts reported
  through registration/heartbeat, `GET /nodes`, and `pmctl nodes list`.
- Command job submission with `type="command"`, `command`, and optional `args`.
- Explicit coordinator-owned job lifecycle states: `QUEUED`, `RUNNING`,
  `COMPLETED`, and `FAILED`.
- Allowlisted external process execution using `exec.CommandContext`.
- Explicit no-shell built-in validation targets for `echo`, `false`, `sleep`,
  and agent-local line counting when mapped through `AGENT_COMMAND_ALLOWLIST`.
- No shell execution and no arbitrary executable paths from job submissions.
- Fixed agent execution timeout, default `30s`.
- Bounded stdout and stderr capture with per-stream truncation flags.
- Retry handling for retryable dispatch failures such as transport errors and
  agent `5xx` responses.
- Cross-node reassignment after retryable dispatch failures when the selected
  healthy node exhausts its configured attempts.
- Periodic coordinator-owned re-dispatch for jobs left `QUEUED` because no
  healthy node existed at submission time.
- Queued jobs expire as `FAILED` after 24 hours if no healthy node becomes
  available.
- Optional Postgres persistence for nodes/jobs, node capability/load metadata,
  bounded startup reconciliation grace for persisted `RUNNING` jobs, and schema
  readiness reporting.
- Sanitized real multi-device LAN validation evidence across macOS, Linux, and
  Windows coordinator/agent pairs.
- Phase 1 readiness review with no remaining Phase 1 exit blockers.
- Source-based first-run onboarding from local smoke to a two-machine LAN mesh.
- Config-driven local smoke demos and a Compose-backed Postgres smoke workflow.

## Not Implemented Yet

- Strong sandbox, container, VM, or multi-tenant isolation.
- Public marketplace, payment, payout, dispute, reputation, or public provider
  onboarding features.
- GPU, storage, or bandwidth pooling as implemented product capabilities.
- Dashboard or rich operator UI.
- OpenAPI, protobuf, or generated API contracts.
- Production Docker image or packaged release workflow.
- Load-aware, capability-aware, or queue-aware scheduling.
- Job cancellation API or cancellation behavior.
- Durable agent result history after agent restart.
- Full in-progress execution recovery after coordinator restart.
- Automated certificate issuance, enrollment, or rotation.
- Remote private mesh, trusted shared pool, or overflow marketplace features.

See [docs/current-limitations.md](docs/current-limitations.md) for the current
limitation and risk register.

Task-oriented operating guides are in
[docs/runbooks/README.md](docs/runbooks/README.md). Start with
[docs/runbooks/first-run-private-mesh.md](docs/runbooks/first-run-private-mesh.md)
for the source-based first-run path from local smoke to a two-machine LAN mesh.
Milestone 18 real LAN validation guidance and sanitized completion evidence are
in
[docs/runbooks/real-lan-validation.md](docs/runbooks/real-lan-validation.md),
with a practical `line-count` workload recipe in
[docs/runbooks/practical-workload-recipe.md](docs/runbooks/practical-workload-recipe.md).
A partial macOS-to-Windows LAN validation reached remote dispatch but exposed a
portable command-example gap: no-shell agents need real platform executables,
not shell built-ins such as Windows `echo`. Milestone 19 added explicit
portable validation built-ins to address that gap, and Milestone 18 then
captured successful sanitized LAN validation evidence.
Milestone 20 reviewed that evidence and closed Phase 1. Milestone 21 added a
source-based first-run onboarding path. Remaining gaps such as packaging,
scheduler policy, cancellation, generated API contracts, richer operator UX,
workflow templates, and stronger isolation are Phase 2 or later backlog unless
explicitly reclassified.

## Architecture Summary

Planetary Mesh has three runtime components:

- **Coordinator**: the single v0 control plane. It owns validation, node state
  and metadata, job state, dispatch, retry policy, storage selection, metrics,
  and status.
- **Agent**: a worker daemon on a trusted machine. It registers with the
  coordinator, sends heartbeats, exposes `/execute`, and runs only locally
  allowlisted commands.
- **pmctl**: a thin operator CLI over the coordinator HTTP/JSON API. It does not
  own scheduling, validation, state transitions, or storage behavior.

The default local runtime uses in-memory coordinator storage and plain HTTP.
When `COORDINATOR_DATABASE_URL` is configured, the coordinator persists nodes
and jobs in Postgres. When TLS files and node allowlists are configured, the
coordinator-agent path can run with mTLS and explicit node admission.

The detailed architecture is in [docs/architecture.md](docs/architecture.md).

## Execution and Security Model

Command execution is security-sensitive. The current implementation narrows the
trust boundary but is not a strong sandbox:

- Job submissions name a logical command key, not an executable path.
- Agents map logical command names to executable paths or reserved built-in
  targets through
  `AGENT_COMMAND_ALLOWLIST`.
- Operators can label agents with static `AGENT_CAPABILITIES`.
- Agents execute external command targets directly with `exec.CommandContext`.
- Agents never execute through a shell.
- Built-in targets are explicit validation helpers, not shell built-ins and not
  stronger isolation.
- Built-ins are not a general workflow extension model. Real private workflows
  should continue to use explicit allowlisted external commands or wrapper
  scripts.
- Stdout and stderr are captured separately and capped at `1 MiB` per stream.
- mTLS and node allowlists are supported for trusted LAN operation, but
  certificate generation, distribution, enrollment, and rotation are manual.

Use the current prototype only with machines and workloads you trust.

## Repository Layout

```text
planetary-mesh/
  README.md
  AGENTS.md
  go.mod

  cmd/
    coordinator/       # Coordinator service entrypoint
    agent/             # Agent daemon entrypoint
    pmctl/             # Operator CLI entrypoint

  internal/
    coordinator/       # Coordinator handlers, stores, dispatch, metrics, tests
    agent/             # Agent handlers, coordinator client, executor, tests
    pmctl/             # CLI command parsing, output, and coordinator client
    protocol/          # Shared protocol constants and wire structs
    security/          # TLS, certificate identity, and allowlist helpers
    configfile/        # Env-style config file parser

  config/              # Tracked example config files
  docs/                # Roadmap, architecture, product, ADRs, limitations
  examples/            # Local and Postgres smoke workflows
  compose.yaml         # Local coordinator + Postgres + agents demo
```

## Build and Test

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .
git diff --check
```

If the Go cache must be pinned outside the default location:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
```

Postgres integration tests are opt-in:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-postgres \
go test -tags postgres ./internal/coordinator
```

The default `go test ./...` path remains DB-free.

## First Run From Source

From a fresh checkout with Go, `curl`, and `python3` available:

```bash
./examples/demo.sh
```

The demo builds temporary local binaries, starts one coordinator and two agents
from tracked config examples, submits an allowlisted command through `pmctl`,
lists nodes/jobs, and inspects the completed job. It uses in-memory storage and
plain HTTP by default.

Expected first result:

- `Smoke demo completed successfully`
- coordinator status `ok`, protocol version `1`, and `in_memory` storage
- `local-agent-1` and `local-agent-2` listed as `HEALTHY`
- a completed `echo` job with stdout `hello from planetary mesh`

For manual component startup, a two-machine LAN path, a `line-count` workload
against an agent-local file, cleanup, and failure handling, follow the
[First-Run Private Mesh Onboarding](docs/runbooks/first-run-private-mesh.md)
runbook.

For durable-state verification with Docker Compose:

```bash
./examples/postgres_smoke.sh
```

That workflow starts coordinator + Postgres + agents, verifies status, schema
readiness, and reconciliation metadata, checks deferred restart recovery for
persisted `RUNNING` jobs, checks `/metrics`, and submits another command after
restart.

For step-by-step operator workflows, see the
[operator runbooks](docs/runbooks/README.md).

For real multi-device LAN validation, use
[docs/runbooks/real-lan-validation.md](docs/runbooks/real-lan-validation.md).

## Run Manually

Start the coordinator:

```bash
COORDINATOR_ADDR=:8080 go run ./cmd/coordinator
```

Start an agent in another terminal:

```bash
COORDINATOR_URL=http://localhost:8080 \
AGENT_ADDR=:8081 \
AGENT_CAPABILITIES='profile:local,role:worker' \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep' \
go run ./cmd/agent
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
```

Submit a command job:

```bash
curl -X POST http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1' \
  -H 'Content-Type: application/json' \
  -d '{"type":"command","command":"echo","args":["hello mesh"]}'
```

List nodes and jobs:

```bash
curl http://localhost:8080/nodes \
  -H 'X-Planetary-Protocol-Version: 1'

curl http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1'
```

## Optional Postgres Storage

By default, coordinator state is in-memory. To persist nodes and jobs:

```bash
COORDINATOR_DATABASE_URL='postgres://planetary:planetary@localhost:5432/planetary_mesh?sslmode=disable' \
go run ./cmd/coordinator
```

Postgres startup uses embedded schema initialization plus lightweight schema
readiness metadata. `schema_version` value `2` represents the current nodes/jobs
schema, node capability/load columns, and readiness marker. The coordinator
exposes schema readiness through startup logs, `/status`, `/metrics`,
`pmctl --json status`, tests, and the Postgres smoke workflow.

When Postgres is enabled, persisted `RUNNING` jobs found at coordinator startup
enter a bounded reconciliation grace window. The default is `30s` and can be set
with:

```bash
COORDINATOR_RECONCILIATION_GRACE=30s
```

During the grace window, matching terminal reports from the assigned agent can
complete or fail the job. If no report arrives, the coordinator marks the
captured startup-running job `FAILED` with
`coordinator restarted before result was recorded`. In-memory coordinator
restart still loses state, and agent result history is in-memory only.

This is not a full migration framework. A database marked with a newer schema
version than the running coordinator expects is rejected at startup.

## Optional mTLS Mode

Plain HTTP remains the default for local development. To enable mTLS between
coordinator and agents, manually provision a CA plus coordinator and agent
certificates, then configure both sides.

Coordinator:

```bash
COORDINATOR_TLS_CA_FILE=./certs/ca.pem \
COORDINATOR_TLS_CERT_FILE=./certs/coordinator.pem \
COORDINATOR_TLS_KEY_FILE=./certs/coordinator-key.pem \
COORDINATOR_ALLOWED_NODE_IDENTITIES='agent-1=dns:agent-1.local' \
go run ./cmd/coordinator
```

Agent:

```bash
COORDINATOR_URL=https://localhost:8080 \
NODE_ID=agent-1 \
AGENT_TLS_CA_FILE=./certs/ca.pem \
AGENT_TLS_CERT_FILE=./certs/agent-1.pem \
AGENT_TLS_KEY_FILE=./certs/agent-1-key.pem \
AGENT_CAPABILITIES='profile:local,role:worker' \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep' \
go run ./cmd/agent
```

Allowed node identities use `node-id=identity` entries, where identity can be
`dns:name`, `ip:addr`, `uri:value`, `cn:name`, or `subject:value`. Fingerprint
allowlists use SHA-256 certificate fingerprints:

```bash
COORDINATOR_ALLOWED_NODE_FINGERPRINTS='agent-1=<sha256-hex-fingerprint>'
```

Certificate generation, distribution, enrollment, and rotation remain manual.

## Operator CLI

`pmctl` is a thin client over the coordinator API:

```bash
go install ./cmd/pmctl

pmctl status
pmctl nodes list
pmctl jobs list
pmctl submit command echo hello mesh
pmctl jobs inspect job-1
```

Use JSON output for automation:

```bash
pmctl --json jobs inspect job-1
pmctl --json nodes list
```

Point at another coordinator with a flag, environment variable, or config file:

```bash
pmctl --coordinator-url http://localhost:9090 status
PMCTL_COORDINATOR_URL=http://localhost:9090 pmctl nodes list
pmctl --config config/pmctl.env.example status
```

For secure coordinator access, provide the CA and operator client certificate:

```bash
pmctl \
  --coordinator-url https://localhost:8080 \
  --ca-file ./certs/ca.pem \
  --cert-file ./certs/operator.pem \
  --key-file ./certs/operator-key.pem \
  nodes list
```

## Compose Demo

Run a local coordinator + Postgres + two-agent stack:

```bash
docker compose up
```

In another terminal:

```bash
go run ./cmd/pmctl nodes list
go run ./cmd/pmctl submit command echo "hello from compose"
go run ./cmd/pmctl jobs inspect job-1
```

The default Compose host ports are `5432`, `8080`, `8081`, and `8082`. Override
them if another stack is running:

```bash
POSTGRES_HOST_PORT=15432 \
COORDINATOR_HOST_PORT=18080 \
AGENT1_HOST_PORT=18081 \
AGENT2_HOST_PORT=18082 \
docker compose up
```

## Documentation Index

Current sources of truth:

- [Product Positioning](docs/product-positioning.md)
- [Roadmap](docs/roadmap.md)
- [Phase 1 Readiness Review](docs/phase-1-readiness-review.md)
- [Architecture](docs/architecture.md)
- [HTTP/JSON v0 API Inventory](docs/api-http-json-v0.md)
- [Operator Runbooks](docs/runbooks/README.md)
- [First-Run Private Mesh Onboarding](docs/runbooks/first-run-private-mesh.md)
- [Current Limitations](docs/current-limitations.md)
- [Product Requirements](docs/product-requirements.md)
- [Tech Choices](docs/tech-choices.md)

Historical context:

- [Kickoff Plan](docs/kickoff.md)

Architecture Decision Records:

- [ADR 0001: Process and docs](docs/adr/0001-process-and-docs.md)
- [ADR 0002: Go for coordinator and agent services](docs/adr/0002-language-choice.md)
- [ADR 0003: HTTP/JSON for v0 control plane](docs/adr/0003-http-json-control-plane-for-v0.md)
- [ADR 0004: In-memory storage for v0 control plane](docs/adr/0004-in-memory-storage-for-v0.md)
- [ADR 0005: Allowlisted direct command execution](docs/adr/0005-command-execution-v0.md)
- [ADR 0006: Postgres durable coordinator state](docs/adr/0006-postgres-coordinator-persistence.md)
- [ADR 0007: mTLS trusted LAN security](docs/adr/0007-mtls-trusted-lan-security.md)
- [ADR 0008: Thin operator CLI](docs/adr/0008-operator-cli.md)
- [ADR 0009: Env-style local config files](docs/adr/0009-env-style-local-config-files.md)
- [ADR 0010: Postgres schema readiness metadata](docs/adr/0010-postgres-schema-readiness.md)
- [ADR 0011: Node capability and load visibility](docs/adr/0011-node-capability-load-visibility.md)
- [ADR 0012: Explicit coordinator-owned job lifecycle transitions](docs/adr/0012-job-lifecycle-state-transitions.md)
- [ADR 0013: Agent reconciliation strategy after coordinator restart](docs/adr/0013-agent-reconciliation-strategy.md)
- [ADR 0014: HTTP/JSON v0 API inventory and compatibility policy](docs/adr/0014-http-json-v0-api-inventory-and-compatibility.md)
