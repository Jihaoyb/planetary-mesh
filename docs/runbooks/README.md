# Operator Runbooks

These runbooks are the task-oriented operating guide for the current
Planetary Mesh trusted LAN/private-network prototype. They document behavior
that exists today. For endpoint details and compatibility rules, use the
authoritative [HTTP/JSON v0 API inventory](../api-http-json-v0.md).

## Scope

Use these runbooks for private/local operation across machines a user or team
owns or controls:

- [Local Private Mesh](local-private-mesh.md) - in-memory coordinator storage,
  tracked config examples, `examples/demo.sh`, manual starts, and basic
  `pmctl` workflows.

The remaining Milestone 17 runbooks cover Postgres durability, mTLS trusted-LAN
setup, command execution safety, and troubleshooting.

## Current Boundaries

Current behavior:

- HTTP/JSON v0 with `X-Planetary-Protocol-Version: 1`.
- Plain HTTP local development by default.
- Optional Postgres coordinator storage for nodes and jobs.
- Optional mTLS with manual CA/certificate/key provisioning and node
  allowlists.
- Allowlisted direct command execution on trusted hosts.
- `pmctl` as a thin client over the coordinator API.

Current non-goals:

- No dashboard or desktop app.
- No production image or packaged release workflow.
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

## Validation Matrix

| Workflow | Command | Notes |
|---|---|---|
| Docs-only change | `git diff --check` | Minimum check for Markdown-only edits. |
| Go formatting inventory | `gofmt -l .` | Should print nothing. |
| Build | `GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...` | DB-free. |
| Default tests | `GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...` | DB-free. |
| Vet | `GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...` | DB-free. |
| Local smoke | `./examples/demo.sh` | Starts local coordinator and two agents with in-memory storage. |
| Postgres smoke | `./examples/postgres_smoke.sh` | Requires Docker Compose; verifies durable storage and reconciliation behavior. |
| Opt-in Postgres tests | `GOCACHE=/private/tmp/planetary-mesh-gocache-postgres go test -tags postgres ./internal/coordinator` | Use only when touching Postgres behavior or explicitly validating durable storage. |

The default `go test ./...` path must remain DB-free. Do not commit local-only
files such as `.DS_Store`, `.claude/`, `.gocache/`, or local `config/*.env`
files.
