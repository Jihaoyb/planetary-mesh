# ADR 0006: Use Postgres for durable coordinator state

- Status: Accepted
- Date: 2026-04-30

## Context

Milestone 2 made jobs concrete by adding allowlisted direct command execution
and result capture. The coordinator can now hold useful job history, but ADR
0004's in-memory-only runtime storage loses that history on restart.

The Milestone 3 goal is narrow durability:

- Persist nodes and jobs.
- Keep ordinary unit tests DB-free.
- Avoid task fanout and task tables until a later milestone.
- Preserve the current HTTP/JSON wire shape, including `job-N` job IDs.

## Decision

The coordinator uses Postgres as its durable runtime store when
`COORDINATOR_DATABASE_URL` is configured. Without that environment variable, the
coordinator continues to use in-memory storage.

Implementation details:

- Storage is hidden behind narrow coordinator interfaces.
- Schema initialization is embedded in the binary; no migration framework is
  introduced for v0.
- Job IDs remain formatted by application code as `job-N`, backed by a Postgres
  sequence.
- On startup, persisted `RUNNING` jobs are marked `FAILED` with:
  `coordinator restarted before result was recorded`.
- Postgres integration tests are opt-in and gated separately from default
  `go test ./...`.

## Alternatives Considered

- **Continue in-memory runtime storage**
  - Pros: simplest runtime and fastest tests.
  - Cons: cannot preserve node/job history across coordinator restarts.

- **SQLite**
  - Pros: low operational overhead.
  - Cons: not the roadmap persistence target and would create a second
    migration path later.

- **Postgres with a migration framework**
  - Pros: stronger schema evolution story.
  - Cons: unnecessary operational weight for the current small v0 schema.

- **Embedded schema initialization with Postgres (chosen)**
  - Pros: durable runtime state with minimal new machinery.
  - Cons: future schema changes will eventually need a real migration approach.

## Consequences

- Positive:
  - Nodes and jobs survive coordinator restarts.
  - The default test suite remains DB-free.
  - Compose can run a realistic coordinator + Postgres + agent stack.
- Negative:
  - Operators must provide a Postgres database for durable runtime state.
  - Metrics counters remain process-local and reset on restart.
  - Schema evolution is intentionally simple for now.
- Known v0 gap:
  - If an agent completed a job before a coordinator crash and the coordinator
    did not persist the result, that result is lost. There is no agent
    reconciliation in v0.
