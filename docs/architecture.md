# Planetary Mesh – Architecture Overview

This document describes the technical architecture of Planetary Mesh for the initial prototype (v0).  
It focuses on a single-coordinator mesh running in a LAN or other trusted environment.

For technology options and why we lean toward specific stacks and patterns, see:

- `tech-choices.md` – stack/pattern options and rationale.
- `roadmap.md` – current milestone plan from the merged baseline.
- `adr/` – Architecture Decision Records for finalized choices.

---

## 1. Goals and Design Principles

### 1.1 Goals

- Provide a simple way to run compute tasks across multiple devices on a LAN.
- Keep security as a default (mutual TLS and basic node trust).
- Offer a minimal but usable scheduling and retry system.
- Support observability and troubleshooting from day one.
- Keep the design incremental so we can extend it to WAN / global mesh later.

### 1.2 Design Principles

- **Separation of concerns**
  - Coordinator handles control plane (jobs, nodes, scheduling).
  - Agents handle data-plane execution (running tasks).
  - Dashboard/CLI handles human interaction and visualization.
- **Secure by default**
  - All control-plane communication uses TLS with mutual authentication.
  - Node participation is explicitly controlled (allowlist / CA).
- **Simple first, extensible later**
  - v0 uses a single coordinator, basic scheduling, and direct process execution.
  - The architecture leaves room for:
    - More advanced scheduling.
    - Container-based execution.
    - Verifiable compute and incentives.
- **Observable from the start**
  - Logging and metrics are part of the design, not an afterthought.
- **Explicit decisions**
  - Major choices (language, protocol, storage) are recorded in `tech-choices.md` and ADRs.

---

## 2. Relationship to SDLC and Decision Docs

The architecture is developed under a lightweight iterative SDLC (described in `kickoff.md`):

- We do a first-pass design here.
- We then implement in small iterations and refine the architecture as needed.
- Significant changes or non-obvious tradeoffs are captured as:
  - `tech-choices.md` – lists options and current leanings.
  - `docs/adr/*.md` – finalized decisions with context and consequences.

This means:

- This document is conceptual (what the system is and how it behaves).
- Implementation details (exact language, frameworks) can evolve but should stay consistent with the architecture unless an ADR explicitly changes direction.

---

## 3. High-Level System View

Conceptual view:

~~~text
+-----------+         +-----------------+         +-------------------+
|           |  Jobs   |                 |  Tasks  |                   |
|  Client   +-------->+   Coordinator   +-------->+     Agents        |
| (CLI/UI)  |         |                 |  Results|   (Node daemons)  |
+-----------+         +-----------------+<--------+-------------------+
                             ^
                             |
                             v
                        +-----------+
                        | Dashboard |
                        +-----------+
~~~

Mermaid diagram (for renderers that support it):

~~~mermaid
flowchart LR
  Client[Client / CLI] --> Coordinator[Coordinator Service]
  Dashboard[Dashboard UI] --> Coordinator

  Coordinator --> Agent1[Agent Node 1]
  Coordinator --> Agent2[Agent Node 2]
  Coordinator --> Agent3[Agent Node 3]

  Coordinator --> DB[(Coordinator DB)]
~~~

- **Coordinator**
  - Central control plane for v0.
- **Agents**
  - Run on participant devices, executing tasks.
- **CLI**
  - Communicates with coordinator’s API.
  - Dashboard work remains future work.

Today, the merged prototype uses HTTP/JSON for the control plane, as captured in
ADR 0003. Coordinator-agent traffic can run with mTLS and node allowlisting, as
captured in ADR 0007. Longer-term protocol evolution remains open.

---

## 4. Components

### 4.1 Coordinator

The coordinator is the central controller of a mesh.

**Responsibilities**

- **Runtime Configuration**
  - Load listen address, storage, TLS, and node allowlist settings from
    defaults, optional env-style config files, and environment variables.
- **Node Registry**
  - Accept node registration requests.
  - Store node metadata: address, certificate identity, health status, and last heartbeat.
- **Health Management**
  - Process periodic heartbeats from agents.
  - Mark nodes as `HEALTHY`, `SUSPECT`, or `OFFLINE` based on heartbeats and timeouts.
- **Job Management**
  - Expose an API for job submission.
  - Store job metadata and status.
  - Split jobs into tasks where applicable.
- **Scheduling**
  - Current: select the first healthy node for a job dispatch attempt.
  - Future: select agents based on:
    - Measured network latency (RTT).
    - Current load and running tasks.
    - Queue length.
    - Reliability score (success/failure history).
  - Use a score-based approach:

    ~~~text
    score = α * RTT + β * Load + γ * Queue + δ * Reliability
    ~~~

    Exact coefficients and formula are implementation details and may evolve.

- **Dispatch and Tracking**
  - Current: dispatch each job to a healthy agent and track job state
    transitions (`QUEUED` → `RUNNING` → `COMPLETED` / `FAILED`).
  - Current: retry retryable transport and agent `5xx` failures.
  - Future: split jobs into tasks and track task state separately.
- **Result Aggregation**
  - Current: record a single command execution result on the job.
  - Future: aggregate task results into final job results when fanout exists.
  - Provide access to results via API.

**Why a single coordinator for v0?**

- Simpler failure model and easier to reason about.
- Enough to validate scheduling, retries, and security.
- Later phases can introduce:
  - Standby coordinators.
  - Partitioned coordinators for different regions.

### 4.2 Agent

The agent runs on participant devices and executes tasks.

**Responsibilities**

- **Runtime Configuration**
  - Load coordinator URL, node id, listen address, advertised address, TLS
    files, execution timeout, and command allowlist settings from defaults,
    optional env-style config files, and environment variables.
- **Registration**
  - Load or obtain its certificate and key.
  - Connect to coordinator using mTLS.
  - Register capabilities (CPU, RAM, GPU, tags).
- **Heartbeat**
  - Periodically send heartbeat messages with:
    - Current load (running tasks, CPU usage if available).
    - Basic health signals (e.g., errors encountered).
- **Task Execution**
  - Receive tasks assigned by the coordinator.
  - Run them in a sandboxed environment.
    - The current v0 implementation supports allowlisted direct process
      execution with a fixed timeout and bounded stdout/stderr capture.
    - Container-based execution can be layered on later.
- **Progress and Result Reporting**
  - Report task start, progress (if needed), and completion.
  - Return final result or error to coordinator.

**Why separate agent processes instead of library calls in a client app?**

- Agents can be reused for many different clients and workloads.
- Clear separation between client (who submits jobs) and workers (agents).
- Easier to run agents on machines that are not used by the original job submitter.

### 4.3 CLI / Client

`pmctl` is the current operator interface and is a thin layer on top of the
coordinator’s API. Dashboard work remains future work.

**Responsibilities**

- **Node View**
  - List nodes and their states (`HEALTHY`, `SUSPECT`, `OFFLINE`).
  - Show address, last heartbeat, and certificate identity metadata when present.
- **Job View**
  - List jobs and their status (`QUEUED`, `RUNNING`, `COMPLETED`, `FAILED`).
  - Inspect command, node, attempts, captured output, and any error messages.
- **Job Submission**
  - Submit command jobs through the same coordinator validation path as direct
    HTTP clients.
- **Coordinator Status**
  - Show non-secret runtime status/config such as protocol version, storage
    backend, secure mode, node allowlist state, and dispatch settings.
- **Local Configuration**
  - Load coordinator URL and optional operator TLS files from defaults,
    optional env-style config files, environment variables, and CLI flags.

**Why keep clients thin?**

- The core responsibility is operator interaction and simple control.
- Most logic (validation, scheduling, retries) stays in the coordinator.
- This makes it easier to maintain multiple clients (web UI, CLI, automation).

### 4.4 Storage

Coordinator storage holds persistent control-plane state:

- Nodes
- Jobs

Postgres is the durable runtime storage target for the current v0 roadmap, as
documented in `tech-choices.md` and ADR 0006:

- Jobs and nodes benefit from transactions, constraints, and structured queries.
- It keeps the design flexible for future reporting and analytics.

In-memory storage remains useful for fast unit tests and simple local runs.
Task fanout and a separate task table remain future work.

Durable-state operation is verified separately from the default DB-free test
path. The current opt-in checks cover embedded schema initialization, job ID
continuity across store reopen, coordinator startup recovery for persisted
`RUNNING` jobs, and a Compose-backed smoke workflow that proves the coordinator
can restart and continue accepting command jobs with Postgres storage.

Postgres also records lightweight schema readiness metadata in a
`schema_version` table. Version `1` represents the current nodes/jobs-only
schema plus the metadata marker. Coordinator startup backfills missing metadata
for existing databases and rejects databases that record a newer schema version
than the running binary expects. This preserves embedded schema initialization;
it is not a full migration framework.

---

## 5. Data Model (Logical)

The logical data model is independent of any specific DB engine.

### 5.1 ERD (Visual Overview)

~~~mermaid
erDiagram
  NODE ||--o{ JOB : executes

  NODE {
    string id
    string address
    string state
    datetime last_seen
  }

  JOB {
    string id
    string type
    string status
    string command
    int attempts
  }
~~~

### 5.2 Node

Represents an agent participating in the mesh.

Fields (example):

- `id` – unique identifier
- `address` – coordinator-reachable agent address
- `state` – enum: `HEALTHY`, `SUSPECT`, `OFFLINE`
- `last_heartbeat_at` / `last_seen` – timestamp updated on registration and heartbeat
- `certificate` – subject, DNS/IP/URI identities, SHA-256 fingerprint, and expiration when mTLS is enabled
- `created_at` – durable storage creation timestamp

### 5.3 Job

Represents a logical workload submitted by a client.

Fields (example):

- `id` – unique identifier
- `type` – job type; current real workload type is `command`
- `payload` – legacy opaque payload for non-command jobs
- `command`, `args` – allowlisted command key and argument vector for command jobs
- `status` – enum: `QUEUED`, `RUNNING`, `COMPLETED`, `FAILED`
- `attempts`, `node_id`, `started_at`, `completed_at`
- `exit_code`, `stdout`, `stderr`, truncation flags, and `last_error`
- `created_at`, `updated_at`

### 5.4 Task

Separate task records are planned for future fanout work. Milestone 3 persists
nodes and jobs only.

---

## 6. Key Flows

This section describes core runtime flows. Sequence diagrams can be added later.

### 6.1 Node Registration

1. Agent starts with a configured coordinator URL, advertised address, and
   optional CA/certificate/key files.
2. In secure mode, the agent connects to the coordinator over HTTPS with a
   client certificate and validates the coordinator certificate against the
   configured CA.
3. Agent sends an HTTP/JSON `POST /register` request with node id and address.
4. In secure mode, the coordinator verifies the agent certificate and rejects
   registration unless the node id matches an allowed certificate identity or
   SHA-256 fingerprint.
5. Coordinator creates or updates the node record, stores certificate metadata,
   and returns success.
6. Agent continues sending the same registration request as a heartbeat.

### 6.2 Heartbeat and Health Management

1. Agent sends heartbeat messages at a fixed interval (for example, every few seconds).
2. Coordinator:
   - Updates `last_heartbeat_at`.
   - Updates load metrics (running task count, optional CPU usage).
3. A background process periodically:
   - Marks nodes as `SUSPECT` if heartbeat is stale beyond threshold A.
   - Marks nodes as `OFFLINE` if heartbeat is stale beyond threshold B (> A).
4. Node state does not cancel an already in-flight execution attempt in v0.

### 6.3 Job Submission and Scheduling

1. Client sends a `SUBMIT_JOB` request to coordinator with:
   - Job type.
   - For command jobs, a logical command key and optional args.
2. Coordinator validates and stores the job as `QUEUED`.
3. Coordinator chooses a healthy node and dispatches one execution request.
4. Coordinator marks the job `RUNNING` for each dispatch attempt.
5. Agent executes the allowlisted command directly with a fixed timeout and
   bounded stdout/stderr capture.
6. Coordinator records the terminal job result as `COMPLETED` or `FAILED`.

On coordinator startup with Postgres storage, any persisted `RUNNING` jobs are
marked `FAILED` with `coordinator restarted before result was recorded`. This
is intentionally coordinator-owned recovery; agents do not reconcile completed
but unrecorded work in v0.

### 6.4 Failure Handling and Retry

1. If an agent stops sending heartbeats or fails to report results:
   - Coordinator detects stale heartbeat or task timeout.
2. Transport errors, coordinator request timeout, and agent `5xx` responses are
   retried under the dispatch policy.
3. Validation failures, allowlist rejection, protocol mismatch, and non-zero
   command exit are terminal.
4. If retry attempts are exhausted, the job is marked `FAILED`.

---

## 7. Networking and Protocol

### 7.1 Transport and APIs

The long-term architecture may still use a structured RPC framework (for
example, gRPC) for:

- Coordinator ↔ Agent
  - `REGISTER_NODE`
  - `HEARTBEAT`
  - `ASSIGN_TASK`
  - `TASK_RESULT`
- Client / Dashboard ↔ Coordinator
  - `SUBMIT_JOB`
  - `GET_JOB_STATUS`
  - `LIST_JOBS`
  - `LIST_NODES`
  - `GET_COORDINATOR_STATUS`
  - Metrics endpoint (HTTP).

For the current merged baseline and near-term roadmap, coordinator-agent and
client-coordinator communication use HTTP/JSON with explicit versioning. This is
documented in ADR 0003. Coordinator-agent communication can be upgraded to
mutual TLS without changing the JSON wire shape, as documented in ADR 0007.

### 7.2 Discovery

Possible approaches:

- **Static configuration**
  - Agents are configured with the coordinator’s address through environment
    variables or env-style local config files.
  - Simple and predictable for v0.
- **mDNS-based discovery**
  - Coordinator advertises its presence via mDNS.
  - Agents discover coordinator on the LAN.

The current implementation uses static configuration. mDNS discovery can be
added once basic functionality is stable.

---

## 8. Security Model (v0)

Current security model:

- **Identity**
  - Agents can be configured with a unique certificate and private key.
  - Coordinator can be configured with its own certificate and private key.
  - Certificate distribution is manual for v0.
- **Authentication**
  - In secure mode, agents validate the coordinator certificate against a trusted CA.
  - In secure mode, the coordinator validates agent certificates against the same trusted CA.
- **Authorization**
  - Secure coordinator mode requires allowed node identities or fingerprints.
  - Only allowlisted nodes may register and receive work.
  - Future work may add more granular roles and multi-tenant controls.
- **Confidentiality and Integrity**
  - In secure mode, coordinator-agent control-plane communication uses TLS for encryption and integrity.
  - Job payloads can be encrypted or signed as needed (future refinement).
- **Sandboxing**
  - Current: command jobs run as allowlisted direct processes with bounded output
    and a fixed timeout.
  - Container-based isolation is a future extension.

Advanced verifiable compute (redundant execution, proofs, TEEs) is a later phase and not part of v0.

---

## 9. Observability

### 9.1 Logging

- Coordinator logs:
  - Node register/unregister.
  - Heartbeat state changes (healthy → suspect → offline).
  - Job submission and completion.
  - Dispatch attempts, retries, and failures.
- Agent logs:
  - Command execution start, completion, and failure.
  - Local sandbox errors and resource issues.
  - Connectivity problems.

Logs should be structured enough (for example, JSON) to be consumed by log processors if needed.

### 9.2 Metrics

Coordinator and optionally agents expose metrics such as:

- Number of nodes by state.
- Number of jobs per status (queued, running, completed, failed).
- Number of persisted running jobs recovered during coordinator startup.
- Postgres schema readiness and recorded/expected schema versions when Postgres
  storage is enabled.
- Number of dispatch attempts and errors.
- Average job latency.
- Scheduler decisions (for example, jobs per node).

These can be exposed via an HTTP endpoint for tools like Prometheus.

Why metrics from v0:

- Scheduling and retry logic are sensitive to configuration and environment.
- Metrics provide feedback to tune thresholds and coefficients.
- They help validate that the system is doing what we expect under load and failure.

---

## 10. Future Evolution (Beyond v0)

The v0 architecture is deliberately simple but should support:

- Multiple coordinators
  - Sharding by region or job type.
  - Coordinator failover.
- WAN / Cross-site mesh
  - Registry service for gluing meshes together.
  - Latency-aware routing between regions.
- Verifiable and incentivized compute
  - Redundant task execution and comparison.
  - Integration with TEEs or cryptographic proofs.
  - Credit or token systems tied to real work done.
- Multi-tenant features
  - Strong isolation between tenants.
  - Authorization and quota per org or user.

Those directions are not implemented in v0, but the current architecture should not block them.

---
