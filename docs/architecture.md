# Planetary Mesh – Architecture Overview

This document describes the technical architecture of Planetary Mesh for the initial prototype (v0).  
It focuses on a single-coordinator mesh running in a LAN or other trusted environment.

For technology options and why we lean toward specific stacks and patterns, see:

- `tech-choices.md` – stack/pattern options and rationale.
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
- **Dashboard / CLI**
  - Communicates with coordinator’s API.
- All control traffic uses TLS with mutual authentication.

Network and protocol details (REST vs gRPC, etc.) are in `tech-choices.md` and ADRs.

---

## 4. Components

### 4.1 Coordinator

The coordinator is the central controller of a mesh.

**Responsibilities**

- **Node Registry**
  - Accept node registration requests.
  - Store node metadata: capabilities, certificate identity, health status, last heartbeat.
- **Health Management**
  - Process periodic heartbeats from agents.
  - Mark nodes as `HEALTHY`, `SUSPECT`, or `OFFLINE` based on heartbeats and timeouts.
- **Job Management**
  - Expose an API for job submission.
  - Store job metadata and status.
  - Split jobs into tasks where applicable.
- **Scheduling**
  - Select agents for tasks based on:
    - Measured network latency (RTT).
    - Current load and running tasks.
    - Queue length.
    - Reliability score (success/failure history).
  - Use a score-based approach:

    ~~~text
    score = α * RTT + β * Load + γ * Queue + δ * Reliability
    ~~~

    Exact coefficients and formula are implementation details and may evolve.

- **Task Dispatch and Tracking**
  - Assign tasks to agents.
  - Track task state transitions (QUEUED → ASSIGNED → RUNNING → COMPLETED / FAILED).
  - Handle retries and reassignment when agents fail or time out.
- **Result Aggregation**
  - Collect task results.
  - Aggregate them into final job results when needed.
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
    - For v0, we lean toward direct process execution with resource limits.
    - Container-based execution can be layered on later.
- **Progress and Result Reporting**
  - Report task start, progress (if needed), and completion.
  - Return final result or error to coordinator.

**Why separate agent processes instead of library calls in a client app?**

- Agents can be reused for many different clients and workloads.
- Clear separation between client (who submits jobs) and workers (agents).
- Easier to run agents on machines that are not used by the original job submitter.

### 4.3 Dashboard / Client

The Dashboard / CLI is a thin layer on top of the coordinator’s API.

**Responsibilities**

- **Node View**
  - List nodes and their states (`HEALTHY`, `SUSPECT`, `OFFLINE`).
  - Show capabilities and basic metrics (jobs handled, last heartbeat).
- **Job View**
  - List jobs and their status (`QUEUED`, `RUNNING`, `COMPLETED`, `FAILED`).
  - Show tasks per node and any error messages.
- **Job Submission**
  - Allow users to submit jobs with simple forms or commands.
- **Metrics**
  - Display key metrics from coordinator (throughput, failure counts, latency).

**Why keep the dashboard thin?**

- The core responsibility is visualization and simple control.
- Most logic (validation, scheduling, retries) stays in the coordinator.
- This makes it easier to maintain multiple clients (web UI, CLI, automation).

### 4.4 Storage

Coordinator storage holds persistent control-plane state:

- Nodes
- Jobs
- Tasks

We lean toward a relational database (e.g., Postgres), as documented in `tech-choices.md`:

- Jobs and tasks are naturally relational.
- We benefit from transactions, constraints, and structured queries.
- It keeps the design flexible for future reporting and analytics.

Early iterations may start with in-memory storage for speed of development but should converge to a durable store for realistic scenarios.

---

## 5. Data Model (Logical)

The logical data model is independent of any specific DB engine.

### 5.1 ERD (Visual Overview)

~~~mermaid
erDiagram
  NODE ||--o{ TASK : handles
  JOB  ||--o{ TASK : contains

  NODE {
    string id
    string name
    string state
    string cert_fingerprint
  }

  JOB {
    string id
    string type
    string status
    string payload_ref
  }

  TASK {
    string id
    string status
    int    attempts
  }
~~~

### 5.2 Node

Represents an agent participating in the mesh.

Fields (example):

- `id` – unique identifier
- `name` – human-readable name
- `cert_fingerprint` – unique identifier for the node certificate
- `capabilities` – CPU cores, memory, GPU presence, tags
- `state` – enum: `HEALTHY`, `SUSPECT`, `OFFLINE`
- `reliability_score` – numeric score based on past successes/failures
- `last_heartbeat_at` – timestamp
- `created_at`, `updated_at`

### 5.3 Job

Represents a logical workload submitted by a client.

Fields (example):

- `id` – unique identifier
- `type` – job type (e.g., `script`, `image_batch`, `embedding`)
- `payload_ref` – reference to input data (file path, object store key, etc.)
- `status` – enum: `QUEUED`, `RUNNING`, `COMPLETED`, `FAILED`
- `submitter` – optional submitter id
- `created_at`, `started_at`, `completed_at`

### 5.4 Task

Represents a unit of work assigned to a single node as part of a job.

Fields (example):

- `id` – unique identifier
- `job_id` – foreign key to `Job`
- `node_id` – foreign key to `Node`
- `status` – enum: `QUEUED`, `ASSIGNED`, `RUNNING`, `COMPLETED`, `FAILED`, `RETRYING`
- `payload_subset` – details of what this task should process (e.g., index range)
- `attempts` – number of attempts so far
- `started_at`, `finished_at`
- `last_error` – optional error message

---

## 6. Key Flows

This section describes core runtime flows. Sequence diagrams can be added later.

### 6.1 Node Registration

1. Agent starts and loads its certificate and key.
2. Agent connects to coordinator using mTLS.
3. Agent sends a `REGISTER_NODE` request with:
   - Node id (or request for assignment).
   - Capabilities.
   - Optional metadata (tags, operator).
4. Coordinator verifies:
   - Certificate is from trusted CA.
   - Node is allowed to join (allowlist).
5. Coordinator creates or updates node record and returns success.
6. Agent enters `REGISTERED` state and starts sending heartbeats.

### 6.2 Heartbeat and Health Management

1. Agent sends heartbeat messages at a fixed interval (for example, every few seconds).
2. Coordinator:
   - Updates `last_heartbeat_at`.
   - Updates load metrics (running task count, optional CPU usage).
3. A background process periodically:
   - Marks nodes as `SUSPECT` if heartbeat is stale beyond threshold A.
   - Marks nodes as `OFFLINE` if heartbeat is stale beyond threshold B (> A).
4. Tasks on `OFFLINE` nodes become candidates for reassignment.

### 6.3 Job Submission and Scheduling

1. Client sends a `SUBMIT_JOB` request to coordinator with:
   - Job type.
   - Payload reference or inline payload.
   - Optional parameters (priority, etc. for future).
2. Coordinator validates and stores the job as `QUEUED`.
3. Coordinator:
   - Optionally splits the job into multiple tasks.
   - For each task, evaluates candidate nodes:
     - Uses the scheduling score: `score = α * RTT + β * Load + γ * Queue + δ * Reliability`.
4. Coordinator assigns each task to a selected node and sets status to `ASSIGNED`.
5. Agent receives task, acknowledges it, and sets local task to `RUNNING`.
6. Agent:
   - Executes task in sandbox.
   - Reports completion or error.
7. Coordinator updates:
   - Task status.
   - Job status based on all tasks (for example, full success vs partial failure).

### 6.4 Failure Handling and Retry

1. If an agent stops sending heartbeats or fails to report results:
   - Coordinator detects stale heartbeat or task timeout.
2. Coordinator moves associated tasks to `RETRYING` (if attempts remain).
3. Tasks are reassigned to other suitable nodes and attempts counter is incremented.
4. If max attempts are reached:
   - Task is marked `FAILED`.
   - Job is marked `FAILED` or `PARTIALLY_COMPLETED` depending on policy (v0 can keep it simple and use `FAILED`).

---

## 7. Networking and Protocol

### 7.1 Transport and APIs

We expect to use a structured RPC framework (for example, gRPC) for:

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
  - Metrics endpoint (HTTP).

REST endpoints may exist for convenience (especially for the dashboard), but internal coordinator–agent communication should use a typed binary protocol (gRPC or equivalent), as discussed in `tech-choices.md`.

### 7.2 Discovery

Possible approaches:

- **Static configuration**
  - Agents are configured with the coordinator’s address.
  - Simple and predictable for v0.
- **mDNS-based discovery**
  - Coordinator advertises its presence via mDNS.
  - Agents discover coordinator on the LAN.

We can start with static configuration (simpler to implement and debug) and add mDNS discovery once basic functionality is stable. The chosen approach should be documented in an ADR.

---

## 8. Security Model (v0)

High-level security model:

- **Identity**
  - Each agent has a unique certificate and private key.
  - Coordinator has its own certificate.
  - A simple local CA or manual process issues certificates.
- **Authentication**
  - Agents validate coordinator certificate against trusted CA.
  - Coordinator validates agent certificates and checks allowlist (by cert fingerprint or node id).
- **Authorization**
  - Only nodes with valid certs and not on denylist may register and receive tasks.
  - Future work may add more granular roles and multi-tenant controls.
- **Confidentiality and Integrity**
  - All control-plane communication uses TLS for encryption and integrity.
  - Job payloads can be encrypted or signed as needed (future refinement).
- **Sandboxing**
  - Tasks run with limited privileges (direct process with constraints in v0).
  - Container-based isolation is a future extension.

Advanced verifiable compute (redundant execution, proofs, TEEs) is a later phase and not part of v0.

---

## 9. Observability

### 9.1 Logging

- Coordinator logs:
  - Node register/unregister.
  - Heartbeat state changes (healthy → suspect → offline).
  - Job submission and completion.
  - Task assignments, retries, and failures.
- Agent logs:
  - Task acceptance, progress, completion.
  - Local sandbox errors and resource issues.
  - Connectivity problems.

Logs should be structured enough (for example, JSON) to be consumed by log processors if needed.

### 9.2 Metrics

Coordinator and optionally agents expose metrics such as:

- Number of nodes by state.
- Number of jobs per status (queued, running, completed, failed).
- Number of tasks and retries.
- Average job latency.
- Scheduler decisions (for example, tasks per node).

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

## 11. Current Prototype Implementation Status (v0.1)

The current Go implementation in this repository is an early prototype and implements only a subset of the architecture described above:

- **Coordinator**
  - Maintains an in-memory node registry with health states:
    - Agents register via `POST /register`.
    - A background ticker updates node state based on last heartbeat:
      - `HEALTHY` → `SUSPECT` → `OFFLINE`.
  - Maintains an in-memory job store:
    - `POST /jobs` creates a job with a simple `type` and `payload`.
    - `GET /jobs` lists all known jobs.
  - Uses a very simple scheduler:
    - Picks the first `HEALTHY` node to run a job.
    - Dispatches the job to the chosen agent's `/execute` endpoint.
    - Updates job status (`QUEUED` → `RUNNING` → `COMPLETED` or `FAILED`).

- **Agent**
  - Registers with the coordinator and sends periodic heartbeats using plain HTTP.
  - Exposes:
    - `GET /healthz` for liveness.
    - `POST /execute` for job execution.
  - Job execution is simulated in v0.1 (log + sleep + 200 OK response).

- **Technology choices (prototype)**
  - Communication is plain HTTP with JSON (no TLS yet).
  - All state (nodes and jobs) is kept in memory inside the coordinator process.
  - There is no database, retries, or multi-task jobs yet.

The more advanced topics in this document (mTLS, gRPC, persistent storage, multi-task jobs, score-based scheduling, retries) represent **future iterations**. As those features are implemented, new ADRs will be added and this section will be updated to reflect the current state.
