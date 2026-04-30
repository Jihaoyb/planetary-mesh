# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network.  
Instead of sending work to a central cloud, clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- **Stage**: Early prototype — control plane working end-to-end on plain HTTP/JSON
- **Code**:
  - Coordinator: node registry with health states, in-memory job store, job dispatch to healthy agents, job detail, and metrics
  - Agent: auto-registration, periodic heartbeat, and a stub `/execute` handler
  - CI: gofmt + build + tests on every push
- **Scope**: LAN-focused prototype with trusted nodes; mTLS, persistence, and dashboard are planned but not yet implemented

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

  cmd/
    coordinator/       # Coordinator service binary (Go, package main)
      main.go          # Thin entrypoint wiring coordinator runtime
    agent/             # Agent daemon binary (Go, package main)
      main.go          # Thin entrypoint wiring agent runtime

  internal/
    coordinator/       # Coordinator HTTP handlers, stores, metrics, tests
    agent/             # Agent HTTP handlers, coordinator client, tests
```

Planned (not yet present): `proto/` for any future gRPC work, durable storage
implementation, `examples/` for smoke demos, and `cmd/pmctl` for the operator
CLI.

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

## Submitting a Job

Once a coordinator and at least one agent are running:

```bash
# Submit a job
curl -X POST http://localhost:8080/jobs \
  -H 'Content-Type: application/json' \
  -d '{"type":"demo","payload":"hello mesh"}'

# List nodes
curl http://localhost:8080/nodes

# List jobs
curl http://localhost:8080/jobs
```

The current agent execution path is still a stub. Real allowlisted command
execution is the next milestone.

## Next Steps

The current roadmap is:

1. **Milestone 2: Real Command Execution**
   - Replace stub execution with allowlisted direct-process command jobs
   - Add bounded stdout/stderr capture and protocol-version enforcement
2. **Milestone 3: Durable Coordinator State**
   - Persist nodes and jobs in Postgres
   - Keep unit tests DB-free and add integration/Compose coverage
3. **Milestone 4: Trusted LAN Security**
   - Add mTLS and node allowlisting
4. **Milestone 5: Operator CLI**
   - Add a thin `pmctl` client for submitting and inspecting jobs

The detailed milestone plan lives in [docs/roadmap.md](docs/roadmap.md).
