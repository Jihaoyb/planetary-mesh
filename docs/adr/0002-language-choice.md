# ADR 0002: Use Go for Coordinator and Agent Services

- Status: Accepted
- Date: 2025-11-25

## Context

Planetary Mesh needs:

- Long-running services (coordinator, agents).
- Concurrency and networking (heartbeats, task dispatch, streaming logs).
- Simple deployment on various machines (lab PCs, edge nodes, etc.).

We considered several languages (Go, Rust, TypeScript/Node.js, Python).

## Decision

For v0, we will implement the coordinator and agent as **Go** services, built as static binaries.

## Alternatives Considered

- **Rust**
  - Pros: performance, memory safety.
  - Cons: steeper learning curve and slower iteration for this project stage.
- **TypeScript / Node.js**
  - Pros: fast iteration, strong ecosystem for web.
  - Cons: less ideal for long-running, high-concurrency daemons without extra care.
- **Python**
  - Pros: fast to prototype.
  - Cons: weaker for high-concurrency networking daemons and binary deployment on varied machines.

## Consequences

- Positive:
  - Simple single-binary deployment for coordinator and agents.
  - Good support for gRPC, TLS, and concurrent networking.
- Negative:
  - Need to maintain Go toolchain.
  - Some workloads (e.g., heavy ML code) may need to be called via external processes or containers.

This decision can be revisited in a future ADR if requirements change.
