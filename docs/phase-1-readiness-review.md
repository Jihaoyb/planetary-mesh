# Phase 1 Readiness Review

- Milestone: 20
- Review date: 2026-05-27
- Baseline reviewed: `main` after Milestone 18 real multi-device LAN validation
  and Milestone 19 portable agent validation built-ins
- Decision: Phase 1 is ready to close

## Decision

Phase 1, private/local compute mesh hardening, should be marked complete.

The repository now contains enough evidence that Planetary Mesh works as a
trusted LAN/private-network command-job mesh across machines a user or team owns
or controls. The remaining gaps are important productization work, but they do
not block the Phase 1 exit criteria.

No final Phase 1 implementation milestone is recommended before Phase 2
planning. If reviewers reject this phase gate, the single highest-value final
Phase 1 milestone should be **Private Mesh Onboarding Hardening**, scoped to
source-based first-LAN-job setup and documentation polish only. It should not
add protocol, storage, scheduling, security, packaging, dashboard, or remote
mesh behavior.

## Evidence Reviewed

Primary evidence:

- `README.md`
- `docs/roadmap.md`
- `docs/product-requirements.md`
- `docs/product-positioning.md`
- `docs/architecture.md`
- `docs/current-limitations.md`
- `docs/tech-choices.md`
- `docs/api-http-json-v0.md`
- `docs/runbooks/`
- `docs/adr/0011-node-capability-load-visibility.md`
- `docs/adr/0012-job-lifecycle-state-transitions.md`
- `docs/adr/0013-agent-reconciliation-strategy.md`
- `docs/adr/0014-http-json-v0-api-inventory-and-compatibility.md`
- `examples/demo.sh`
- `examples/postgres_smoke.sh`
- `config/*.env.example`
- `compose.yaml`
- `.github/workflows/ci.yml`
- Milestone 19 built-in validation code and tests under `internal/agent`

Preflight state:

- Branch at review start: `main`
- Recent baseline commit: `5e854b0 Merge milestone 18 real LAN validation`
- `main` was up to date with `origin/main` before this milestone branch was
  created

## Exit Criteria

| Criterion | Result | Evidence |
|---|---|---|
| Coordinator runs on one physical machine | Satisfied | Milestone 18 evidence in `docs/runbooks/real-lan-validation.md` records coordinator hosts on macOS, Linux, and Windows physical machines. |
| Agent runs on a different physical LAN machine | Satisfied | The same evidence records coordinator and agent on different physical LAN machines for each tested OS pair. |
| Allowlisted command dispatch works across devices | Satisfied | Real LAN evidence records cross-device `echo` jobs using `echo=builtin:echo` reaching `COMPLETED`. |
| `pmctl` inspects remote node/job/result state | Satisfied | Real LAN evidence records `pmctl status`, `pmctl nodes list`, job listing, and job inspection over the coordinator LAN address. |
| Basic failure/restart behavior is observed across LAN | Satisfied | Real LAN evidence records remote agent stop, node `OFFLINE`, queued job while no healthy agent exists, restart, and later completion after scheduler re-dispatch. |
| Practical workload exists beyond `echo` | Satisfied | `docs/runbooks/practical-workload-recipe.md` documents `line-count`, and Milestone 18 evidence records remote `builtin:line-count` completion against an agent-local file. |
| Operator docs exist for real LAN setup | Satisfied | `docs/runbooks/README.md`, `local-private-mesh.md`, `real-lan-validation.md`, `mtls-trusted-lan.md`, `postgres-durability.md`, `command-execution-safety.md`, and `troubleshooting.md` cover current operations. |
| Command execution safety limitations are clear | Satisfied | `README.md`, `docs/current-limitations.md`, `docs/architecture.md`, and `docs/runbooks/command-execution-safety.md` describe trusted-host allowlisted direct execution. |
| No strong sandboxing is clearly stated | Satisfied | Current docs repeatedly state there is no strong sandbox, container, VM, microVM, or multi-tenant isolation. |
| Manual mTLS lifecycle is clear | Satisfied | `docs/runbooks/mtls-trusted-lan.md`, `docs/current-limitations.md`, and ADR 0007 describe opt-in mTLS and manual certificate lifecycle. |
| Local/private-only scope is clear | Satisfied | Product and roadmap docs keep remote private mesh, shared pools, and marketplace work as future phases. |

## Phase 1 Blockers

No Phase 1 exit blockers were found.

The main historical blocker was real multi-device LAN evidence. Milestone 18
now records sanitized evidence across macOS, Linux, and Windows
coordinator/agent pairs, and Milestone 19 removed the cross-OS validation
dependency on platform shell built-ins by adding explicit no-shell validation
targets.

## Phase 2 Backlog

These gaps remain important, but they should be treated as Phase 2 or later
backlog unless a future phase gate reclassifies them:

- Install and onboarding friction: users still run from source or Compose
  examples.
- Packaging/release gap: there is no production image, packaged release, or
  service install workflow.
- Operator UX: `pmctl` and runbooks cover current workflows, but there is no
  dashboard or rich logs UX.
- Scheduler policy: reported capabilities and active execution count are
  operator-visible only; dispatch remains first healthy node with cross-node
  retry.
- Cancellation gap: `CANCELLED` is reserved, but there is no cancellation API or
  cancellation behavior.
- API contract: the HTTP/JSON v0 inventory is manual; generated OpenAPI,
  protobuf, SDKs, gRPC, WebSockets, and SSE remain future decisions.
- Postgres and restart limits: Postgres persists nodes/jobs only; agent result
  history is bounded in memory; full in-progress execution recovery does not
  exist.
- mTLS lifecycle: certificate generation, distribution, enrollment, and
  rotation remain manual.
- Execution isolation: allowlisted command execution is not strong sandboxing
  and is not appropriate for arbitrary untrusted workloads.
- Workflow model: built-ins are validation helpers, not a generic workflow
  extension mechanism.

## Compatibility Statements

This milestone makes no runtime behavior change.

- Public API/type/interface changes: none.
- Coordinator-agent protocol: unchanged HTTP/JSON v0 with
  `X-Planetary-Protocol-Version: 1`.
- Health endpoints: unchanged and unversioned.
- Job lifecycle states: unchanged `QUEUED`, `RUNNING`, `COMPLETED`, and
  `FAILED`; `CANCELLED` remains reserved and unsupported.
- Command execution: unchanged allowlisted direct execution with no shell.
- Built-ins: unchanged explicit validation targets available only through
  `AGENT_COMMAND_ALLOWLIST`.
- Storage: unchanged in-memory default and optional Postgres nodes/jobs
  persistence.
- Postgres schema readiness: unchanged version `2`.
- Security model: unchanged trusted-host execution, opt-in mTLS, node
  allowlists, and manual certificate lifecycle.

## ADR Assessment

No new ADR is needed for this milestone. The readiness review records a phase
gate decision and does not introduce a new protocol, storage, execution,
security, lifecycle, operator, or product architecture behavior.

If a future milestone changes scheduler policy, cancellation semantics,
generated API contracts, remote node trust, certificate lifecycle, execution
isolation, storage shape, or workflow templates, it should include a focused ADR
when the decision is non-trivial.

## Validation Plan

Docs-only validation:

```bash
git diff --check
gofmt -l .
```

Full confidence validation:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
```

The opt-in Postgres test gate is not required because this milestone does not
touch coordinator/Postgres behavior.
