# Planetary Mesh - Kickoff Plan

This document describes the initial plan for Planetary Meash using standard software development lifecycle (SDLC) practices.
It covers objectives, scope, requirements, architecture direction, and the first stages of delivery.

---

## 1. Project Summary

### 1.1 Problem

- Cloud compute is centralized, expensive, and not always close to where workloads run.
- Many devices (laptops, desktops, lab machines, edge devices) are idle for most of the day.
- There is no simple, trusted way to pool this idle capacity into a usable compute mesh.

### 1.2 Solution

Planetary Mesh is a decentralized compute layer for trusted networks.
It allows devices to:

- Register as compute nodes (agents).
- Accept jobs from a coordinator.
- Execute tasks securely and report back results.

The first version targets **LAN or tightly controlled environments**, not the open internet.

---

## 2. SDLC Approach

### 2.1 What "Lightweight SDLC" Means Here

"LightWeight SDLC" in this project means:

- We **do follow a clear lifecycle** (requirements → design → implementation→ testing → demo).
- We **avoid heavy, rigid process** (hundreds of pages of specs, large phase gates).
- We keep:
  - A small set of **core docs** (kickoff, architecture, tech choices, ADRs).
  - Short iterations (1-2 weeks) with **working software** at each step.
  - Continuous feedback and adjustment.

In other words: the process is **structured enough** to stay aligned and make good decisions, but flexible enough for a new, exploratory system.

## 2.2 SDLC Model We Are Using

We are using an **iterative, incremental, Agile-inspired SDLC**:

1. **Inception / Kickoff**
  - Define goals, scope, constraints, and risks.
  - Agree on SDLC model and core tech direction.
2. **Design**
  - Write and refine architecture and tech choices.
  - Capture key decision as ADRs (Architecture Decision Records).
3. **Implementation in Iterations**
  - Build in small slices (coordinator skeleton →  registration → scheduling).
  - Each iteration ends with something runnable and demoable.
4. **Testing and Validation**
  - Unit tests, integration tests, and fault-injection for failure cases.
5. **Demo / Review**
  - Run mesh demos, collect feedback, update roadmap.

We do **not** try to lock every detail up front. We expect to refine the design as we learn from real runs.

### 2.3 Why This Model vs Other SDLCs

**Why not classic Waterfall?**

- Waterfall assumes requirements are stable and well-known up front.
- Planetary Mesh involves:
  - New scheduling logic.
  - Security and networking details that will need tuning.
  - Evolving use cases (e.g., which workloads are most important).
- A strict "design everyting then implement" flow would:
  - Delay feedback from real runs.
  - Make early design mistakes harder to correct.

**Why not heavy Spiral / RUP?**

- Spiral and RUP emphasize detailed risk management, phase gates, and extensive documentation.
- For this project:
  - The team is small
  - We need to move fast at the prototype stage.
- A heavy process would slow iteration without providing enough extra value.

**Why not  pure ad-hoc hacking (no SDLC)?**

- A pure "just code and see" approach:
  - Often leads to unclear scope and inconsistent architecture.
  - Makes it harder to reason about security and reliability.
- For a distributed system with security and failure handling, we need:
  - Clear docs for architecture and trust model.
  - A way to capture and justify key decisions.

**Why iterative, incremental with lightweight docs is a good fit:**

- We get:
  - Enough structure to design a safe, coherent system.
  - Short cycles to test assumptions and adjust.
  - Visible trace of why certain tech stacks / patterns were chosen.

Detailed tech choice rationales live in:

- `docs/architecture.md` (how components fit together).
- `docs/tech-choices.md` (stack / pattern options and reasons for the chosen ones).
- `docs/adr/` (specific decisions with trade-offs).

---

## 3. Objectives and Scope

### 3.1 Objectives for v0 (Prototype)

1. **LAN Mesh Compute**
  - Run jobs across 3-5 agents connected on a local network.
2. **Secure Communication**
  - Use TLS with mutual authentication between coodinator and agents.
3. **Basic Schedulingand Reliability**
  - Schedule jobs based on simple metrics (e.g., latency and load).
  - Reassing tasks when agents fail or time out.
4. **Operational Visivility**
  - Dashboard or CLI that shows node health, jobs, and basic metrics.
5. **Reproducible Local Setup**
  - One or few commands to start a small mesh development and testing.

### 3.2 Out of Scope for v0

- Public, permissionless global network.
- Token economics or credit systems.
- Zero-knowledge proofs or TEEs for verifiable compute (beyong simple redundany).
- Complex multi-tenant isolation or strong multi-user authorization.

---

## 4. Personas and Use Cases

### 4.1 Personas

1. **Mesh Admin (Infra / Lab Admin)**
  - Sets up the coordinator.
  - Controls which nodes are allowed to join.
  - Monitors health and address failures.

2. **Compute Consumer (Developer / Researcher)**
  - Submits jobs (e.g., batch compute tasks).
  - Tracks job status and retrieves results.

3. **Node Contributor (Device Owner)**
  - Installs and runs the agent.
  - Configures basic limits (CPU/GPU usage, memory).

### 4.2 Sample Use Cases

- Mesh Admin:
  - Start coordinator and see nodes auto-register.
  - Block a node from participating if needed.
  - Check which nodes are failing or unhealthy.

- Compute Consumer:
  - Submit a job (e.g., "run this script with parameters X, Y").
  - Watch progress (queued, running, done, failed).
  - Download or access results when complete.

- Ndoe Contributor:
  - Run agent with a simple command.
  - See that the node is recognized and used.
  - Limit resource usage to avoid impacting local work.

---

## 5. Requirements

### 5.1 Functional Requirements (Summary)

**Coordinator**

- Register nodes with capabilities and identity.
- Maintain node state using heartbeats.
- Accept job submission via an API.
- Schedule tasks and dispatch them to agents.
- Track progress, handle retries, and update job status.
- Aggregate task results into final job outputs.

**Agent**

- Obtain or load its certificate and key.
- Register with coordinator and advertise capabilities.
- Accept tasks and run them in a sandboxed environment.
- Send heartbeats and progress updates.
- Respect resource limits and timeouts per task.

**Network / Protocol**

- Discover the coordinator on a LAN (e.g., mDNS) or via configured address.
- Use TLS with mutual authentication for all control channels.
- Support request-response and streaming routes for tasks and logs.

**Dashboard / CLI**

- List all nodes with state (healthy, suspect, offline).
- Show jobs, tasks, and their status.
- Provide basic metrics (jobs per node, failures, average latency).
- Offer a simple interface to submit jobs and inspect results.

### 5.2 Non-Functional Requirements

**Performance**

- v0 should handle at least:
  - 3-5 nodes.
  - Tens of concurrent jobs.
- Job dispatch overhead (coordinator-side) should remain low on a LAN.

**Reliability**

- All traffic between coordinator and agents uses TLS with mutual authentication.
- Only nodes with valid certificates may join.
- Tasks run in constrained environments to reduce host impact.

**Observability**

- Log significant events: node join/leave, job submission, failures.
- Expose metrics via an endpoint (e.g., for Prometheus scraping).

---

## 6. Architecture Direction (High Level)

For v0, Planetary Mesh is composed of:

- **Coordinator Service**
  - API for clients.
  - Internal scheduler and job manager.
  - Node registry and heartbeat processing.

- **Agent Service**
  - Persistent process or daemon.
  - Receives tasks and runs them in a sandbox.
  - Reports progress and result.

- **Dashboard**
  - Simple web UI or CLI.
  - Communicates with coordinator's API.

Detailed component and data model design is documented in:

- `architecture.md` – structure and flows.
- `tech-choices.md` – options and rationale for stack/patterns.

---

## 7. Delivery Plan (Early Stages)

### 7.1 Epics (High Level)

1. **Networking & Discovery**
  - Coordinator listen server.
  - mDNS or static address discovery.
2. **Node Registration & Health**
  - Registration API.
  - Heartbeat and node state transitions.
3. **Job Submission & Scheduling**
  - Job submission API.
  - Scheduling logic and task dispatch.
4. **Agent Execution**
  - Task handler and sandbox runner.
  - Resource limit enforcement.
5. **Reliability Layer**
  - Timeouts, retries, and reassignment.
6. **Security**
  - Local CA or simple cert pipeline.
  - Enforceing mTLS and allowlists.
7. **Dashboard / Observability**
  - Node and job views.
  - Metrics and logs.

### 7.2 Early Iterations (Example)

- **Iteration 1**
  - Coordinator skeleton with health endpoint.
  - Agent skeleton with basic registration.
  - In-memory node registry.

- **Iteration 2**
  - Job submission API.
  - Simple scheduling (round-robin or random).
  - Single-node execution path.

- **Iteration 3**
  - Heartbeats , timeouts, and task reassignment.
  - Initial dashboard or CLI.
  - Basic metrics and logs.

---

## 8. Risks and Open Questions

Examples (to be refined as we progress):

- How to handle heterogeneous environments (different OS, hardware)?
- How strict sandboxing should be in v0 vs later phases.
- Which workload types should be supported first (script tasks vs container tasks).
- Exact backend stack (language, frameworks) and how it scales long term.

For each major question, related options and decisions are (or will be) documented in:

- `docs/tech-choices.md`
- `docs/adr/` (specific decision records)

---