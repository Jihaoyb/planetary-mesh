# Planetary Mesh - Tech Choices and Rationale

This document lists important technology / pattern options for Planetary Mesh and explains why we choose (or lean toward) specific ones for v0

This is not final forever. Decisions can change, but changes should be documented (e.g., via ADRs).

---

## 1. Process and Documentation Patterns

### 1.1 Options

- **Strict Waterfall**
  - Heavy upfront requirements and design.
  - Sequential phases; limited iteration.

- **Heavyweight RUP / Spiral**
  - Strong phase structure and risk analysis.
  - Extensive documentation and governance.

- **Ad-hoc Development**
  - Minimal documentation or process.
  - Code-first, design later (if at all).

- **Iterative / Incremental (Agile-inspired)**
  - Short iterations.
  - Continuous integration and feedback.
  - Lightweight but real documentation.

### 1.2 Choice

We choose **Iterative / Incremental, Agile-inspired with lightweight docs**.

**Why this choice**

- The project is **exploratory** (new kind of local mesh compute) and will require adjustments.
- We need **enough structure** for security and distributed-systems complexity.
- We want to **avoid heavy ceremony** and move quickly.

The kickoff plan and architecture docs support this model by:

- Defining goals and structure.
- Leaving room for change as we learn from real runs.

---

## 2. Backend Language for Coordinator and Agent

*(This section can be updated once you formally pick the langauge and record an ADR.)*

### 2.1 Options (Examples)

- **Go**
  - Strong concurrency model (goroutines, channels).
  - Compiled, single static binaries (easy deployment).
  - Good ecosystem for gRPC, TLS, and networking.

- **Rust**
  - Excellent performance and safety.
  - Strong type system and memory safety.
  - More complex onboarding and build times for some teams.

- **Node.js (TypeScript)**
  - Fast iteration and quick prototyping.
  - Great ecosystem for web, but long-running high-load services need careful tuning.

- **Python**
  - Very fast for prototypes; rich ecosystem.
  - Less ideal for high-concurrency network daemons without extra frameworks.

### 2.2 Tentative Choice

For v0, we **lean toward Go** for both coordinator and agent.

**Reasons**

- Simple deployment (single binary) fits **agents on many machines**.
- Good fit for **concurrent networking** and **long-running daemons**.
- Strong support and libraries for **gRPC + TLS**.
- Easier onboarding and CI than a more complex toolchain for this use case.

If a different language is chosen later, the change and its rationale should be captured in an ADR (e.g., `adr/0001-language-choice.md`).

---

## 3. Communication Protocol Style

### 3.1 Options

- **REST + JSON over HTTPS**
  - Very familiar.
  - Easy to debug.
  - Less efficient for high-frequency, streaming interactions.

- **gRPC (HTTP/2 + Protobuf)**
  - Strong typing via proto files.
  - Efficient binary serialization.
  - Built-in streaming and good support for mutual TLS.

- **Custom TCP / Binary Protocol**
  - Maximum control and potentially high performance.
  - More custom work (framing, versioning, tooling).

  ### 3.2 Choice

For control-plane communication (Coordinator ↔ Agent ↔ Dashboard API), we **lean toward gRPC**.

**Reasons**

- The system needs **structured, versioned messages** and **streaming** (for progress updates, logs).
- gRPC + Protobuf gives:
  - Schema-based definitions.
  - Good performance.
  - Built-in support for **mTLS**.
- Better than custom TCP for v0, because we can rely on battle-tested tooling and focus on core mesh logic.

REST endpoints may still be exposed for the dashboard or simple client integration, but the internal coordinator–agent protocol is likely gRPC.

---

## 4. Data Storage

### 4.1 Options

- **Relational DB (e.g., Postgres)**
  - Strong consistency and schema.
  - Good choice for jobs, tasks, node records.

- **Document Store (e.g., MongoDB)**
  - Flexible schema.
  - Good for variable payloads, less strict for relational queries.

- **Embedded DB (e.g., SQLite)**
  - Very simple to ship.
  - Good for single-node coordinator, later scaling may require migration.

- **In-memory Only**
  - Very fast, but state is lost on restart.
  - Hard to reason about failures and recovery.

### 4.2 Choice

For v0, we **lean toward Postgres** (or another relational DB) for coordinator state.

**Reasons**

- Jobs, tasks, and node states are naturally relational.
- We need **durability** and **queries** across jobs and nodes.
- Postgres is a solid default with good tooling and libraries.

In-memory caches (or in-memory-only prototypes) can still be used **in early iterations**, but the target architecture assumes durable storage.

---

## 5. Deployment and Local Development

### 5.1 Options

- **Local binaries + manual startup**
  - Minimal overhead but manual wiring of services.

- **Docker + Docker Compose**
  - Standardizes runtime across machines.
  - Easy to bring up coordinator, agents, and DB together.

- **Kubernetes**
  - Powerful orchestration and scaling.
  - Heavy for early prototypes and local dev.

### 5.2 Choice

For v0, we **lean toward Docker + Docker Compose** for local dev and demos.

**Reasons**

- Good balance of **repeatability** and **simplicity**.
- Easy to share a demo config (e.g., `docker-compose up` starts a full mesh env).
- K8s can be considered later if/when the system needs production-grade orchestration.

---

## 6. Task Execution Model on Agents

### 6.1 Options

- **Direct Process Execution**
  - Run specific executables or scripts with arguments.
  - Simple to implement but may be OS-dependent.

- **Container-based Execution**
  - Run tasks in containers (e.g., Docker).
  - Better isolation and repeatability but heavier.

- **VM / MicroVM-based Execution**
  - Strong isolation.
  - Heavy for small tasks and early prototypes.

### 6.2 Choice

For v0, we **lean toward direct process execution** with clear constraints.

**Reasons**

- Easier to implement and debug for early stages.
- Good enough to prove the coordinator–agent–dashboard flow.
- Container-based execution can be added later once the basic system is stable.

---

## 7. Scheduling Strategy

### 7.1 Options

- **Simple Round-Robin / Random**
  - Very easy to implement.
  - Does not consider node load or latency.

- **Score-Based Scheduling (Latency + Load + Reliability)**
  - Uses metrics to pick better nodes.
  - More logic, but more realistic behavior.

- **Advanced Scheduling (e.g., queues per node, priorities, SLAs)**
  - More complex policies and configuration.

### 7.2 Choice

For v0, we use a **score-based scheduler** with a simple formula:

```text
score = α * RTT + β * Load + γ * Queue + δ * Reliability
```

**Reason**

- Still simple enough to implement and tune.
- Already captures key mesh concerns:
  - Latency (RTT).
  - Current load.
  - Queue length.
  - Historical reliability.

More Advanced policies can be added later as needed.

---

## 8. Observability Stack

### 8.1 Options

- **Logging Only**
  - Simple application logs, no structured metrics.

- **Metrics + Logs**
  - Expose metrics via HTTP endpoint and use something like Prometheus + Grafana.

- **Full Tracing**
  - Distributed tracing (e.g., OpenTelemetry) from start.

### 8.2 Choice

For v0, we **target metrics + logs**:
  - Structured logs from coordinator and agents.
  - A basic metrics endpoint from coordinator (and possibly agents).

**Reason**

- Provides enough visibility for debugging and tuning.
- Not as heavy as full tracing for a first prototype.

---

## 9. Recording Decisions (ADRs)

For any non-trivial choice (language, protocol, storage, etc.), we should:

- Create a new file under [docs/adr/](docs/adr/), for example:
  - [docs/adr/0001-language-choice.md]
  - [docs/adr/0002-grpc-vs-rest-for-internal-protocol.md]

Each ADR includes:

- Context
- Decision
- Alternatives considered
- Consequences

This keeps the project history clear and explains **why** things look the way they do.

---