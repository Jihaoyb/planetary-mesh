# Planetary Mesh Roadmap

This document is the canonical roadmap for Planetary Mesh after Milestone 4
trusted LAN security.

## Current Stage

- Baseline: `main` after PR #8 (`6eb22f6`) plus the Milestone 4 security branch
- Stage: command-capable control-plane prototype with durable coordinator state and opt-in coordinator-agent mTLS
- Current capabilities:
  - thin `cmd/agent` and `cmd/coordinator` entrypoints
  - reusable logic in `internal/agent` and `internal/coordinator`
  - HTTP/JSON control plane with protocol versioning, job detail, and metrics
  - allowlisted direct command execution with bounded stdout/stderr capture
  - optional Postgres persistence for nodes and jobs
  - optional mTLS between coordinator and agents with node allowlisting
  - structured logging, graceful shutdown, retry/backoff, and E2E/failure tests

Milestones 1 through 4 are complete. The remaining v0 implementation milestone
is the operator CLI.

## Milestone 2: Real Command Execution

Goal: replace the stub `/execute` path with a real, safe, direct-process workload.

Implemented in PR #7:

- `POST /jobs` supports `type="command"`, `command`, and optional `args`
- `payload` is rejected for `type="command"` instead of being silently ignored
- `GET /jobs` and `GET /jobs/{id}` expose execution detail fields:
  - `attempts`
  - `started_at`
  - `completed_at`
  - `exit_code`
  - `stdout`
  - `stderr`
  - `stdout_truncated`
  - `stderr_truncated`
  - `last_error`
- All control-plane HTTP requests require `X-Planetary-Protocol-Version: 1`
- Agent executes only allowlisted commands using `exec.CommandContext`
- Fixed agent-configured timeout, default `30s`
- Stdout and stderr are capped at `1 MiB` each and marked as truncated when clipped
- Add `examples/demo.sh` as the living smoke demo

Status: complete

Acceptance criteria:

- A real allowlisted command can be submitted and completed end to end
- Success, non-zero exit, timeout, allowlist rejection, truncation, and
  protocol mismatch are all test-covered

## Milestone 3: Durable Coordinator State

Goal: preserve node/job history across coordinator restarts.

Implementation changes:

- Introduce narrow storage interfaces aligned to coordinator needs
- Keep in-memory implementations for fast unit tests
- Add a Postgres-backed implementation for runtime and integration tests
- Persist nodes and jobs only; task fanout is still out of scope
- Add Compose support for coordinator + Postgres + agent
- On startup, any persisted `RUNNING` jobs are marked `FAILED` with a
  restart-specific error

Status: complete

Known v0 limitation:

- If an agent completed a job before a crash but the coordinator did not record
  the result, that result is lost. There is no best-effort agent reconciliation
  in v0.

## Milestone 4: Trusted LAN Security

Goal: secure coordinator-agent communication and node admission.

Implemented changes:

- Add mTLS between coordinator and agents
- Add CA/cert/key/allowlist configuration
- Enforce node allowlisting during registration
- Extend node inspection with certificate identity metadata
- Keep manual certificate distribution for v0

Status: complete

Acceptance criteria:

- Coordinator-agent registration and dispatch can run over HTTPS with mutual
  certificate authentication
- Unauthorized nodes are rejected during registration
- Node inspection includes operator-facing certificate metadata
- Handshake success/failure and secured dispatch are test-covered without
  requiring external services

## Milestone 5: Operator CLI

Goal: make the product usable without `curl`.

Planned changes:

- Add a thin CLI under `cmd/pmctl`
- Support:
  - submit job
  - list nodes
  - list jobs
  - inspect job
  - show coordinator status/config
- Keep the CLI as a pure client over coordinator APIs

## Later Operational Options

These are not required for v0 milestones, but may be useful once the core LAN
mesh behavior is stable:

- Evaluate hosted Postgres providers, including Supabase, for managed database
  hosting, visual table inspection, SQL editing, and operational visibility.
- Keep the coordinator database integration provider-neutral through
  `COORDINATOR_DATABASE_URL`; do not depend on Supabase-specific APIs unless a
  future product requirement needs them.
- Revisit schema migrations once existing deployments need safe database
  upgrades across versions.

## Delivery Model

- One PR per milestone
- README stays concise and links here
- `go build ./...`, `go test ./...`, and `gofmt` are the baseline gate for
  every milestone
