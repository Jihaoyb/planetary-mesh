# Roadmap

This is the canonical roadmap for Planetary Mesh. It separates the current
implemented baseline from future product direction.

Planetary Mesh starts as a private/local compute mesh: users run jobs across
machines they already own or control. Remote private mesh, trusted shared compute
pools, and overflow compute marketplace features are later stages, not current
capabilities.

## Current Baseline

- Baseline: `main` after Milestone 16 HTTP/JSON v0 API inventory and
  compatibility policy
- Stage: Go 1.25.4 LAN/private-network command-job prototype
- Positioning: lightweight private compute mesh for running command-based jobs
  across machines you own or control, with a future path toward trusted overflow
  compute

Implemented capability:

- `cmd/coordinator`, `cmd/agent`, and `cmd/pmctl`
- HTTP/JSON coordinator/agent control plane
- `X-Planetary-Protocol-Version: 1` protocol header
- node registration and heartbeat
- node health states: `HEALTHY`, `SUSPECT`, `OFFLINE`
- node capabilities and active execution count reported through
  registration/heartbeat, `GET /nodes`, and `pmctl nodes list`
- command job submission with `type="command"`, `command`, and optional `args`
- explicit coordinator-owned job lifecycle transitions for `QUEUED`,
  `RUNNING`, `COMPLETED`, and `FAILED`
- first-healthy-node initial dispatch at job submission time
- cross-node reassignment after retryable dispatch failures
- periodic queued-job scheduler/re-dispatch loop
- queued jobs expire as `FAILED` after 24 hours
- allowlisted direct command execution using `exec.CommandContext`
- no shell execution
- bounded stdout/stderr capture with truncation flags
- retry handling for retryable dispatch failures
- additive agent terminal result reporting through `POST /jobs/{id}/result`
- optional Postgres persistence for nodes/jobs and node metadata
- Postgres schema readiness metadata version `2`
- bounded Postgres reconciliation grace for persisted startup `RUNNING` jobs
- best-effort agent result reporting from bounded in-memory cache
- opt-in mTLS and node allowlists with manual certificate lifecycle
- coordinator `/status` and `/metrics`
- manual HTTP/JSON v0 API inventory and compatibility policy
- DB-free API drift tests for route, protocol, JSON-field, and metrics
  expectations
- env-style config files
- local in-memory smoke script
- Postgres durability smoke script
- thin CLI for status, node/job listing, job inspection, and command submission,
  including human and JSON node metadata output
- CI/build/test health with default DB-free tests

Current limitations are tracked in
[current-limitations.md](current-limitations.md).

## Completed Baseline / Milestones 1-16

The first sixteen milestones established a working private LAN/trusted-network
prototype. This history remains useful because it explains why the current
baseline is intentionally narrow.

### Milestone 1: Control-Plane Foundation

Goal: establish the first coordinator/agent shape and basic docs.

Completed outcomes:

- Go coordinator and agent services
- HTTP/JSON control plane decision
- initial node/job model
- in-memory state for fast local development
- lightweight process, architecture, and ADR documentation

Status: complete

### Milestone 2: Real Command Execution

Goal: replace the stub `/execute` path with a real, constrained workload.

Completed outcomes:

- `POST /jobs` supports `type="command"`, `command`, and optional `args`
- command jobs reject `payload`
- job responses include attempts, timestamps, exit code, stdout, stderr,
  truncation flags, and last error
- all control-plane requests use `X-Planetary-Protocol-Version: 1`
- agent executes only allowlisted commands with `exec.CommandContext`
- no shell execution
- fixed agent timeout, default `30s`
- stdout and stderr capped at `1 MiB` each
- success, non-zero exit, timeout, allowlist rejection, truncation, protocol
  mismatch, and retry/terminal failure behavior are test-covered
- `examples/demo.sh` became the living local smoke demo

Status: complete

### Milestone 3: Durable Coordinator State

Goal: preserve node/job history across coordinator restarts.

Completed outcomes:

- narrow node/job storage interfaces
- in-memory stores retained for default tests and local fallback
- optional Postgres store for runtime/integration use
- nodes and jobs persisted; task fanout remains out of scope
- Compose support for coordinator + Postgres + agent
- persisted `RUNNING` jobs marked `FAILED` on coordinator startup with
  `coordinator restarted before result was recorded`

Known limitation:

- This milestone intentionally used immediate startup failure for persisted
  `RUNNING` jobs. Milestone 15 later narrowed that lost-result gap with
  best-effort result reporting and bounded Postgres reconciliation grace.

Status: complete

### Milestone 4: Trusted LAN Security

Goal: support authenticated coordinator-agent communication and explicit node
admission for trusted LAN operation.

Completed outcomes:

- opt-in mTLS between coordinator and agents
- CA/cert/key configuration
- node allowlisting by certificate identity or SHA-256 fingerprint
- registration rejects unauthorized nodes in secure mode
- node inspection includes certificate identity metadata
- manual certificate distribution for v0
- handshake success/failure, unauthorized node rejection, and secured dispatch
  are test-covered

Important current-state note:

- mTLS is supported but not the default. Plain HTTP remains available for local
  development unless secure configuration is supplied.

Status: complete

### Milestone 5: Operator CLI

Goal: make common operator workflows possible without hand-written `curl`.

Completed outcomes:

- thin CLI under `cmd/pmctl`
- coordinator status/config inspection
- node listing
- job listing
- job inspection
- command job submission
- secure coordinator access with manually configured CA/cert/key files
- in-process coordinator smoke coverage

Status: complete

### Milestone 6: Config and Install Ergonomics

Goal: improve local operator ergonomics while preserving env-based runtime
configuration.

Completed outcomes:

- optional env-style config files for coordinator, agent, and `pmctl`
- existing environment variables continue to work
- `--config <path>` support
- per-binary config path env vars
- precedence: defaults, config file, non-empty env vars, CLI flags where
  supported
- tracked config examples for one coordinator, two agents, and `pmctl`
- documented `go install ./cmd/pmctl`

Status: complete

### Milestone 7: Local Smoke and Release Workflow

Goal: make the current local workflow repeatable from a fresh checkout.

Completed outcomes:

- `examples/demo.sh` runs coordinator, two agents, config files, command
  execution, and `pmctl` end to end
- smoke workflow uses plain HTTP and in-memory storage by default
- `compose.yaml` runs coordinator + Postgres + two agents
- tests protect tracked config examples and demo script syntax from drift

Status: complete

### Milestone 8: Postgres Ops Readiness

Goal: make durable coordinator operation easier to verify.

Completed outcomes:

- opt-in Postgres integration coverage for schema initialization idempotency,
  job ID continuity, and restart recovery
- `examples/postgres_smoke.sh` durable-state workflow
- default in-memory smoke workflow preserved
- Compose host ports can be overridden
- `planetary_jobs_recovered_on_startup_total` exposed from `/metrics`
- documented split between DB-free default tests and opt-in durable Postgres
  verification

Status: complete

### Milestone 9: Postgres Schema Migration Readiness

Goal: make future Postgres schema changes safer without introducing a full
migration framework or changing runtime behavior.

Completed outcomes:

- embedded Postgres schema initialization remains the runtime model
- `schema_version` metadata table added
- current schema version recorded as `1`
- missing metadata backfilled on existing Postgres databases
- databases with newer schema versions rejected at startup
- schema readiness exposed through startup logs, `/status`, `/metrics`,
  opt-in Postgres tests, `pmctl --json status`, and the Postgres smoke workflow
- default tests remain DB-free

Status: complete

### Milestone 10: Queued Job Scheduler

Goal: make jobs submitted while no healthy node exists run later once capacity
appears.

Completed outcomes:

- coordinator-owned periodic scheduler lists jobs still in `QUEUED` state
- queued jobs are re-dispatched when at least one `HEALTHY` node exists
- queued jobs are marked `FAILED` after 24 hours without a healthy node
- duplicate concurrent dispatch of the same job is skipped within one running
  coordinator process
- HTTP/JSON API shape and protocol version behavior unchanged
- in-memory default tests remain DB-free
- optional Postgres still persists nodes/jobs only, with no schema version bump
- docs now describe scheduler behavior and remaining scheduling limits

Status: complete

### Milestone 11: Cross-Node Reassignment

Goal: let a command job try another healthy node when the selected node has
retryable dispatch failures.

Completed outcomes:

- coordinator still selects the first `HEALTHY` node for the initial dispatch
- retryable failures exhaust the selected node's configured attempts before
  moving to another `HEALTHY` node
- terminal failures still stop dispatch immediately
- jobs fail with the last retryable error when all eligible healthy nodes fail
- duplicate concurrent dispatch protection still covers the whole dispatch
  cycle in one coordinator process
- HTTP/JSON API shape, protocol version behavior, command execution rules,
  in-memory tests, and optional Postgres schema version `1` are unchanged
- docs now describe cross-node reassignment and remaining scheduler limits

Status: complete

### Milestone 12: Node Capability and Load Visibility

Goal: make node information more useful for private mesh operators without
changing scheduler scoring or dispatch priority.

Completed outcomes:

- agents can report optional static capabilities through `AGENT_CAPABILITIES`
- agents report approximate active command execution count on registration and
  heartbeat
- coordinator validates and stores capabilities plus active execution count
- older agents that omit the new fields remain compatible and default to empty
  capabilities plus zero active executions
- `GET /nodes`, registration responses, `pmctl nodes list`, and
  `pmctl --json nodes list` expose the new node metadata
- optional Postgres persists node metadata in the `nodes` table
- Postgres schema readiness metadata advanced to version `2`
- first-healthy-node dispatch, cross-node retry, queued scheduling, protocol
  version behavior, mTLS allowlisting, and command execution rules are unchanged

Status: complete

### Milestone 13: Explicit Job Lifecycle State Transitions

Goal: make job lifecycle/state transitions explicit, documented, and
test-covered for private mesh reliability and API/operator readiness.

Completed outcomes:

- documented the current coordinator-owned job lifecycle state model
- kept public job JSON fields and status strings unchanged
- kept active states to `QUEUED` and `RUNNING`, terminal states to `COMPLETED`
  and `FAILED`
- kept `CANCELLED` reserved/unsupported; no cancellation API or behavior was
  added
- enforced lifecycle transitions through in-memory and Postgres job stores
- prevented terminal job rows from being overwritten by later lifecycle methods
- preserved no-healthy-node queue retention, queued expiration, restart
  recovery, duplicate dispatch protection, retryable cross-node reassignment,
  and terminal dispatch failure behavior
- kept Postgres schema readiness metadata at version `2`
- added focused default and opt-in Postgres tests for lifecycle transitions

Status: complete

### Milestone 14: Agent Reconciliation Strategy

Goal: decide the agent reconciliation strategy after coordinator restart for
private mesh reliability and API/operator readiness.

Completed outcomes:

- documented current restart/recovery behavior and the lost-result gap
- chose a strategy/ADR-only milestone with no runtime behavior changes
- accepted explicit agent-to-coordinator result reporting as the future
  reconciliation direction instead of heartbeat-carried reports
- kept HTTP/JSON v0 and `X-Planetary-Protocol-Version: 1` for the future
  additive result-reporting path
- preserved public job JSON fields, job status strings, scheduler behavior,
  command execution rules, mTLS behavior, node allowlisting, and `pmctl`
  behavior
- preserved terminal `COMPLETED` and `FAILED` immutability
- preserved nodes/jobs-only storage and Postgres schema readiness metadata
  version `2`
- defined future compatibility expectations for older agents and older
  coordinators
- defined future edge-case policy for duplicate, late, wrong-node, unknown-job,
  unsupported-state, and concurrent result reports

Status: complete

### Milestone 15: Runtime Agent Reconciliation and Result Reporting

Goal: implement the first narrow runtime slice of ADR 0013 for private mesh
restart reliability and operator/API readiness.

Completed outcomes:

- additive coordinator `POST /jobs/{id}/result` endpoint for agent terminal
  result reports
- terminal report acceptance only for existing `RUNNING` jobs whose current
  `node_id` matches the reporting node
- duplicate, late, wrong-node, unknown-job, unsupported-status, and
  unsupported-state reports do not mutate terminal or non-matching jobs
- mTLS/node allowlist enforcement applies to result reports in secure mode
- agents keep a bounded in-memory terminal result cache and report best-effort
  while synchronous `/execute` remains primary
- Postgres startup captures persisted `RUNNING` IDs, serves during a bounded
  reconciliation grace window, and fails only remaining captured IDs after grace
- in-memory restart still loses state; agent restart still loses cached results
- public job JSON fields, status strings, protocol version, scheduler behavior,
  nodes/jobs-only storage, and Postgres schema version `2` are unchanged
- `/status`, `/metrics`, `pmctl status`, tests, and Postgres smoke workflow now
  expose or verify reconciliation behavior

Status: complete

### Milestone 16: HTTP/JSON v0 API Inventory and Compatibility Contract

Goal: create an authoritative, reviewable HTTP/JSON v0 API inventory and
compatibility policy for the current coordinator, agent, protocol, metrics, and
operator-facing client surfaces.

Completed outcomes:

- manual API inventory at `docs/api-http-json-v0.md`
- ADR 0014 records the decision to maintain a manual v0 API inventory before
  generated OpenAPI/protobuf
- documented coordinator operator/client-facing endpoints, coordinator
  agent-facing endpoints, agent endpoints, public JSON fields, metrics, status
  codes, and mTLS/node identity expectations
- documented compatibility policy for protocol version `1`, unversioned health
  endpoints, additive JSON fields, breaking-field changes, status/lifecycle
  changes, endpoint additions, text error responses, metrics, and `pmctl`
  boundaries
- added focused default DB-free tests for route/protocol expectations,
  public JSON field names, and metric names/types
- runtime endpoint behavior, public JSON fields, status strings, protocol
  version, scheduler behavior, mTLS behavior, nodes/jobs-only storage, and
  Postgres schema version `2` are unchanged

Status: complete

## Phase 1: Private Mesh Hardening

Goal: make the current local/trusted mesh more reliable and easier to operate.

Potential work:

- scheduler policy that can use reported node capabilities/load when explicitly
  planned
- follow-up reconciliation hardening only where the current best-effort slice
  proves insufficient
- better operator runbooks
- future generated API contract decision after the manual inventory stabilizes
- improved local install workflow
- certificate/onboarding helper planning
- stronger docs around execution risks

Non-goals:

- marketplace features
- payment systems
- public-node onboarding
- remote private mesh networking
- dashboard unless explicitly scoped as operator UX work

## Phase 2: Productized Private Mesh

Goal: make the private mesh usable by a real developer or small team.

Potential work:

- richer CLI/operator UX or dashboard
- API keys or another user-facing auth model
- job templates for repeatable private workloads
- file upload/result download if needed by selected workflows
- persistent job history improvements
- logs UX
- packaged release or production image
- demo pipeline such as OCR, transcription, embeddings, image conversion, or
  developer batch jobs
- private deployment runbooks

Non-goals:

- public marketplace
- arbitrary untrusted compute
- payment/payout systems
- production multi-tenant cloud platform behavior

## Phase 3: Remote Private Mesh

Goal: support trusted machines outside the LAN while still owned or controlled
by the same user/team.

Potential work:

- secure remote node registration
- authenticated remote communication
- TLS/cert lifecycle tooling
- node identity model suitable for remote nodes
- operator-facing access control
- network failure handling
- remote health checks
- NAT/firewall deployment guidance

This phase is still private-first. It is not a public marketplace.

## Phase 4: Trusted Shared Compute Pool

Goal: allow approved users, devices, contractors, labs, teams, or partner
machines to contribute compute in a controlled group.

Long-term exploration only. This phase should not start until the private mesh
and remote private mesh are mature.

Potential work:

- admin-approved nodes
- trust levels
- usage accounting
- quotas/credits
- approved workload templates
- internal chargeback reports
- stronger operator auditing
- tighter isolation requirements

This is a controlled bridge between private mesh and possible overflow compute,
not an open public network.

## Phase 5: Overflow Compute Marketplace

Goal: explore verified external capacity only after the private mesh and trusted
shared pool are mature.

Long-term exploration only. Marketplace functionality is not part of the current
prototype and should not be implied as implemented.

Potential work:

- provider onboarding
- hardware and runtime benchmarking
- pricing and metering
- payouts and platform fee
- reputation/uptime scoring
- disputes/refunds
- stronger sandboxing and tenant isolation
- strict acceptable-use controls
- abuse prevention
- provider/buyer trust and compliance model

The long-term framing is trusted overflow compute: when private resources are
not enough, users may rent verified external capacity. It is not an early
open arbitrary-code compute marketplace.

## Later Operational Options

These options may be useful once the private mesh is stable:

- Evaluate managed Postgres providers for database operations and visual
  inspection while keeping `COORDINATOR_DATABASE_URL` provider-neutral.
- Revisit a full schema migration framework once deployments need
  multi-version database upgrades beyond the lightweight readiness marker.
- Decide whether to add OpenAPI/protobuf contract generation after the API
  surface stabilizes.

## Delivery Model

- One PR per milestone or documentation slice.
- Keep branches focused.
- README stays concise and links to deeper docs.
- Behavior-changing PRs update docs in the same branch.
- `gofmt -l .`, `git diff --check`, `go build ./...`, `go test ./...`, and
  `go vet ./...` are the default validation gates.
- Postgres integration tests stay opt-in so default tests remain DB-free.
