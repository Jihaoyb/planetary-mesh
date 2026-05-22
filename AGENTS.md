# AGENTS.md

This file gives coding agents the project-specific context and operating rules
needed to work safely in this repository.

## Project Summary

Planetary Mesh is a Go-based private-first compute mesh prototype. A single
coordinator accepts jobs, tracks nodes, schedules work, and dispatches work to
agent daemons. Agents register with the coordinator, send heartbeats, and execute
assigned allowlisted command jobs. `pmctl` is a thin operator CLI over the
coordinator HTTP/JSON API.

The product direction is private/local mesh first, remote private mesh later,
trusted shared compute pool later, and overflow compute marketplace only as
long-term exploration. Do not imply that marketplace, payment, public-node, GPU
pooling, storage pooling, or bandwidth pooling features exist today.

The project is intentionally incremental. Keep current behavior, planned
behavior, and future ideas separate in code and documentation.

## Current Baseline

Current `main` is after Milestone 15 runtime agent reconciliation/result
reporting.

Milestones 1 through 15 are complete:

- initial docs/process alignment
- HTTP/JSON coordinator/agent control plane
- protocol version header enforcement
- node registration, heartbeat, and health states
- command job submission and allowlisted direct execution
- optional Postgres persistence for nodes/jobs
- opt-in coordinator-agent mTLS and node allowlists
- thin `pmctl` operator CLI
- env-style local config files
- local and Postgres smoke workflows
- queued-job scheduler/re-dispatch loop
- cross-node reassignment after retryable dispatch failure
- simple node capability/load reporting for operators
- explicit coordinator-owned job lifecycle transitions
- accepted strategy for future agent reconciliation/result reporting after
  coordinator restart
- runtime implementation of additive agent terminal result reporting and
  bounded Postgres reconciliation grace after coordinator restart
- Postgres schema readiness metadata version `2`

Runtime agent reconciliation is implemented as a narrow best-effort slice:
agents keep only bounded in-memory terminal result history, and Postgres startup
uses a bounded grace window before failing unreconciled startup-running jobs.
The next phase should focus on narrow private mesh hardening such as operator
clarity, API readiness, security hardening, and packaging. Do not jump to
marketplace, payment, dashboard, public-node, or remote-node product work
without explicit planning and an accepted direction.

## Canonical Context

Read these files before making architectural, roadmap, or product-direction
changes:

- `README.md` - concise project entrypoint and local usage
- `docs/product-positioning.md` - current product framing and staged direction
- `docs/current-limitations.md` - current limitations and risk register
- `docs/roadmap.md` - canonical roadmap and sequencing
- `docs/architecture.md` - component model and system boundaries
- `docs/tech-choices.md` - accepted language, protocol, storage, runtime, and
  execution choices
- `docs/adr/` - accepted Architecture Decision Records

`docs/kickoff.md` is historical context only. Do not treat it as current product
or architecture truth if it conflicts with the docs above.

If docs conflict, prefer the most recent ADR for decided technical choices,
`docs/roadmap.md` for sequencing, and `docs/product-positioning.md` for product
framing. If a change makes a doc misleading, update that doc in the same PR
unless the branch scope says otherwise.

Always inspect the active branch and recent commits before assuming project
state:

```bash
git status --short --branch
git log --oneline -5
```

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
    pmctl/             # Operator CLI entrypoint

  internal/
    coordinator/       # Coordinator HTTP handlers, dispatch, stores, metrics, tests
    agent/             # Agent HTTP handlers, coordinator client, executor, tests
    pmctl/             # CLI command parsing, output, and coordinator client
    protocol/          # Shared protocol constants and wire structs
    security/          # TLS, certificate identity, and allowlist helpers
    configfile/        # Env-style config file parser

  config/              # Tracked example env-style config files
  docs/                # Roadmap, architecture, product docs, ADRs
  examples/            # Local and Postgres smoke demos
  compose.yaml         # Local coordinator + Postgres + agents demo
```

Keep reusable application logic under `internal/`. Keep `cmd/*` packages thin:
parse environment/configuration, wire dependencies, start servers, and handle
shutdown.

## Branching Rules

Use focused branches:

- Docs-only work: `docs/<short-topic>`
- Milestone work: `feature/milestone-<n>-<short-topic>`
- Bug fixes: `fix/<short-topic>`

Avoid stacking unrelated work in one branch. If work has already been mixed,
split it before opening PRs.

## Go Standards

The project is Go-first. Go is an accepted current implementation choice, not a
tentative preference. Follow the existing standard-library-heavy style unless
there is a clear reason to add a dependency.

Before handing off Go changes, run:

```bash
gofmt -w <changed-go-files>
go build ./...
go test ./...
go vet ./...
```

If sandboxing blocks writes to the default Go cache, pin the cache outside the
repo:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
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
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep' \
go run ./cmd/agent
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
```

Run the default local smoke demo:

```bash
./examples/demo.sh
```

Run the opt-in Postgres smoke demo:

```bash
./examples/postgres_smoke.sh
```

## Control Plane Rules

The v0 control plane is HTTP/JSON. ADR 0003 records this decision. Do not
introduce gRPC, protobuf, generated API contracts, or a second runtime protocol
unless a future ADR and roadmap change explicitly call for it.

Current coordinator endpoints:

- `GET /healthz`
- `GET /status`
- `POST /register`
- `GET /nodes`
- `POST /jobs`
- `GET /jobs`
- `GET /jobs/{id}`
- `POST /jobs/{id}/result`
- `GET /metrics`

Current agent endpoints:

- `GET /healthz`
- `POST /execute`

All coordinator control-plane endpoints except `/healthz` require
`X-Planetary-Protocol-Version: 1`. Agent `/execute` also requires the protocol
header. Missing or mismatched protocol version returns `409 Conflict`.

Keep validation, scheduling, retry policy, and state transitions in the
coordinator. Keep agents focused on registration, heartbeat, and execution. Keep
`pmctl` as a pure client of coordinator APIs.

## Command Execution Rules

Command execution is security-sensitive. Preserve these rules:

- `POST /jobs` supports `type="command"`, `command`, and optional `args`.
- `payload` is rejected for `type="command"` with `400 Bad Request`.
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
- Agent execution uses `exec.CommandContext`.
- Never execute through a shell.
- Submitted command names are logical allowlist keys, not arbitrary executable
  paths.
- The agent maps logical command names to executable paths through explicit
  allowlist configuration.
- The execution timeout is fixed by agent config. The default is `30s`.
- There is no per-job timeout override today.
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

Do not describe this model as strong sandboxing. It is allowlisted direct
process execution with bounded output and a fixed timeout.

## Storage Rules

Preserve these rules:

- Persist nodes and jobs only.
- Do not add task fanout or a `tasks` table without explicit planning.
- Keep in-memory stores for ordinary unit tests and fast `go test ./...`.
- Keep default `go test ./...` DB-free.
- Postgres is the only durable persistence target in the current roadmap. Do
  not add SQLite or another persistence backend without an ADR.
- Postgres is enabled only when `COORDINATOR_DATABASE_URL` is configured.
- On Postgres-backed coordinator startup, persisted `RUNNING` jobs are captured
  for a bounded reconciliation grace window. Matching agent reports can complete
  or fail those jobs during grace; unreconciled captured jobs are marked
  `FAILED` with: `coordinator restarted before result was recorded`.
- In-memory coordinator restart still loses state. Agents keep only bounded
  in-memory terminal result history, so agent restart can still lose a result.
- ADR 0013 records the accepted reconciliation strategy: explicit
  agent-to-coordinator result reporting plus a Postgres reconciliation grace
  window before failing persisted `RUNNING` jobs.
- Postgres integration tests must be opt-in or separately gated.
- Schema readiness metadata version `2` is current. It is not a full migration
  framework.

## Security Rules

Planetary Mesh supports opt-in mTLS and node allowlists today. Plain HTTP
remains available for local development unless configuration changes.

Preserve these rules:

- Keep HTTP/JSON as the v0 control plane.
- mTLS requires manual CA/certificate/key configuration.
- If coordinator TLS is configured, allowed node identities or fingerprints are
  required.
- Enforce node allowlisting during registration in secure mode.
- Node inspection includes certificate identity metadata when mTLS is enabled.
- Certificate generation, distribution, enrollment, and rotation are manual.
- Do not add automated enrollment or certificate issuance unless explicitly
  planned.
- Keep protocol-version enforcement in place.
- Treat direct command execution, node identity, and coordinator-agent
  communication as security-sensitive.

## Product and Scope Guardrails

Do not implement or imply current support for:

- marketplace, payment, payout, dispute, reputation, or transaction-fee systems
- public, permissionless, or arbitrary third-party compute nodes
- shared compute pools without admin approval/trust design
- GPU, storage, or bandwidth marketplace features
- strong sandbox/container isolation unless actually implemented
- Kubernetes, Ray, Airflow, or Temporal replacement behavior
- dashboard or frontend work unless explicitly requested
- multi-tenant authorization or public cloud platform behavior
- remote private mesh networking without explicit planning

Allowed near-term direction is private mesh hardening:

- queued-job scheduler/re-dispatch loop
- cross-node reassignment after dispatch failure
- scheduler policy for reported node capabilities/load
- clearer job state transitions
- follow-up reconciliation hardening if the current best-effort slice proves
  insufficient
- operator runbooks and API inventory
- install/release packaging
- certificate/onboarding helper planning
- optional private batch/AI demo pipeline

## Testing Expectations

Use the smallest test that proves the behavior, but broaden coverage when a
change affects cross-component contracts.

General gates:

```bash
gofmt -l .
git diff --check
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
```

Opt-in durable storage gate:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-postgres go test -tags postgres ./internal/coordinator
```

Docs-only branches should at least run `git diff --check`; running the full
build/test/vet suite is preferred when feasible.

## Documentation Rules

Documentation should describe current state honestly and label future work as
planned.

When changing behavior:

- Update `README.md` if local usage or user-facing API examples change.
- Update `docs/roadmap.md` if milestone status or sequencing changes.
- Update `docs/architecture.md` if component responsibilities or boundaries
  change.
- Update `docs/tech-choices.md` if a technology choice changes.
- Update `docs/product-positioning.md` if product framing changes.
- Update `docs/current-limitations.md` if a limitation is fixed or a new risk is
  introduced.
- Add an ADR for non-trivial decisions involving protocol, storage, execution,
  security, operations, or product architecture.

Avoid future-state fiction. It is better to say "planned" than to imply
unfinished behavior exists.

## Git Hygiene

- Check status before editing.
- Do not remove or overwrite user changes.
- Do not use destructive git commands unless explicitly requested.
- Do not amend commits unless explicitly requested.
- Keep commits focused and named by intent, for example:
  - `docs: align product direction with private mesh`
  - `feat: add queued job scheduler`
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
- For Postgres, keep provider-neutral configuration through
  `COORDINATOR_DATABASE_URL`.
- Do not introduce frontend frameworks, dashboard dependencies, marketplace
  dependencies, payment dependencies, or auth systems without explicit planning.

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

For milestone PRs, include a flat file-by-file explanation specific enough that
a reviewer can trace each file to the milestone goal.
