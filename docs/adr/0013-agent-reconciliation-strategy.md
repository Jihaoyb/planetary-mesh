# ADR 0013: Define agent reconciliation strategy after coordinator restart

- Status: Accepted
- Date: 2026-05-21

## Context

Milestone 13 made coordinator-owned job lifecycle transitions explicit. That
clarified the restart gap that existed at the time: with Postgres enabled,
coordinator startup marked persisted `RUNNING` jobs `FAILED` with
`coordinator restarted before result was recorded`. Agents do not persist
execution history or report completed results after coordinator restart.

Behavior before Milestone 15 remained intentionally simple:

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

The chosen runtime strategy is explicit agent-to-coordinator result reporting,
not heartbeat-carried reconciliation.

Details for the runtime slice:

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

## Milestone 15 Implementation Notes

Milestone 15 implements the first runtime slice of this ADR:

- Coordinator endpoint: `POST /jobs/{id}/result`.
- Request shape matches the preferred body above and uses the existing protocol
  version header.
- Public job JSON fields, job status strings, and `ExecuteRequest` /
  `ExecuteResponse` shapes are unchanged.
- Accepted reports return `200 OK` with the current public job JSON.
- Same-node duplicate or late reports for already-terminal jobs also return
  `200 OK` with the current job JSON so agents can drop cached reports.
- Unknown jobs, wrong-node reports, unsupported reported statuses, and
  unsupported current states do not mutate storage and use the existing
  `http.Error` response style.
- In secure mode, result reports pass through the same client-certificate and
  node allowlist checks used for registration.
- The store-level reported-result acceptance operation is atomic and returns an
  explicit outcome, so concurrent dispatch/result-report races cannot overwrite
  terminal jobs or accept stale wrong-node reports.
- Postgres startup captures persisted `RUNNING` job ids and starts serving HTTP
  during reconciliation grace instead of sleeping before `ListenAndServe`.
- Default grace is `30s`; `COORDINATOR_RECONCILIATION_GRACE=0s` preserves
  immediate startup failure behavior.
- When grace expires, only remaining captured startup `RUNNING` job ids are
  marked `FAILED` with
  `coordinator restarted before result was recorded`.
- Agents keep a bounded in-memory cache of terminal command results only:
  maximum 128 entries and 5 minute TTL.
- Agents drop cached reports after `2xx`, older-coordinator compatibility
  responses such as `404` or `405`, and permanent non-retryable `4xx`
  responses; they retry network errors and `5xx` until cache expiry.
- Agent restart still loses cached result history, and command execution remains
  tied to the `/execute` request context.
- Postgres schema readiness metadata remains version `2`; no task, attempt, or
  result-history table was added.

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
  - Current behavior remained stable for Milestone 14.
  - The future implementation path is compatible with HTTP/JSON v0 and older
    agents.
  - Terminal job immutability remains part of the public lifecycle contract.
- Negative:
  - Milestone 14 did not fix the lost-result gap by itself.
  - The first runtime slice is still best-effort because agent result history is
    in-memory only.
  - Commands already tied to a dropped `/execute` request context may still be
    canceled before a result exists to report.
- Open questions:
  - Whether future durable agent result history is worth the added local storage
    and cleanup policy.
  - Whether future metrics should distinguish more detailed classes of ignored
    result reports.
