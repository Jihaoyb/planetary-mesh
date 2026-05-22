# ADR 0014: Maintain a manual HTTP/JSON v0 API inventory and compatibility policy

- Status: Accepted
- Date: 2026-05-22

## Context

Milestone 15 expanded the HTTP/JSON v0 control plane with additive agent
terminal result reporting, bounded Postgres reconciliation grace, reconciliation
status metadata, and new metrics. The API is still small and implemented with
the Go standard library, but endpoint behavior, public JSON fields, metrics, and
protocol-version expectations now span the coordinator, agent, and `pmctl`.

ADR 0003 accepted HTTP/JSON for v0 because it kept the early control plane easy
to debug and avoided code generation while the model changed quickly. That
tradeoff remains valid, but scattered code and tests are no longer enough as the
only API source of truth.

The project needs API/operator readiness before generated OpenAPI/protobuf,
dashboard/UI work, remote private mesh, packaging, scheduler policy, shared
pools, or marketplace work.

## Decision

Maintain a manual v0 HTTP/JSON API inventory and compatibility policy at
[../api-http-json-v0.md](../api-http-json-v0.md).

The inventory is the authoritative review reference for the current implemented
HTTP/JSON v0 coordinator, agent, metrics, and `pmctl`-facing surfaces. It
documents:

- coordinator operator/client-facing endpoints
- coordinator agent-facing endpoints
- agent coordinator-facing endpoints
- public JSON field names
- metric names and current counter/gauge types
- protocol header expectations
- important status codes
- mTLS/node identity expectations where applicable
- compatibility policy for future additive and breaking changes

Generated OpenAPI, protobuf, gRPC, WebSockets, SSE, streaming logs, and SDK
generation remain out of scope for Milestone 16. Adding any of those requires a
future explicit decision.

The manual inventory is not a permanent generated SDK/API guarantee. It is the
current v0 compatibility reference while the API remains small and
standard-library-driven.

## Compatibility Policy

The policy recorded in the inventory is part of this decision:

- HTTP/JSON remains the v0 runtime protocol.
- Protocol version remains `X-Planetary-Protocol-Version: 1`.
- Coordinator and agent health endpoints stay unversioned.
- Additive JSON fields may be introduced under version `1` when older clients
  tolerate unknown fields.
- Removing or renaming public fields requires a future milestone and likely an
  ADR.
- Changing job status strings or lifecycle semantics requires future
  lifecycle/API planning.
- New endpoints should be additive.
- Error responses remain `http.Error` text bodies, not a JSON envelope.
- `pmctl` remains a thin client and must not become authoritative for
  coordinator validation, scheduling, lifecycle, result acceptance, metrics, or
  storage behavior.
- Metric names and current counter/gauge types are part of the v0 operational
  surface and should be documented when changed, without overstating permanent
  Prometheus stability.

## Alternatives Considered

### Generated OpenAPI now

Pros:

- Gives a machine-readable REST-style contract.
- Could support future SDK generation or documentation tooling.

Cons:

- Adds tooling and maintenance before the small API has fully stabilized.
- Risks encoding incidental behavior into a generated contract too early.
- Does not help coordinator-agent behavior that is still intentionally narrow
  and Go-internal.

### Protobuf or gRPC now

Pros:

- Gives typed schemas and a path to streaming features.
- May fit future internal coordinator-agent traffic.

Cons:

- Conflicts with ADR 0003's v0 HTTP/JSON decision without a stronger trigger.
- Adds code generation and a second protocol decision before current API
  readiness work is complete.

### Continue with scattered code and tests only

Pros:

- No documentation work.

Cons:

- Future contributors have no single current reference for endpoint behavior,
  public JSON fields, status-code expectations, or metrics.
- Route and protocol drift is more likely after Milestone 15's additions.

### Manual inventory plus focused drift tests (chosen)

Pros:

- Matches the current small HTTP/JSON API.
- Keeps `curl`-friendly, standard-library-driven development.
- Creates a reviewable compatibility reference without premature generated
  tooling.
- Gives future OpenAPI/protobuf decisions a stable baseline.

Cons:

- Manual docs can drift unless maintained with behavior changes.
- It is less strict than generated schema validation.

## Consequences

Positive:

- Operators and contributors have one authoritative current API reference.
- Future API changes have a documented compatibility policy.
- Generated contract tooling remains a future decision instead of an accidental
  commitment.
- Tests can focus on preventing route, protocol, JSON-field, and metrics drift.

Negative:

- The API inventory must be updated manually when public behavior changes.
- External clients still do not get generated clients or schema validation.

Out of scope:

- Runtime feature changes.
- Protocol version changes.
- Postgres schema changes.
- OpenAPI/protobuf/gRPC or other generated contract tooling.
