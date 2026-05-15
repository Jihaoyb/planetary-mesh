# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network.  
Instead of sending work to a central cloud, clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- **Stage**: Early prototype — control plane, allowlisted command execution, optional Postgres persistence with schema readiness metadata, opt-in coordinator-agent mTLS, operator CLI, local config files, repeatable local smoke workflow, and opt-in durable Postgres verification working end-to-end
- **Code**:
  - Coordinator: node registry with health states and certificate metadata, job dispatch to healthy agents, job detail, metrics, optional Postgres persistence, and mTLS node admission
  - Agent: auto-registration, periodic heartbeat, allowlisted direct command execution, and optional mTLS
  - CLI: `pmctl` for coordinator status, node/job listing, job inspection, and command job submission
  - Config: optional env-style local config files for coordinator, agent, and `pmctl`
  - Smoke: config-driven local demo for one coordinator, two agents, and `pmctl`
  - Ops: opt-in Postgres smoke workflow for durable-state, schema readiness, and restart-recovery verification
  - CI: gofmt + build + tests on every push
- **Scope**: LAN-focused prototype with trusted nodes; dashboard work remains future work

For more details, see:

- [Kickoff Plan](docs/kickoff.md)
- [Architecture](docs/architecture.md)
- [Tech Choices](docs/tech-choices.md)
- [Roadmap](docs/roadmap.md)

---

## Goals for v0 (Prototype)

The initial prototype targets a **3–5 node LAN mesh** with:

- Secure node registration and mutual TLS between components.
- Basic job submission API (e.g., simple batch tasks).
- Coordinator-based scheduling and task dispatch.
- Agent execution in a sandboxed environment.
- Heartbeats, timeouts, and automatic reassignment on failure.
- Operator CLI with node list, job list, job inspection, submission, and status.

---

## High-Level Architecture

Core components:

- **Coordinator**
  - Maintains node registry and health.
  - Accepts jobs from clients.
  - Schedules and dispatches tasks to agents.
  - Aggregates results and updates job status.

- **Agent**
  - Runs on each participant device.
  - Registers with the coordinator and advertises capabilities.
  - Executes tasks in a sandbox (e.g., container or restricted process).
  - Sends heartbeats and progress updates.

- **Network Layer**
  - Static coordinator address configuration for v0.
  - HTTP/JSON control messages with explicit protocol versioning.
  - Optional TLS with mutual authentication for trusted LAN coordinator-agent traffic.

- **CLI**
  - Shows nodes, jobs, and coordinator status.
  - Provides a simple interface to submit and inspect command jobs.
  - Dashboard work remains future work.

The detailed design is in [docs/architecture.md](docs/architecture.md).

---

## Project Structure

Current structure:

```text
planetary-mesh/
  README.md
  go.mod

  .github/workflows/
    ci.yml             # gofmt + build + test on every push/PR

  docs/
    kickoff.md
    architecture.md
    tech-choices.md
    roadmap.md
    adr/
      0000-template.md
      0001-process-and-docs.md
      0002-language-choice.md
      0003-http-json-control-plane-for-v0.md
      0004-in-memory-storage-for-v0.md
      0005-command-execution-v0.md
      0006-postgres-coordinator-persistence.md
      0007-mtls-trusted-lan-security.md
      0008-operator-cli.md
      0009-env-style-local-config-files.md

  config/
    coordinator.env.example
    agent-1.env.example
    agent-2.env.example
    pmctl.env.example

  compose.yaml         # local coordinator + Postgres + agent demo

  cmd/
    coordinator/       # Coordinator service binary (Go, package main)
      main.go          # Thin entrypoint wiring coordinator runtime
    agent/             # Agent daemon binary (Go, package main)
      main.go          # Thin entrypoint wiring agent runtime
    pmctl/             # Operator CLI binary (Go, package main)

  internal/
    coordinator/       # Coordinator HTTP handlers, stores, metrics, tests
    agent/             # Agent HTTP handlers, coordinator client, tests
    pmctl/             # CLI command parsing, output, and coordinator client
    protocol/          # Shared protocol constants and wire structs
    security/          # Shared TLS, certificate identity, and allowlist helpers
```

Planned (not yet present): `proto/` for any future gRPC work.

---

## Quickstart (Development)

### Requirements

- Go 1.25+ (check with `go version`)

### Configuration precedence

Coordinator, agent, and `pmctl` still work with environment variables. They can
also load env-style config files that use the same keys as the environment.

Config precedence is:

1. compiled defaults
2. config file values
3. non-empty environment variables
4. CLI flags, where supported

Each binary accepts `--config <path>`. You can also set a config path with
`COORDINATOR_CONFIG_FILE`, `AGENT_CONFIG_FILE`, or `PMCTL_CONFIG_FILE`.

If present, these default local files are auto-loaded:

- `config/coordinator.env`
- `config/agent.env`
- `config/pmctl.env`

Local `config/*.env` files are ignored by git. The tracked `*.env.example` files
are safe starting points.

### Run the local smoke demo

From a fresh checkout with Go, `curl`, and `python3` available:

```bash
./examples/demo.sh
```

The demo builds temporary local binaries, starts one coordinator and two agents
from tracked config examples, submits an allowlisted command through `pmctl`,
lists nodes/jobs through `pmctl`, and inspects the completed job. It uses
in-memory coordinator storage and plain HTTP by default.

Logs are written under `/tmp` or `$TMPDIR` and printed at the end of the run.

### Run the Postgres durability smoke demo

For an opt-in durable-state check, run the Postgres-backed smoke workflow with
Docker Compose available:

```bash
./examples/postgres_smoke.sh
```

This starts the Compose stack on isolated high host ports by default, verifies
`pmctl status` reports Postgres storage, verifies `pmctl --json status` reports
schema readiness version `1`, submits a completed command job, restarts the
coordinator while a job is `RUNNING`, verifies startup recovery marks that job
`FAILED`, checks `/metrics`, and submits another command after restart. It
cleans up its Compose project and volume unless
`KEEP_POSTGRES_SMOKE=1` is set.

To run the opt-in Postgres integration tests against an existing database:

```bash
POSTGRES_TEST_DSN='postgres://planetary:planetary@localhost:5432/planetary_mesh?sslmode=disable' \
go test -tags postgres ./internal/coordinator
```

The default `go test ./...` remains free of external services.

### Run the coordinator

From the repo root:

```bash
go run ./cmd/coordinator
```

By default it listens on `:8080`. You can change the address with:

```bash
COORDINATOR_ADDR=":9090" go run ./cmd/coordinator
```

By default the coordinator uses in-memory storage. To persist nodes and jobs in
Postgres, set `COORDINATOR_DATABASE_URL`:

```bash
COORDINATOR_DATABASE_URL='postgres://planetary:planetary@localhost:5432/planetary_mesh?sslmode=disable' \
go run ./cmd/coordinator
```

Postgres startup still uses the embedded schema initialization model. The
coordinator records a lightweight `schema_version` marker and exposes it through
`/status`, `/metrics`, and `pmctl --json status`:

```bash
pmctl --json status
```

For Postgres storage, the `schema` object should report `ready: true`,
`version: 1`, and `expected_version: 1`. A database marked with a newer schema
version is rejected at startup so an older coordinator does not run against a
newer schema. This is readiness metadata, not a full migration framework.

You can also run from a config file:

```bash
go run ./cmd/coordinator --config config/coordinator.env.example
```

Health check:

```bash
curl http://localhost:8080/healthz
# → ok
```

### Run the agent

In another terminal:

```bash
go run ./cmd/agent
```

By default it listens on `:8081`. You can change the address with:

```bash
AGENT_ADDR=":9091" go run ./cmd/agent
```

For command jobs, agents execute only allowlisted logical command names:

```bash
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep' go run ./cmd/agent
```

If the address agents listen on is different from the address the coordinator
should call, set `AGENT_ADVERTISE_ADDR`.

You can also run from config files. For a local two-agent setup, open separate
terminals:

```bash
go run ./cmd/agent --config config/agent-1.env.example
go run ./cmd/agent --config config/agent-2.env.example
```

Agent health check:

```bash
curl http://localhost:8081/healthz
# → ok
```

### Secure coordinator-agent mode

Plain HTTP remains the default for local development. To enable mTLS between the
coordinator and agents, manually provision a CA plus coordinator and agent
certificates, then start both processes with TLS file paths.

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
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep' \
go run ./cmd/agent
```

Allowed node identities use `node-id=identity` entries, where identity can be
`dns:name`, `ip:addr`, `uri:value`, `cn:name`, or `subject:value`. Fingerprint
allowlists use SHA-256 certificate fingerprints:

```bash
COORDINATOR_ALLOWED_NODE_FINGERPRINTS='agent-1=<sha256-hex-fingerprint>'
```

Secure `curl` requests to the coordinator need the CA and a client certificate:

```bash
curl --cacert ./certs/ca.pem \
  --cert ./certs/operator.pem \
  --key ./certs/operator-key.pem \
  https://localhost:8080/nodes \
  -H 'X-Planetary-Protocol-Version: 1'
```

Certificate generation, distribution, enrollment, and rotation remain manual in
v0.

---

## Operator CLI

`pmctl` is a thin client over the coordinator HTTP/JSON API. It defaults to the
local plain-HTTP coordinator:

```bash
go install ./cmd/pmctl

pmctl status
pmctl nodes list
pmctl jobs list
pmctl submit command echo hello mesh
pmctl jobs inspect job-1
```

Make sure `$(go env GOPATH)/bin` or your configured `GOBIN` is on `PATH`.
`go run ./cmd/pmctl ...` still works for development.

Use `--json` for automation-friendly output:

```bash
pmctl --json jobs inspect job-1
```

Point at another coordinator with a flag, environment variable, or config file:

```bash
pmctl --coordinator-url http://localhost:9090 status

PMCTL_COORDINATOR_URL=http://localhost:9090 pmctl nodes list

pmctl --config config/pmctl.env.example status
```

For a secure coordinator, provide the CA and operator client certificate:

```bash
pmctl \
  --coordinator-url https://localhost:8080 \
  --ca-file ./certs/ca.pem \
  --cert-file ./certs/operator.pem \
  --key-file ./certs/operator-key.pem \
  nodes list
```

Equivalent environment variables are `PMCTL_TLS_CA_FILE`,
`PMCTL_TLS_CERT_FILE`, and `PMCTL_TLS_KEY_FILE`.

---

## Submitting a Job

Once a coordinator and at least one agent are running:

```bash
# Submit a job
curl -X POST http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1' \
  -H 'Content-Type: application/json' \
  -d '{"type":"command","command":"echo","args":["hello mesh"]}'

# List nodes
curl http://localhost:8080/nodes \
  -H 'X-Planetary-Protocol-Version: 1'

# List jobs
curl http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1'
```

Command jobs reject `payload`; use `command` and optional `args`.

## Compose Demo

To run a local coordinator + Postgres + two-agent stack:

```bash
docker compose up
```

In another terminal, inspect the mesh with `pmctl`:

```bash
go run ./cmd/pmctl nodes list
```

Submit a command job and inspect the returned job id:

```bash
go run ./cmd/pmctl submit command echo "hello from compose"
go run ./cmd/pmctl jobs inspect job-1
```

Expected output includes `Status COMPLETED` and captured `Stdout`.

The default Compose host ports are `5432`, `8080`, `8081`, and `8082`. Override
them when another local stack is already running:

```bash
POSTGRES_HOST_PORT=15432 \
COORDINATOR_HOST_PORT=18080 \
AGENT1_HOST_PORT=18081 \
AGENT2_HOST_PORT=18082 \
docker compose up
```

## Next Steps

The v0 milestone plan lives in [docs/roadmap.md](docs/roadmap.md).
