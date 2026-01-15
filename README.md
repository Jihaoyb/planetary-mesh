# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network.  
Instead of sending work to a central cloud, clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- **Stage**: Design / Early prototype
- **Code**: Initial Go services scaffolded (coordinator / agent health checks)
- **Scope**: LAN-focused prototype with trusted nodes and secure communication

For more details, see:

- [Kickoff Plan](docs/kickoff.md)
- [Architecture](docs/architecture.md)
- [Tech Choices](docs/tech-choices.md)

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

Current (early) structure:

```text
planetary-mesh/
  README.md

  docs/
    kickoff.md
    architecture.md
    tech-choices.md
    adr/
      0000-template.md
      0001-process-and-docs.md
      0002-language-choice.md

  cmd/
    coordinator/       # Coordinator service binary (Go, package main)
    agent/             # Agent daemon binary (Go, package main)

  internal/
    coordinator/       # Coordinator-specific logic (to be added)
    agent/             # Agent-specific logic (to be added)
    config/            # Config loading helpers (to be added)
    logging/           # Logging helpers (to be added)

  proto/               # Protocol / gRPC definitions (future)
```

This may evolve as we add the dashboard and more shared libraries.

---

## Quickstart (Development)

### Requirements

- Go 1.21+ (check with `go version`)

### Run the coordinator

From the repo root:

```bash
go run ./cmd/coordinator
```

By default it listens on `:8080`. You can change the address with:

```bash
COORDINATOR_ADDR=":9090" go run ./cmd/coordinator
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

Agent health check:

```bash
curl http://localhost:8081/healthz
# → ok
```

---

## Next Steps

- Implement coordinator ↔ agent registration and heartbeats.
- Add an in-memory node registry on the coordinator.
- Expose a simple `/nodes` API to inspect registered agents.
- Start defining the job/task model and submission API.
# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network. Clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- Stage: Design / early prototype
- Code: Go coordinator and agent with health checks, job submission, dispatch, and agent execution
- Scope: LAN-focused prototype with trusted nodes and plain HTTP (TLS/mTLS planned)

Docs: [Kickoff](docs/kickoff.md) | [Architecture](docs/architecture.md) | [Tech Choices](docs/tech-choices.md) | [ADRs](docs/adr)

---

## Goals for v0 (Prototype)

- Secure node registration and mutual TLS between components.
- Basic job submission API.
- Coordinator-based scheduling and task dispatch.
- Agent execution in a sandboxed environment.
- Heartbeats, timeouts, and automatic reassignment on failure.
- Dashboard with node list, job list, and basic metrics.

---

## Project Structure

```text
planetary-mesh/
  README.md

  docs/
    kickoff.md
    architecture.md
    tech-choices.md
    adr/
      0000-template.md
      0001-process-and-docs.md
      0002-language-choice.md
      0003-job-api-v0.md
      0004-job-execution-v1.md

  cmd/
    coordinator/       # Coordinator service binary (Go, package main)
    agent/             # Agent daemon binary (Go, package main)

  internal/            # Reserved for shared/internal packages (future)
  proto/               # Protocol / gRPC definitions (future)
```

---

## Quickstart (Development)

Requirements: Go 1.21+

### Coordinator

```bash
go run ./cmd/coordinator
```

- Default listen: `:8080` (override `COORDINATOR_ADDR`).
- Health: `curl http://localhost:8080/healthz` -> `ok`

Config (env):
- `COORDINATOR_ADDR` (default `:8080`)
- `DISPATCH_TIMEOUT` (default `5s`)
- `DISPATCH_BACKOFF` (default `200ms`)
- `DISPATCH_MAX_ATTEMPTS` (default `2`)

Metrics:
```bash
curl http://localhost:8080/metrics
```

### Agent

```bash
go run ./cmd/agent
```

- Default listen: `:8081` (override `AGENT_ADDR`).
- Health: `curl http://localhost:8081/healthz` -> `ok`

Config (env):
- `AGENT_ADDR` (default `:8081`)
- `COORDINATOR_URL` (default `http://localhost:8080`)
- `NODE_ID` (default hostname)
- `HEARTBEAT_INTERVAL` (default `10s`)
- `COORD_REQUEST_TIMEOUT` (default `5s`)

---

## Current Prototype Capabilities

- Endpoints: `/healthz`, `/register`, `/nodes`, `/jobs`, `/jobs/{id}`; coordinator dispatches to agent `/execute`.
- Health & lifecycle: background health checker for nodes; graceful shutdown for coordinator and agent.
- Dispatch: first-healthy scheduling with configurable timeout/backoff/retries and per-request timeouts.
- Agent: registration + heartbeat loop with configurable interval and request timeout.

---

## Next Steps

- Richer job/task model and task-level scheduling.
- Observability (metrics) and better retry/backoff policies.
- TLS/mTLS and move toward gRPC control plane.
- Persist nodes/jobs instead of in-memory stores.
