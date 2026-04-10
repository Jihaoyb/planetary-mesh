# Planetary Mesh

## Overview

Planetary Mesh is a decentralized compute network that pools idle CPU/GPU, storage, and bandwidth across devices on a local or trusted network.  
Instead of sending work to a central cloud, clients submit jobs to a coordinator, which schedules tasks across participating agent nodes.

---

## Status

- **Stage**: Early prototype — control plane working end-to-end on plain HTTP/JSON
- **Code**:
  - Coordinator: node registry with health states, in-memory job store, job dispatch to healthy agents
  - Agent: auto-registration, periodic heartbeat, `/execute` handler (stub workload)
  - CI: gofmt + build + tests on every push
- **Scope**: LAN-focused prototype with trusted nodes; mTLS, persistence, and dashboard are planned but not yet implemented

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
    adr/
      0000-template.md
      0001-process-and-docs.md
      0002-language-choice.md

  cmd/
    coordinator/       # Coordinator service binary (Go, package main)
      main.go
      server.go        # HTTP handlers + dispatch logic
      nodes.go         # NodeRegistry + health checker
      jobs.go          # JobStore (in-memory)
      *_test.go
    agent/             # Agent daemon binary (Go, package main)
      main.go
      coord_client.go  # Register + heartbeat client
      executor.go      # /execute handler (stub workload)
      *_test.go
```

Planned (not yet present): `internal/` packages for shared logic, `proto/` for gRPC definitions, `cmd/dashboard/` or `cmd/cli/` for the operator surface.

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
  -d '{"type":"echo","payload":"hello mesh"}'

# List nodes
curl http://localhost:8080/nodes

# List jobs
curl http://localhost:8080/jobs
```

The coordinator dispatches the job to the first healthy agent, which simulates work and returns success.

## Next Steps

The current focus is hardening the control plane before expanding scope:

- Refactor shared logic from `cmd/` into `internal/` packages.
- Add `GET /jobs/{id}`, structured logging, graceful shutdown, configurable
  dispatch timeout/retry/backoff, and a `/metrics` endpoint.
- Add end-to-end and failure-path tests.
- Then layer in TLS/mTLS, node identity/allowlisting, and durable persistence.
- Finally add a thin operator CLI or dashboard that consumes the coordinator API.
