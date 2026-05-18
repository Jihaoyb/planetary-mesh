# ADR 0003: Use HTTP/JSON for the v0 control plane

- Status: Accepted
- Date: 2026-04-06

## Context

At the time this ADR was accepted, the architecture doc and `tech-choices.md`
leaned toward gRPC for the coordinator-agent control plane. gRPC gives us typed
schemas, streaming, and good mTLS ergonomics, all of which we may want
eventually.

However, for the very first iterations we needed the control plane to be:

- Trivial to debug from `curl` and the browser.
- Free of code-generation steps in CI.
- Easy to evolve while the data model is still in flux (Job, Task, Node
  fields are still changing every few commits).

Standing up gRPC + protobuf tooling, generated code, and a separate
contract repo would have slowed down the early iterations without
providing much real value while there is only one client (the agent) and
one server (the coordinator).

## Decision

For v0 we implement the coordinator ↔ agent and client ↔ coordinator
control plane as **HTTP/JSON** using the Go standard library
(`net/http` + `encoding/json`).

The endpoint inventory can evolve while this decision remains the same: v0 uses
HTTP/JSON for coordinator-agent and client-coordinator control-plane traffic.
The current inventory is listed below for reader convenience.

## Current Endpoint Inventory

Coordinator:

- `GET  /healthz`         – basic health check
- `GET  /status`          – non-secret coordinator runtime status/config
- `POST /register`        – agent registration / heartbeat
- `GET  /nodes`           – list registered nodes
- `POST /jobs`            – create a job
- `GET  /jobs`            – list jobs
- `GET  /jobs/{id}`       – inspect a job
- `GET  /metrics`         – Prometheus-style text metrics

Agent:

- `GET  /healthz`         – basic health check
- `POST /execute`         – run an assigned job

Versioned coordinator endpoints except `/healthz` require
`X-Planetary-Protocol-Version: 1`. Agent `/execute` also requires the protocol
version header.

Original early endpoint inventory:

- Coordinator
  - `GET  /healthz`
  - `POST /register`        – agent registration / heartbeat
  - `GET  /nodes`           – list registered nodes
  - `POST /jobs`            – create a job
  - `GET  /jobs`            – list jobs
- Agent
  - `GET  /healthz`
  - `POST /execute`         – run an assigned job

This decision is explicitly **scoped to v0** and is expected to be
revisited once:

- The data model has stabilized.
- We need streaming (logs, progress updates).
- mTLS and structured contracts become a bigger pain point than the
  cost of introducing protobuf tooling.

## Alternatives Considered

- **gRPC + Protobuf from day one**
  - Pros: typed contracts, streaming, good mTLS story, matches the
    long-term architecture direction.
  - Cons: extra build/codegen step, harder to debug with `curl`, more
    moving parts while the schema is still churning.

- **Custom TCP / binary protocol**
  - Pros: maximum control and performance.
  - Cons: framing, versioning, and tooling all become our problem; no
    upside at this scale.

- **HTTP/JSON (chosen)**
  - Pros: no codegen, trivially debuggable, stdlib only, fast to evolve.
  - Cons: no schema enforcement, no built-in streaming, will need to be
    replaced or wrapped to get the long-term gRPC benefits.

## Consequences

- Positive:
  - We can iterate on the Job/Node/Task model without fighting tooling.
  - Tests can use `net/http/httptest` directly, with no mock server
    generation.
  - New contributors only need Go and `curl` to exercise the system.
- Negative:
  - There is no shared schema between coordinator and agent. Drift is
    possible and must be caught by tests.
  - Streaming features (logs, progress) will require either SSE,
    WebSockets, or the eventual gRPC migration.
- Open questions:
  - When exactly to migrate to gRPC. A reasonable trigger is "we need
    streaming logs OR we have a second non-Go client."
  - Whether the eventual gRPC layer should fully replace HTTP/JSON or
    sit alongside it (REST kept for the dashboard, gRPC for internal
    coordinator ↔ agent).
