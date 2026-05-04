# ADR 0008: Add a thin operator CLI over the coordinator API

- Status: Accepted
- Date: 2026-05-02

## Context

After Milestone 4, Planetary Mesh can run command jobs with optional Postgres
persistence and opt-in coordinator-agent mTLS, but operators still need raw
`curl` commands to submit jobs and inspect state.

Milestone 5 needs a usable operator interface without changing the v0
HTTP/JSON control plane or moving coordinator-owned behavior into a client.

## Decision

Add `pmctl` under `cmd/pmctl` as a thin Go client for the coordinator API.

Details:

- `pmctl` supports coordinator status, node listing, job listing, job
  inspection, and command job submission.
- `pmctl` sends `X-Planetary-Protocol-Version: 1` on coordinator API requests.
- Plain local development defaults to `http://localhost:8080`.
- Secure coordinator access uses manually configured CA, client certificate,
  and client key files.
- `pmctl` does not generate, enroll, rotate, or allowlist certificates.
- Coordinator validation, scheduling, retry policy, state transitions, and
  storage remain coordinator responsibilities.
- Add versioned `GET /status` for non-secret coordinator runtime status/config.

## Alternatives Considered

- **Keep curl-only operations**
  - Pros: no new binary or command surface.
  - Cons: poor operator experience and easy-to-miss protocol headers.

- **Put operational logic in the CLI**
  - Pros: might reduce coordinator endpoint changes.
  - Cons: would duplicate validation/state behavior and violate the v0 control
    plane boundary.

- **Thin CLI over coordinator APIs (chosen)**
  - Pros: improves usability while preserving coordinator-owned behavior and
    keeping tests fast with in-process HTTP handlers.
  - Cons: adds another client surface that must track HTTP/JSON wire shape.

## Consequences

- Positive:
  - Operators can use `pmctl` instead of hand-written `curl` for common flows.
  - The CLI can access secure coordinators with the same manual certificate
    distribution model as the rest of v0.
  - `GET /status` gives operators non-secret runtime context without exposing
    DSNs, certificate paths, or allowlist values.
- Negative:
  - Human-readable CLI output is another compatibility surface.
  - Full dashboard workflows remain out of scope for v0.
