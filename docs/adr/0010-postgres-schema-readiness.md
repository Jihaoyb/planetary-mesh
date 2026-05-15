# ADR 0010: Add lightweight Postgres schema readiness metadata

- Status: Accepted
- Date: 2026-05-15

## Context

ADR 0006 chose embedded Postgres schema initialization for v0 and explicitly did
not introduce a migration framework. After Milestone 8, durable operation is
verified through opt-in Postgres tests and a Compose smoke workflow, but the
database schema itself is not inspectable as a versioned runtime fact.

Future Postgres schema changes need a safer baseline without changing current
runtime behavior, adding a new persistence backend, or making the default test
suite depend on external services.

## Decision

Keep embedded schema initialization, and add a lightweight Postgres schema
readiness marker.

Details:

- Add a `schema_version` table managed by coordinator startup.
- Record the current embedded schema as version `1`.
- Backfill missing metadata on existing Postgres databases after embedded schema
  initialization succeeds.
- Fail startup if the database records a schema version newer than the
  coordinator binary expects.
- Expose non-secret schema readiness through coordinator `/status`, `/metrics`,
  startup logs, opt-in Postgres tests, and the Postgres smoke workflow.
- Do not add a full migration framework in this milestone.

## Alternatives Considered

- **Full migration framework**
  - Pros: mature versioned migration lifecycle.
  - Cons: more machinery than the current v0 schema needs and explicitly out of
    scope for this milestone.

- **Continue with no schema metadata**
  - Pros: no code or operator surface changes.
  - Cons: future schema changes remain harder to reason about and harder to
    verify in smoke tests.

- **Embedded schema with readiness metadata (chosen)**
  - Pros: keeps current runtime model while making schema state inspectable and
    testable.
  - Cons: still not a general-purpose migration system.

## Consequences

- Positive:
  - Operators and tests can confirm that Postgres schema state matches the
    coordinator binary.
  - Existing Postgres databases without metadata are upgraded in place to record
    version `1`.
  - Older binaries fail fast rather than running against databases initialized by
    newer code.
- Negative:
  - Real schema migrations are still deferred.
  - Future schema changes must decide whether embedded SQL plus version checks
    remain sufficient or whether to adopt a migration framework.
