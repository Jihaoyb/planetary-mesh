# Planetary Mesh Roadmap

This document is the canonical roadmap for Planetary Mesh after Milestone 9
Postgres schema migration readiness.

## Current Stage

- Baseline: `main` after PR #13 (`07088d8`)
- Stage: command-capable control-plane prototype with durable coordinator state, lightweight Postgres schema readiness metadata, opt-in coordinator-agent mTLS, an operator CLI, file-based local config, repeatable local smoke workflows, and opt-in durable Postgres verification
- Current capabilities:
  - thin `cmd/agent` and `cmd/coordinator` entrypoints
  - reusable logic in `internal/agent` and `internal/coordinator`
  - HTTP/JSON control plane with protocol versioning, job detail, and metrics
  - allowlisted direct command execution with bounded stdout/stderr capture
  - optional Postgres persistence for nodes and jobs
  - optional mTLS between coordinator and agents with node allowlisting
  - `pmctl` for status, node/job listing, job inspection, and command job submission
  - env-style local config files for coordinator, agents, and `pmctl`
  - config-driven local smoke demo for one coordinator, two agents, and `pmctl`
  - opt-in Postgres smoke workflow for durable-state and restart-recovery verification
  - Postgres schema readiness metadata exposed through `/status`, `/metrics`,
    logs, tests, and the Postgres smoke workflow
  - startup recovery metric for persisted `RUNNING` jobs failed during coordinator startup
  - structured logging, graceful shutdown, retry/backoff, and E2E/failure tests

Milestones 1 through 9 are complete.

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

Implemented changes:

- Add a thin CLI under `cmd/pmctl`
- Support:
  - submit command job
  - list nodes
  - list jobs
  - inspect job
  - show coordinator status/config
- Keep the CLI as a pure client over coordinator APIs

Status: complete

Acceptance criteria:

- Operators can submit command jobs, list nodes/jobs, inspect a job, and view
  non-secret coordinator status/config without writing `curl` requests
- Plain local development works by default
- Secure coordinator access works with CA/cert/key configuration
- CLI tests cover command behavior and an in-process coordinator smoke flow

## Milestone 6: Config and Install Ergonomics

Goal: improve local operator ergonomics without changing env-based runtime
behavior.

Implemented changes:

- Add optional env-style config files for coordinator, agent, and `pmctl`
- Keep existing environment variables working
- Add `--config <path>` and per-binary config path env vars
- Define precedence as defaults, config file, non-empty environment variables,
  then CLI flags where supported
- Add tracked config examples for a coordinator, two local agents, and `pmctl`
- Document `go install ./cmd/pmctl` so operators can run `pmctl` directly

Status: complete

Acceptance criteria:

- Existing env-only coordinator, agent, and `pmctl` runs keep working
- Local two-agent development can be run from example config files
- Config parsing, precedence, and runtime wiring are test-covered
- Default `go test ./...` remains free of external services

## Milestone 7: Local Smoke and Release Workflow

Goal: make the current local operator workflow repeatable and easy to verify
end to end from a fresh checkout.

Implementation changes:

- Update `examples/demo.sh` into the canonical local smoke workflow
- Start one coordinator and two agents from tracked env-style config examples
- Use `pmctl` for coordinator status, node listing, command submission, job
  inspection, and job listing
- Keep the smoke workflow plain-HTTP and in-memory by default so it requires no
  external services
- Align `compose.yaml` with a coordinator + Postgres + two-agent demo stack
- Add lightweight tests that keep tracked config examples and demo script syntax
  from drifting

Status: complete

Acceptance criteria:

- `./examples/demo.sh` proves coordinator, two agents, config files, command
  execution, and `pmctl` work together locally
- Compose demo wiring includes two distinct agents without changing Postgres
  storage behavior
- Existing env var behavior, config precedence, command execution, storage,
  mTLS, and `pmctl` behavior are preserved
- Default `go test ./...` remains fast and free of external services

## Milestone 8: Postgres Ops Readiness

Goal: make durable coordinator operation easier to verify and reason about
without changing the v0 control-plane model.

Implemented changes:

- Add opt-in Postgres integration coverage for schema initialization
  idempotency, job ID continuity across store reopen, and restart recovery
  preserving terminal jobs
- Add `examples/postgres_smoke.sh` as the durable-state smoke workflow
- Keep `examples/demo.sh` as the default in-memory smoke workflow
- Parameterize Compose host ports while preserving existing defaults
- Expose `planetary_jobs_recovered_on_startup_total` from `/metrics`
- Document the split between fast DB-free tests and opt-in durable Postgres
  verification

Status: complete

Acceptance criteria:

- A Postgres-backed coordinator can be started, inspected through `pmctl`,
  restarted, and used again after restart
- A persisted `RUNNING` job is marked `FAILED` on coordinator startup with the
  restart-specific error
- Operators can inspect durable smoke failures through captured Compose logs,
  `/status`, `/metrics`, and `pmctl jobs inspect`
- Existing env var/config file behavior, command execution, Postgres storage,
  mTLS, `pmctl`, config precedence, and in-memory smoke behavior are preserved
- Default `go test ./...` remains fast and free of external services

## Milestone 9: Postgres Schema Migration Readiness

Goal: make future Postgres schema changes safer and easier to reason about
without introducing a full migration framework or changing runtime behavior.

Implemented changes:

- Keep embedded Postgres schema initialization as the runtime model
- Add a `schema_version` metadata table with current schema version `1`
- Backfill missing schema metadata on existing Postgres databases
- Reject databases marked with a newer schema version than the coordinator
  binary expects
- Expose schema readiness through startup logs, `/status`, `/metrics`, opt-in
  Postgres tests, and the Postgres smoke workflow
- Keep default tests DB-free and keep Postgres as the only durable persistence
  target

Status: complete

Acceptance criteria:

- Existing in-memory, command execution, config, mTLS, `pmctl`, local smoke, and
  Postgres restart-recovery behavior is preserved
- Operators can inspect Postgres schema readiness with `pmctl --json status` and
  `/metrics`
- A database initialized before schema metadata is backfilled without losing
  nodes, jobs, or job ID continuity
- A database with a newer recorded schema version is rejected on startup
- Default `go test ./...` remains fast and free of external services

## Later Operational Options

These are not required for v0 milestones, but may be useful once the core LAN
mesh behavior is stable:

- Evaluate hosted Postgres providers, including Supabase, for managed database
  hosting, visual table inspection, SQL editing, and operational visibility.
- Keep the coordinator database integration provider-neutral through
  `COORDINATOR_DATABASE_URL`; do not depend on Supabase-specific APIs unless a
  future product requirement needs them.
- Revisit a full schema migration framework once existing deployments need
  multi-version database upgrades beyond the lightweight readiness marker.

## Delivery Model

- One PR per milestone
- README stays concise and links here
- `go build ./...`, `go test ./...`, and `gofmt` are the baseline gate for
  every milestone
