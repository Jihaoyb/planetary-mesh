# ADR 0013: Define agent reconciliation strategy after coordinator restart

- Status: Accepted
- Date: 2026-05-21

## Context

Milestone 13 made coordinator-owned job lifecycle transitions explicit. That
clarified the current restart gap: with Postgres enabled, coordinator startup
marks persisted `RUNNING` jobs `FAILED` with
`coordinator restarted before result was recorded`. Agents do not persist
execution history or report completed results after coordinator restart.

Current behavior remains intentionally simple:

- in-memory coordinator state is lost on restart
- Postgres persists nodes and jobs only
- dispatch marks a job `RUNNING` before sending agent `/execute`
- the coordinator records terminal results only after the synchronous agent
  response returns
- agent `/execute` uses `exec.CommandContext` with the request context plus the
  configured execution timeout
- terminal `COMPLETED` and `FAILED` jobs are not overwritten

This creates several private-mesh reliability questions: a result can be lost if
the coordinator crashes after an agent finishes but before the result is
persisted, older agents will not know how to reconcile, and late or duplicate
reports must not weaken terminal job immutability.

## Decision

Milestone 14 records the reconciliation strategy only. It does not change
runtime behavior, public job JSON, status strings, schema, scheduling, command
execution, mTLS, or `pmctl`.

The future runtime strategy is explicit agent-to-coordinator result reporting,
not heartbeat-carried reconciliation.

Details for the future runtime slice:

- Add a coordinator endpoint for agents to report completed command results,
  using HTTP/JSON and `X-Planetary-Protocol-Version: 1`.
- Prefer `POST /jobs/{id}/result` with a body shaped like:

  ```json
  {
    "node_id": "agent-1",
    "status": "COMPLETED",
    "exit_code": 0,
    "stdout": "hello\n",
    "stderr": "",
    "stdout_truncated": false,
    "stderr_truncated": false,
    "last_error": ""
  }
  ```

- `status` is limited to terminal job states currently supported by the
  coordinator: `COMPLETED` and `FAILED`.
- The coordinator remains the authority for validation, result acceptance,
  lifecycle transitions, node identity checks, storage, metrics, and operator
  visibility.
- Agents may keep a bounded in-memory result cache for recently completed
  executions while the process is alive. Agent restarts lose that cache.
- New agents still return the synchronous `/execute` response; asynchronous
  result reporting is additive and best-effort.
- A new coordinator with older agents remains compatible: older agents do not
  report results, and unreconciled jobs follow the coordinator recovery policy.
- A new agent with an older coordinator remains compatible: result reporting can
  receive `404` or another non-success response, but normal synchronous
  execution remains the primary path.
- In secure mode, result reporting uses the same client-certificate and node
  allowlist expectations as registration.

Storage and lifecycle policy:

- Preserve nodes/jobs-only storage for the first runtime slice.
- Keep Postgres schema readiness metadata at version `2` unless a later
  implementation proves a schema change is necessary.
- In-memory mode has no coordinator restart recovery because state is process
  local.
- Postgres restart recovery should use a bounded reconciliation grace window
  before failing persisted `RUNNING` jobs.
- During that grace window, persisted `RUNNING` jobs remain `RUNNING` and are not
  re-dispatched.
- A matching result report can transition a `RUNNING` job to `COMPLETED` or
  `FAILED`.
- If no matching report arrives before the grace window expires, the coordinator
  marks the job `FAILED` with the existing restart recovery error.
- Terminal immutability remains absolute: restart-recovered `FAILED` jobs and
  any other terminal jobs are not updated by late reports.

Result acceptance policy:

- Unknown jobs do not create new job records.
- Reports for unsupported job states, unsupported terminal statuses, or wrong
  nodes do not mutate job state.
- Duplicate and late reports for already-terminal jobs do not mutate job state.
- Concurrent dispatch and result reporting must use an atomic store operation
  that updates only the expected `RUNNING` job for the matching node.
- Retryable dispatch failures, cross-node reassignment, terminal dispatch
  failures, no-healthy-node queue retention, queued expiration, and process-local
  duplicate dispatch protection are unchanged.

## Alternatives Considered

- **Keep current restart recovery forever**
  - Pros: no new protocol or agent behavior.
  - Cons: known completed results can remain lost after a coordinator crash.

- **Carry result reports in heartbeat registration**
  - Pros: no new endpoint.
  - Cons: mixes node liveness with job result acceptance, makes payloads
    unbounded, and complicates registration compatibility and validation.

- **Let agents own reconciliation state durably**
  - Pros: can survive agent restarts.
  - Cons: adds agent-side persistence and cleanup policy before the coordinator
    result contract is settled.

- **Permit terminal job updates during reconciliation**
  - Pros: can recover results even after startup has marked jobs failed.
  - Cons: weakens the Milestone 13 lifecycle contract and makes operator-visible
    terminal history less predictable.

- **Explicit result reporting with a coordinator grace window (chosen)**
  - Pros: preserves coordinator ownership, keeps the protocol additive, avoids
    schema changes for the first slice, and protects terminal immutability.
  - Cons: does not recover results after agent restart and does not guarantee
    in-flight commands survive a dropped coordinator connection.

## Consequences

- Positive:
  - The restart recovery design is explicit before runtime changes begin.
  - Current behavior remains stable for Milestone 14.
  - The future implementation path is compatible with HTTP/JSON v0 and older
    agents.
  - Terminal job immutability remains part of the public lifecycle contract.
- Negative:
  - Milestone 14 does not fix the lost-result gap by itself.
  - The first runtime slice will still be best-effort if agent result history is
    in-memory only.
  - Commands already tied to a dropped `/execute` request context may still be
    canceled before a result exists to report.
- Open questions:
  - The exact reconciliation grace duration and whether it should be
    configurable.
  - Whether future durable agent result history is worth the added local storage
    and cleanup policy.
  - Whether metrics should distinguish startup recovery failures from
    reconciliation-accepted terminal reports.
