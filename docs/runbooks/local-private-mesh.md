# Local Private Mesh Runbook

This runbook covers the default local workflow: one coordinator, two agents,
plain HTTP, in-memory coordinator storage, and allowlisted command execution.
It does not cover durable Postgres operation or mTLS.

## Quick Smoke

From the repository root:

```bash
./examples/demo.sh
```

The script:

- builds temporary local `coordinator`, `agent`, and `pmctl` binaries
- starts the coordinator from `config/coordinator.env.example`
- starts two agents from `config/agent-1.env.example` and
  `config/agent-2.env.example`
- uses `config/pmctl.env.example`
- waits for both agents to become `HEALTHY`
- submits an allowlisted `echo` command job
- inspects the completed job

Expected outcome:

- `pmctl status` reports `status=ok`, protocol version `1`, and in-memory
  storage.
- `pmctl nodes list` shows `local-agent-1` and `local-agent-2` as `HEALTHY`.
- The submitted job reaches `COMPLETED`.
- The job stdout is `hello from planetary mesh`.

The script writes logs under a temporary directory and prints that path at the
end. If it fails, inspect the coordinator and agent logs in that directory
first.

## Tracked Config Examples

The local smoke workflow uses these tracked examples directly:

- `config/coordinator.env.example`
- `config/agent-1.env.example`
- `config/agent-2.env.example`
- `config/pmctl.env.example`

For personal local runs, copy examples to local `*.env` files such as
`config/coordinator.env`. Do not commit local `config/*.env` files.

Default local addresses:

| Component | Address |
|---|---|
| Coordinator | `http://localhost:8080` |
| Agent 1 | `http://localhost:8081` |
| Agent 2 | `http://localhost:8082` |

The example agents allow logical command keys:

```text
echo=echo,false=false,sleep=sleep
```

Those are allowlist entries, not a general permission to execute arbitrary
commands.

## Manual Startup

Start the coordinator:

```bash
go run ./cmd/coordinator --config config/coordinator.env.example
```

Start the first agent in another terminal:

```bash
go run ./cmd/agent --config config/agent-1.env.example
```

Start the second agent in another terminal:

```bash
go run ./cmd/agent --config config/agent-2.env.example
```

Health checks do not require the protocol header:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
curl http://localhost:8082/healthz
```

Each should return:

```text
ok
```

## Operator Flow With pmctl

Use the tracked `pmctl` example config:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example status
go run ./cmd/pmctl --config config/pmctl.env.example nodes list
```

Expected node state after both agents register:

- `local-agent-1` is `HEALTHY`
- `local-agent-2` is `HEALTHY`
- capabilities include the configured example labels
- active execution counts are approximate heartbeat snapshots

Submit a command job:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example submit command echo "hello mesh"
```

List and inspect jobs:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example jobs list
go run ./cmd/pmctl --config config/pmctl.env.example jobs inspect job-1
```

For scripts or precise inspection, use JSON output:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example --json nodes list
go run ./cmd/pmctl --config config/pmctl.env.example --json jobs inspect job-1
```

## Operator Flow With curl

Versioned coordinator endpoints require:

```text
X-Planetary-Protocol-Version: 1
```

Submit a command job:

```bash
curl -X POST http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1' \
  -H 'Content-Type: application/json' \
  -d '{"type":"command","command":"echo","args":["hello mesh"]}'
```

List nodes and jobs:

```bash
curl http://localhost:8080/nodes \
  -H 'X-Planetary-Protocol-Version: 1'

curl http://localhost:8080/jobs \
  -H 'X-Planetary-Protocol-Version: 1'
```

For full endpoint details, status codes, JSON fields, and compatibility policy,
use [HTTP/JSON v0 API inventory](../api-http-json-v0.md).

## Local Behavior Notes

- In-memory coordinator state is lost when the coordinator exits.
- Plain HTTP is the default for local development.
- Node capability and load metadata is visibility-only and does not affect
  scheduling.
- Initial dispatch selects the first healthy node; retryable dispatch failures
  can reassign work to another healthy node.
- `CANCELLED` is reserved but unsupported; there is no cancellation API.
- Command execution is allowlisted direct process execution, not strong
  sandboxing.

## Basic Validation

For a local docs or operator workflow change:

```bash
git diff --check
```

For a full DB-free confidence check:

```bash
gofmt -l .
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
```
