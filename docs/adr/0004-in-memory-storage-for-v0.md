# ADR 0004: Use in-memory storage for the v0 control plane

- Status: Superseded by ADR 0006 for runtime persistence; in-memory storage remains for tests and local fallback.
- Date: 2026-04-06

## Context

The architecture doc describes a relational store (Postgres) as the
target for coordinator state: nodes, jobs, and tasks. We agree with that
direction long term.

In the early iterations, however, the priorities were:

- Validate the control-plane shape (registration, heartbeats, dispatch).
- Iterate quickly on the Node/Job structs.
- Keep the test surface small (no DB to spin up in CI).

Introducing Postgres at this stage would have:

- Added migration tooling and schema decisions before the model was stable.
- Made tests slower and more environment-dependent.
- Forced configuration and connection management work that does not
  contribute to the v0 demo.

## Decision

For v0, the coordinator stores nodes and jobs in **in-memory, mutex-protected
maps** (`NodeRegistry` and `JobStore`). State is lost on restart.

This is explicitly a **temporary** choice. The plan is to migrate to a
durable store (likely Postgres, with SQLite as a possible dev fallback)
before:

- Implementing retry exhaustion and reassignment logic that depends on
  surviving coordinator restarts.
- Wiring up a dashboard that users will rely on for real work.
- Any multi-coordinator or HA work.

## Alternatives Considered

- **Postgres from day one**
  - Pros: real persistence, transactions, queries, matches long-term
    architecture.
  - Cons: schema migrations and DB lifecycle become a constant cost
    while the model is still changing daily.

- **SQLite embedded**
  - Pros: persistence with zero ops; great for single-coordinator dev.
  - Cons: still requires schema work; will need a second migration if
    we move to Postgres later anyway.

- **In-memory maps (chosen)**
  - Pros: zero setup, fastest possible iteration, trivially testable.
  - Cons: state is lost on restart; cannot validate any flow that
    depends on durability.

## Consequences

- Positive:
  - Tests run with no external dependencies.
  - The Node/Job structs can change without writing migrations.
  - The HTTP layer is the only thing under test, which keeps the focus
    sharp.
- Negative:
  - Coordinator restart wipes all state. No flow that depends on
    durability can be demoed yet.
  - Some abstractions (the registry/store types) will need a small
    refactor when a real backend is introduced. We can mitigate this by
    keeping the storage interface narrow from the start.
- Open questions:
  - Exact trigger for the persistence migration. Current plan: do it
    immediately after the control plane is hardened (graceful shutdown,
    metrics, retries) and before the security work, so retries can
    survive restarts.
  - Whether to introduce a `Store` interface now to make the swap easier
    later, or to wait until the second backend exists.
