# ADR 0011: Add node capability and load visibility

- Status: Accepted
- Date: 2026-05-20

## Context

After queued scheduling and cross-node reassignment, operators can see node id,
address, health state, last heartbeat, and certificate metadata. That is enough
for basic dispatch, but it does not tell a private mesh operator what kind of
node they are looking at or whether it is currently running work.

The next private-mesh hardening step needs better node visibility without
changing scheduler behavior, adding hardware discovery, introducing a task
table, or implying GPU/resource marketplace support.

## Decision

Agents report optional static capabilities and approximate active command
execution count through the existing HTTP/JSON registration and heartbeat path.

Details:

- Extend `POST /register` additively with `capabilities` and `load`.
- Extend `GET /nodes` and registration responses with the same fields.
- Keep protocol version `X-Planetary-Protocol-Version: 1`.
- Configure static agent labels through `AGENT_CAPABILITIES`.
- Report only `load.active_executions` for now.
- Preserve compatibility with older agents by defaulting missing fields to empty
  capabilities and zero active executions.
- Persist the new node metadata in the existing Postgres `nodes` table.
- Advance Postgres schema readiness metadata to version `2`.
- Keep scheduler selection unchanged: first healthy node for initial dispatch,
  then cross-node reassignment only after retryable failures.

## Alternatives Considered

- **Scheduler policy in the same milestone**
  - Pros: immediately uses the new data.
  - Cons: mixes visibility with scheduling semantics and makes behavior harder
    to review.

- **Hardware/resource auto-discovery**
  - Pros: less operator config.
  - Cons: larger platform-specific surface and premature for the current
    private mesh prototype.

- **Task-level load or queue depth**
  - Pros: richer scheduling input.
  - Cons: requires more execution/state modeling than current command jobs need.

- **Static labels plus active execution count (chosen)**
  - Pros: simple, inspectable, backwards-compatible, and useful to operators.
  - Cons: active count is only a heartbeat snapshot and does not describe total
    capacity.

## Consequences

- Positive:
  - Operators can distinguish nodes by labels and see approximate current work.
  - Existing agents remain compatible with the coordinator.
  - Future scheduler work has a narrow metadata base to build on.
- Negative:
  - Load values can be stale for `SUSPECT` or `OFFLINE` nodes.
  - Capabilities are operator-provided labels, not verified hardware facts.
  - Schema version `2` is required for Postgres-backed durable metadata.
- Open questions:
  - What scheduler policy should use this metadata, if any.
  - Whether future load reporting should include capacity, queue depth, or
    hardware/runtime discovery.
