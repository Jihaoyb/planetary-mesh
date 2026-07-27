# Operator Runbooks

These runbooks are the task-oriented operating guide for the current
Planetary Mesh trusted LAN/private-network prototype. They document behavior
that exists today. For endpoint details and compatibility rules, use the
authoritative [HTTP/JSON v0 API inventory](../api-http-json-v0.md).

## Scope

Use these runbooks for private/local operation across machines a user or team
owns or controls:

- [First-Run Private Mesh Onboarding](first-run-private-mesh.md) - the
  source-based path from fresh checkout to local smoke, manual component
  startup, a two-machine LAN mesh, `pmctl` inspection, portable validation
  workloads, cleanup, and failure handling.
- [Local Release Build and Install Smoke](local-release-install.md) - the
  pre-release local binary artifact layout and installed-binary smoke workflow
  for coordinator, agent, `pmctl`, and the tracked `text-stats` workload.
- [Linux Managed Service Installation](linux-service-install.md) - independent
  coordinator and agent installation, systemd operation, journald inspection,
  safe removal, and manual upgrade/rollback from Linux release archives.
- [Local Private Mesh](local-private-mesh.md) - in-memory coordinator storage,
  tracked config examples, `examples/demo.sh`, manual starts, and basic
  `pmctl` workflows.
- [Postgres Durability](postgres-durability.md) - optional durable
  coordinator storage, schema readiness, Compose workflows, and startup
  reconciliation behavior.
- [mTLS Trusted LAN](mtls-trusted-lan.md) - opt-in mutual TLS, manual
  certificate provisioning, node allowlists, and secure `pmctl` access.
- [Command Execution Safety](command-execution-safety.md) - allowlisted direct
  command execution, timeout/output bounds, and current isolation limits.
- [Real LAN Validation](real-lan-validation.md) - two-physical-machine LAN
  validation workflow, sanitized completion evidence, and failure/restart
  observation steps.
- [Practical External Workload Recipe](practical-workload-recipe.md) - trusted
  host-local `text-stats` external executable/wrapper workload.
- [Workflow Templates](workflow-templates.md) - `pmctl` JSON template
  validation, inspection, preview, and submission for repeatable private
  command workflows.
- [Operator Diagnostics](operator-diagnostics.md) - read-only `pmctl doctor`
  checks, PASS/WARN/FAIL and exit behavior, JSON automation contract,
  remediation, redaction, and limitations.
- [Troubleshooting](troubleshooting.md) - common failure symptoms and current
  inspection surfaces.

## Current Boundaries

Current behavior:

- HTTP/JSON v0 with `X-Planetary-Protocol-Version: 1`.
- Plain HTTP local development by default.
- Optional Postgres coordinator storage for nodes and jobs.
- Optional mTLS with manual CA/certificate/key provisioning and node
  allowlists.
- Allowlisted command execution on trusted hosts, including explicit portable
  built-in validation targets when configured.
- Tracked external workload examples under `examples/workloads/` for the
  current wrapper/executable path.
- Tracked example workflow templates under `examples/templates/` for the
  current `pmctl` client-side validation, inspection, preview, and expansion
  path.
- Pre-release local binary artifact generation and installed-binary smoke
  validation for development use.
- Pre-release Linux/systemd installation for independently managed coordinator
  and agent services from extracted Linux release archives.
- `pmctl` as a thin client over the coordinator API.
- `pmctl doctor` as a read-only composition over `/status` and `/nodes`; it
  creates no jobs and contacts no agents directly.

Current non-goals:

- No dashboard or desktop app.
- No production image, signed distribution, package-manager delivery, GitHub
  Release artifact, automatic upgrade, macOS launchd installer, or Windows
  service installer.
- No generated OpenAPI/protobuf contract.
- No remote private mesh, shared pool, public-node onboarding, marketplace, or
  payment system.
- No strong sandbox, container, VM, or multi-tenant isolation.
- No automated certificate enrollment, issuance, or rotation.

## Prerequisites

Baseline local operation expects:

- Go matching `go.mod`.
- `curl`.
- `python3` for the smoke scripts.
- `bash` for `examples/*.sh`.

Postgres and Compose workflows additionally expect:

- Docker with the `docker compose` command.
- Available host ports or explicit port overrides.

Secure mTLS operation additionally expects:

- A manually provisioned CA file.
- Coordinator, agent, and optional operator client certificate/key pairs.
- Node allowlists by certificate identity or SHA-256 fingerprint.

Real LAN validation additionally expects:

- A coordinator on one physical machine.
- At least one agent on a different physical machine on the same LAN.
- Firewall rules or host settings that allow the chosen coordinator and agent
  ports.
- Sanitized evidence capture using placeholders, not private IP addresses,
  hostnames, credentials, certificates, keys, or local env files.

## Validation Matrix

| Workflow | Command | Notes |
|---|---|---|
| Docs-only change | `git diff --check` | Minimum check for Markdown-only edits. |
| Go formatting inventory | `gofmt -l .` | Should print nothing. |
| Build | `GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...` | DB-free. |
| Default tests | `GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...` | DB-free. |
| Vet | `GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...` | DB-free. |
| Local smoke | `./examples/demo.sh` | Starts local coordinator and two agents with in-memory storage. |
| Doctor smoke | `GOCACHE=/private/tmp/planetary-mesh-gocache-doctor ./examples/doctor_smoke.sh` | Verifies coordinator-only WARN/strict behavior, healthy PASS, schema-versioned JSON, and no job creation. |
| External workload smoke | `GOCACHE=/private/tmp/planetary-mesh-gocache-workload ./examples/external_workload_smoke.sh` | Builds and runs the tracked `text-stats` helper through the allowlisted external command path. |
| Template smoke | `GOCACHE=/private/tmp/planetary-mesh-gocache-template ./examples/template_smoke.sh` | Validates, previews, and submits the tracked `text-stats` template through `pmctl`. |
| Local release smoke | `GOCACHE=/private/tmp/planetary-mesh-gocache-release ./examples/release_smoke.sh` | Builds a host release layout and runs coordinator, agent, installed `pmctl doctor`, `text-stats`, and installed template preview/submission. |
| Linux service-install smoke | `GOCACHE=/private/tmp/planetary-mesh-gocache-linux-service ./examples/linux_service_install_smoke.sh` | Builds a Linux archive and validates service assets and safe temporary-root install/uninstall behavior without mutating the host. |
| Postgres smoke | `./examples/postgres_smoke.sh` | Requires Docker Compose; verifies durable storage and reconciliation behavior. |
| First-run onboarding | Follow [First-Run Private Mesh Onboarding](first-run-private-mesh.md) | Source-based local and LAN operator path with cleanup and failure handling. |
| Opt-in Postgres tests | `GOCACHE=/private/tmp/planetary-mesh-gocache-postgres go test -tags postgres ./internal/coordinator` | Use only when touching Postgres behavior or explicitly validating durable storage. |
| Real LAN validation | Follow [Real LAN Validation](real-lan-validation.md) | Manual gate for proving coordinator, remote agent, `pmctl`, dispatch, result capture, and failure/restart behavior across physical LAN machines. |

The default `go test ./...` path must remain DB-free. Do not commit local-only
files such as `.DS_Store`, `.claude/`, `.gocache/`, or local `config/*.env`
files.
