# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network.  
Instead of sending work to a central cloud, clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- **Stage**: Early prototype — control plane, allowlisted command execution, optional Postgres persistence, opt-in coordinator-agent mTLS, and operator CLI working end-to-end
- **Code**:
  - Coordinator: node registry with health states and certificate metadata, job dispatch to healthy agents, job detail, metrics, optional Postgres persistence, and mTLS node admission
  - Agent: auto-registration, periodic heartbeat, allowlisted direct command execution, and optional mTLS
  - CLI: `pmctl` for coordinator status, node/job listing, job inspection, and command job submission
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
- Dashboard with node list, job list, and basic metrics.

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
  - LAN discovery (mDNS) and/or static coordinator address.
  - All communication over TLS with mutual authentication.
  - gRPC or similar RPC-style protocol for control messages.

- **Dashboard / CLI**
  - Shows nodes, jobs, and metrics.
  - Provides a simple interface to submit and inspect jobs.

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
go run ./cmd/pmctl status
go run ./cmd/pmctl nodes list
go run ./cmd/pmctl jobs list
go run ./cmd/pmctl submit command echo hello mesh
go run ./cmd/pmctl jobs inspect job-1
```

Use `--json` for automation-friendly output:

```bash
go run ./cmd/pmctl --json jobs inspect job-1
```

Point at another coordinator with a flag or environment variable:

```bash
go run ./cmd/pmctl --coordinator-url http://localhost:9090 status

PMCTL_COORDINATOR_URL=http://localhost:9090 go run ./cmd/pmctl nodes list
```

For a secure coordinator, provide the CA and operator client certificate:

```bash
go run ./cmd/pmctl \
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

To run a local coordinator + Postgres + agent stack:

```bash
docker compose up
```

In another terminal, submit a command job:

```bash
curl -X POST http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1' \
  -H 'Content-Type: application/json' \
  -d '{"type":"command","command":"echo","args":["hello from compose"]}'
```

Use the returned `id` to inspect the result:

```bash
curl http://localhost:8080/jobs/job-1 \
  -H 'X-Planetary-Protocol-Version: 1'
```

Expected output includes `"status": "COMPLETED"` and captured `stdout`.

## Next Steps

The v0 milestone plan lives in [docs/roadmap.md](docs/roadmap.md).
