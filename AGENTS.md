# AGENTS.md

This file gives coding agents the project-specific context and operating rules
needed to work safely in this repository.

## Project Summary

Planetary Mesh is a Go-based LAN compute mesh prototype. A coordinator accepts
jobs, tracks nodes, schedules work, and dispatches work to agent daemons. Agents
register with the coordinator, send heartbeats, and execute assigned work.

The project is intentionally incremental. Keep current behavior, planned
behavior, and future ideas separate in code and documentation.

## Canonical Context

Read these files before making architectural or milestone-level changes:

- `README.md` - concise project entrypoint and local usage
- `docs/roadmap.md` - canonical milestone plan
- `docs/architecture.md` - component model and system boundaries
- `docs/tech-choices.md` - language, protocol, storage, and runtime choices
- `docs/adr/` - accepted Architecture Decision Records

If these documents conflict, prefer the most recent ADR for decided technical
choices and `docs/roadmap.md` for milestone sequencing. If a change makes a doc
misleading, update the doc in the same PR unless the branch scope says otherwise.

## Current Milestone Model

The project uses one PR per milestone or documentation slice.

Expected sequence:

1. PR 0: docs sync and roadmap alignment
2. PR 1: Milestone 2, real command execution
3. PR 2: Milestone 3, durable coordinator state with Postgres
4. PR 3: Milestone 4, trusted LAN security with mTLS
5. PR 4: Milestone 5, operator CLI

Important boundary rules:

- Do not mix milestone implementation work into docs-only branches.
- Do not start Postgres work on the command-execution branch.
- Do not start mTLS work before the persistence milestone branch.
- Do not start CLI or dashboard work before the security milestone branch.
- Keep future-state documentation explicit about what is planned versus what is
  already implemented.

Always inspect the active branch and recent commits before assuming project
state:

```bash
git status --short --branch
git log --oneline -5
```

## Branching Rules

Use focused branches:

- Docs-only work: `docs/<short-topic>`
- Milestone work: `feature/milestone-<n>-<short-topic>`
- Bug fixes: `fix/<short-topic>`

Known project branches from the current delivery plan:

- `docs/roadmap-sync`
- `feature/milestone-2-command-execution`
- `feature/milestone-3-postgres-persistence`
- `feature/milestone-4-mtls-security`
- `feature/milestone-5-pmctl-cli`

Avoid stacking unrelated work in one branch. If work has already been mixed,
split it before opening PRs.

## Repository Layout

Current layout:

```text
planetary-mesh/
  README.md
  AGENTS.md
  go.mod

  cmd/
    coordinator/       # Coordinator service entrypoint
    agent/             # Agent daemon entrypoint

  internal/
    coordinator/       # Coordinator HTTP handlers, node/job stores, metrics, tests
    agent/             # Agent HTTP handlers, coordinator client, executor, tests
    protocol/          # Shared protocol constants and wire structs, when present

  docs/
    kickoff.md
    architecture.md
    tech-choices.md
    roadmap.md
    adr/

  examples/            # Smoke demos, when present
```

Keep reusable application logic under `internal/`. Keep `cmd/*` packages thin:
parse environment/configuration, wire dependencies, start servers, and handle
shutdown.

## Go Standards

The project is Go-first. Follow the existing standard-library-heavy style unless
there is a clear reason to add a dependency.

Before handing off Go changes, run:

```bash
gofmt -w <changed-go-files>
go build ./...
go test ./...
```

If sandboxing blocks writes to the default Go cache, pin the cache inside the
repo:

```bash
GOCACHE=$(pwd)/.gocache go build ./...
GOCACHE=$(pwd)/.gocache go test ./...
```

Do not commit `.gocache/`.

## Local Run Commands

Run the coordinator:

```bash
COORDINATOR_ADDR=:8080 go run ./cmd/coordinator
```

Run an agent in another terminal:

```bash
COORDINATOR_URL=http://localhost:8080 \
AGENT_ADDR=:8081 \
go run ./cmd/agent
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
```

After Milestone 2 lands, command execution uses an allowlist:

```bash
COORDINATOR_URL=http://localhost:8080 \
AGENT_ADDR=:8081 \
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep' \
go run ./cmd/agent
```

Milestone-specific smoke demos should live under `examples/`.

## Control Plane Rules

The v0 control plane is HTTP/JSON. ADR 0003 records this decision. Do not
introduce gRPC or protobuf runtime requirements unless a future ADR and roadmap
change explicitly call for it.

Current and planned endpoints are coordinator-owned unless noted otherwise:

- `GET /healthz`
- `POST /register`
- `GET /nodes`
- `POST /jobs`
- `GET /jobs`
- `GET /jobs/{id}`
- `GET /metrics`
- Agent `POST /execute`

Keep validation, scheduling, retry policy, and state transitions in the
coordinator. Keep agents focused on registration, heartbeat, and execution.

## Milestone 2 Rules: Command Execution

When working on Milestone 2 or branches that include it, preserve these rules:

- `POST /jobs` supports `type="command"`, `command`, and optional `args`.
- `payload` must be rejected for `type="command"` with `400 Bad Request`.
- Job responses include execution/result fields:
  - `attempts`
  - `started_at`
  - `completed_at`
  - `exit_code`
  - `stdout`
  - `stderr`
  - `stdout_truncated`
  - `stderr_truncated`
  - `last_error`
- Control-plane requests require `X-Planetary-Protocol-Version: 1` once the
  protocol-version milestone code is present.
- Missing or mismatched protocol version returns `409 Conflict`.
- Agent execution uses `exec.CommandContext`.
- Never execute through a shell.
- Submitted command names are logical allowlist keys, not arbitrary executable
  paths.
- The agent maps logical command names to executable paths through explicit
  allowlist configuration.
- The execution timeout is fixed by agent config. The default is `30s`.
- There is no per-job timeout override in Milestone 2 or Milestone 3.
- Stdout and stderr are captured separately.
- Stdout and stderr are capped at `1 MiB` per stream.
- Truncated streams must set their corresponding boolean flags.
- Non-zero command exit is a terminal execution failure and should not be
  retried by the coordinator.
- Validation errors, allowlist rejection, and protocol mismatch are terminal.
- Transport errors, coordinator request timeout, and agent `5xx` responses
  remain retryable under coordinator dispatch policy.
- Node state changes to `SUSPECT` or `OFFLINE` do not cancel an already
  in-flight execution attempt in v0.

Required Milestone 2 coverage:

- successful command execution
- non-zero exit
- execution timeout
- allowlist rejection
- stdout/stderr truncation
- protocol mismatch
- terminal versus retryable dispatch behavior

## Milestone 3 Rules: Durable State

When working on Milestone 3, preserve these rules:

- Persist nodes and jobs only.
- Do not add task fanout or a `tasks` table yet.
- Keep in-memory stores for ordinary unit tests and fast `go test ./...`.
- Add Postgres-backed storage for runtime/integration use.
- Postgres is the only persistence target in this roadmap. Do not add SQLite.
- Add Compose support for coordinator + Postgres + agent.
- On coordinator startup, mark persisted `RUNNING` jobs as `FAILED`.
- Use a restart-specific error such as:
  `coordinator restarted before result was recorded`.
- Document the known v0 gap: if an agent completed before coordinator crash and
  the result was not persisted, that result is lost.
- Do not implement agent reconciliation in v0.
- Postgres integration tests must be opt-in or separately gated so default
  `go test ./...` remains DB-free.

## Milestone 4 Rules: Trusted LAN Security

When working on Milestone 4, preserve these rules:

- Keep HTTP/JSON as the v0 control plane.
- Add mTLS between coordinator and agents.
- Add configuration for CA, certificate, key, and allowed node identities or
  fingerprints.
- Enforce node allowlisting during registration.
- Extend node inspection with certificate identity metadata needed by operators.
- Certificate distribution is manual for v0.
- Do not add automated enrollment or certificate issuance in v0.
- Keep protocol-version enforcement in place.
- Database secret-management hardening is not a separate v0 milestone.

## Milestone 5 Rules: Operator CLI

When working on Milestone 5, preserve these rules:

- Add the CLI under `cmd/pmctl`.
- Keep the CLI as a pure client of coordinator APIs.
- Do not move scheduling, validation, or state logic into the CLI.
- Support:
  - submit job
  - list nodes
  - list jobs
  - inspect job
  - show coordinator status/config
- Dashboard work remains out of scope for v0.

## Testing Expectations

Use the smallest test that proves the behavior, but broaden coverage when a
change affects cross-component contracts.

General gates:

```bash
gofmt -w <changed-go-files>
go build ./...
go test ./...
```

Milestone-specific gates:

- Docs-only branches: `git diff --check` is sufficient unless docs tooling is
  added later.
- Command execution: include agent unit tests, coordinator dispatch tests, and
  an end-to-end coordinator flow.
- Persistence: keep default unit tests DB-free and add separately gated
  Postgres integration tests.
- mTLS: cover handshake success/failure, unauthorized node rejection, and
  secured dispatch.
- CLI: include integration smoke tests against a live coordinator.

## Documentation Rules

Documentation should describe current state honestly and label future work as
planned.

When changing behavior:

- Update `README.md` if local usage or user-facing API examples change.
- Update `docs/roadmap.md` if milestone status or sequencing changes.
- Update `docs/architecture.md` if component responsibilities or boundaries
  change.
- Update `docs/tech-choices.md` if a technology choice changes.
- Add an ADR for non-trivial decisions involving protocol, storage, execution,
  security, or operational model.

Avoid rewriting docs into future-state fiction. It is better to say "planned"
than to imply unfinished behavior exists.

## Git Hygiene

- Check status before editing.
- Do not remove or overwrite user changes.
- Do not use destructive git commands unless explicitly requested.
- Do not amend commits unless explicitly requested.
- Keep commits focused and named by intent, for example:
  - `docs: sync roadmap after control-plane hardening`
  - `feat: add real command execution`
  - `test: cover command timeout handling`
- Leave local-only files alone unless asked.

Common local-only files in this repo:

- `.DS_Store`
- `.claude/`
- `.gocache/`

Do not commit these files.

## Dependency Rules

- Prefer the Go standard library.
- Add dependencies only when they clearly reduce risk or complexity.
- Explain any new dependency in the PR.
- For Postgres, use the driver selected in the milestone implementation and
  keep DB-backed tests opt-in.
- Do not introduce frontend frameworks, dashboards, or CLIs before their
  roadmap milestone.

## Security Posture

This project is moving toward trusted LAN execution. Treat command execution,
node identity, and coordinator-agent communication as security-sensitive.

Current security expectations:

- No shell execution for agent workloads.
- Explicit command allowlists for direct process execution.
- No arbitrary executable paths from job submissions.
- Bounded output capture.
- Clear distinction between terminal validation failures and retryable transport
  failures.

Future security expectations:

- mTLS between coordinator and agents.
- Node allowlisting at registration.
- Manual certificate distribution for v0.
- No public, permissionless network behavior in v0.

## Hand-Off Standard

Every implementation hand-off should include:

- what changed
- why it changed
- how it connects to the rest of the system
- behavior changes
- test-only changes
- doc-only changes
- commands run and their results
- known gaps or intentionally deferred work

For milestone PRs, include a flat file-by-file explanation. Keep the explanation
specific enough that a reviewer can trace each file to the milestone goal.
