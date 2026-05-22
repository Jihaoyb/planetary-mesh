# ADR 0012: Make coordinator job lifecycle transitions explicit

- Status: Accepted
- Date: 2026-05-21

## Context

After queued scheduling, cross-node reassignment, and node capability/load
visibility, the coordinator had the runtime behavior needed for the private
mesh prototype. The job status model was still mostly implicit in storage and
dispatch code.

Milestone 13 needs clearer job lifecycle behavior for reliability, operator
readiness, and API stability without changing the v0 HTTP/JSON protocol,
scheduler policy, command execution model, or Postgres schema.

## Decision

The coordinator owns an explicit job lifecycle state model for current command
jobs.

Current statuses:

- `QUEUED`
- `RUNNING`
- `COMPLETED`
- `FAILED`

Allowed transitions:

- accepted jobs start as `QUEUED`
- dispatch attempts transition `QUEUED` to `RUNNING`
- retry and reassignment attempts keep jobs `RUNNING`
- successful execution transitions `RUNNING` to `COMPLETED`
- terminal execution or dispatch failure transitions `RUNNING` to `FAILED`
- queued expiration can transition `QUEUED` to `FAILED`
- Postgres startup recovery can transition persisted `RUNNING` jobs to `FAILED`

Details:

- In-memory and Postgres job stores enforce the same lifecycle rules.
- Terminal `COMPLETED` and `FAILED` jobs are not overwritten by lifecycle
  methods.
- If no healthy node exists, jobs remain `QUEUED` with no attempts recorded.
- Duplicate dispatch protection remains process-local to one running
  coordinator.
- `CANCELLED` remains reserved/unsupported. No cancellation API or cancellation
  behavior is added.
- Public job JSON fields, status strings, and protocol version are unchanged.
- Postgres schema readiness metadata remains version `2`.

Milestone 15 narrows the Postgres startup recovery behavior for persisted
`RUNNING` jobs by adding bounded reconciliation grace before unreconciled
startup-running jobs are failed. See
[ADR 0013](0013-agent-reconciliation-strategy.md).

## Alternatives Considered

- **Document transitions only**
  - Pros: minimal code change.
  - Cons: storage methods could still drift from the documented lifecycle.

- **Add a cancellation API now**
  - Pros: would make the reserved `CANCELLED` status actionable.
  - Cons: requires agent execution semantics, in-flight cancellation behavior,
    and result handling that are outside this milestone.

- **Explicit lifecycle validation in coordinator storage (chosen)**
  - Pros: keeps state ownership in the coordinator and makes memory/Postgres
    behavior consistent without changing public API shape.
  - Cons: future lifecycle additions must update the transition model and tests.

## Consequences

- Positive:
  - Operators can rely on documented status meanings.
  - Tests cover current lifecycle behavior across default and Postgres-backed
    storage.
  - Terminal job history is protected from accidental lifecycle overwrites.
- Negative:
  - Unsupported persisted statuses remain inspectable but are not dispatchable.
  - Cancellation and richer progress states remain future work.
