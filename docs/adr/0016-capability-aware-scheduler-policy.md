# ADR 0016: Schedule command jobs by required capabilities and reported load

- Status: Accepted
- Date: 2026-07-29

## Context

Planetary Mesh agents already report normalized capability labels and an
`active_executions` load value on registration and heartbeat. Milestone 10 made
that metadata visible to operators, but the coordinator still dispatches every
job to the first healthy node returned by the node store. In-memory store order
is map-derived, so placement is not stable, and operators must currently prepare
every healthy agent for every allowlisted workload.

Phase 2 needs a narrow placement policy for heterogeneous private meshes without
turning capability labels into verified inventory or adding a capacity,
reservation, fairness, or workflow system. The policy also needs to fail closed
when a new client submits placement requirements to an older coordinator that
would otherwise ignore an unknown JSON field.

Postgres schema readiness version `2` currently persists node capability/load
metadata but not job placement requirements. HTTP/JSON v0 and
`X-Planetary-Protocol-Version: 1` remain the accepted control plane.

## Decision

Command jobs may carry coordinator-owned `required_capabilities` metadata.
Requirements use all-of matching and the existing capability label grammar:

- trim surrounding whitespace
- require an ASCII alphanumeric first character
- allow ASCII alphanumeric characters plus `.`, `_`, `:`, and `-`
- allow at most 64 characters per label
- deduplicate exact, case-sensitive labels
- allow at most 32 distinct labels
- sort labels lexically

Absent, JSON `null`, and empty requirements mean no constraint and normalize to
an empty array.

### Fail-closed submission

Add `POST /jobs/command` as the placement-aware command submission endpoint.
Its request contains `type: "command"`, `command`, optional `args`, and optional
`required_capabilities`. The endpoint remains under protocol version `1` and
returns the existing job representation with an additive, always-present
`required_capabilities` array.

The existing `POST /jobs` endpoint remains available for legacy unconstrained
jobs. A new coordinator rejects any occurrence of `required_capabilities` on
that endpoint, including `null` or an empty array.

New `pmctl` binaries use `POST /jobs` when requirements are empty so ordinary
submission remains compatible with old coordinators. They use
`POST /jobs/command` when requirements are nonempty. A `404` or `405` response
from that placement-aware request is reported as an unsupported coordinator
feature, and `pmctl` never falls back to an unconstrained request.

An old coordinator handles `POST /jobs/command` as an unsupported method and
does not create a job. Direct HTTP callers that need constraints must therefore
use the placement-aware endpoint rather than adding the field to the legacy
endpoint.

### Eligibility and ordering

Each dispatch reads one job and one node snapshot. Eligible candidates are
`HEALTHY` nodes whose reported capabilities contain every required label.
Candidates are sorted by:

1. `load.active_executions` ascending
2. node ID ascending

The same deterministic least-reported-active ordering applies to unconstrained
jobs.

The candidate list is a heartbeat snapshot, not a reservation or capacity
guarantee. It remains fixed for the dispatch's initial attempt, per-node
retries, and cross-node retryable reassignment. Concurrent jobs may choose the
same node from equivalent snapshots. Node metadata changes are considered the
next time a queued job is dispatched, not during an already-started dispatch.

If no eligible node exists, the job stays `QUEUED` with no new attempt and no
agent contact. The existing scheduler may dispatch it after a later matching
heartbeat. The current per-node retry limit, timeout, exponential backoff,
terminal error classification, fixed queued-job expiry, lifecycle states, and
result model remain unchanged.

### Storage and rollback

Persist canonical requirements in the in-memory and Postgres job stores. Slice
values are cloned at storage boundaries.

Add one Postgres `jobs.required_capabilities` JSONB column with a non-null empty
array default and advance schema readiness metadata from version `2` to version
`3`. Schema application remains transactional and idempotent. Existing rows are
backfilled as unconstrained, and databases newer than version `3` remain
rejected.

A version-2 coordinator must not open a database marked version `3`. Supported
rollback therefore requires restoring a complete pre-upgrade database backup.
Manually decrementing schema metadata or dropping the column is unsupported.
This milestone does not add a general migration framework or down migration.

### Trust and runtime boundaries

Capability labels are operator-configured assertions. Matching a label does not
verify hardware, installed software, an agent allowlist, wrapper availability,
files, identity, or actual capacity.

Agents continue executing only explicitly allowlisted logical command keys with
`exec.CommandContext`, no shell, the configured fixed timeout, and bounded
stdout/stderr. Placement requirements are never sent to agents. Template schema
version `1` remains command/argument expansion; placement flags are invocation
metadata rather than template fields.

## Alternatives Considered

### Add `required_capabilities` only to `POST /jobs`

Rejected because old coordinators tolerate unknown JSON fields and could create
an unconstrained job after silently ignoring the requirement.

### Bump the shared protocol version

Rejected because the change is an additive coordinator/operator capability.
Agent registration, execution, result reporting, and existing unconstrained
clients remain compatible with protocol version `1`.

### Preserve first-node behavior for unconstrained jobs

Rejected because in-memory node iteration is unstable and would leave ordinary
jobs with a less deterministic policy than constrained jobs. Applying one
least-reported-active ordering is simpler and predictable.

### Reserve capacity or update load during dispatch

Rejected because reported load is a heartbeat snapshot. Reservations, fairness,
capacity models, priorities, and quotas require broader scheduler planning.

### Probe agents for executable or file availability

Rejected because it would expand the agent protocol and trust model. Allowlist
enforcement remains agent-local and terminal at execution time.

## Consequences

Positive:

- Heterogeneous private meshes can route approved workloads to prepared hosts.
- Placement fails closed across old and new coordinator/client combinations.
- Initial dispatch and reassignment become deterministic.
- Requirements survive Postgres restart and are visible to operators.

Negative:

- Stale or concurrent heartbeat snapshots can still concentrate jobs.
- Incorrect capability assertions can queue or misroute work.
- Postgres schema version `3` requires backup restoration to roll back to a
  version-2 coordinator.
- The additional endpoint and job field expand the manual v0 API inventory.

Out of scope:

- protocol version `2` or agent wire changes
- hardware discovery or verified capability attestation
- reservations, fairness, quotas, priorities, or new lifecycle states
- file transfer, artifact storage, template registries, or workflow engines
- sandboxing, containers, VMs, secrets, or multi-tenant authorization
- new metrics or runtime dependencies
