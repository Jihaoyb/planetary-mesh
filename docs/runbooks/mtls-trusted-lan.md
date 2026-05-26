# mTLS Trusted LAN Runbook

This runbook covers current opt-in mTLS operation for a trusted LAN/private
network. It assumes operators manually provision certificate files and maintain
node allowlists. It does not describe automated enrollment, issuance, rotation,
remote private mesh, or public-node onboarding.

## Current Security Shape

Plain HTTP remains available for local development. mTLS is enabled only when
CA, certificate, and key files are configured.

Secure mode provides:

- TLS encryption for coordinator-agent traffic
- mutual certificate authentication
- coordinator-side node admission by certificate identity or SHA-256
  fingerprint
- certificate metadata on node inspection

Secure mode does not provide:

- automated certificate lifecycle management
- user/operator authorization
- remote mesh networking
- strong sandboxing for command execution
- support for untrusted arbitrary workloads

## Certificate Files

Operators must provision these files outside Planetary Mesh:

| Actor | Required placeholders |
|---|---|
| CA | `./certs/ca.pem` |
| Coordinator | `./certs/coordinator.pem`, `./certs/coordinator-key.pem` |
| Agent 1 | `./certs/agent-1.pem`, `./certs/agent-1-key.pem` |
| Agent 2 | `./certs/agent-2.pem`, `./certs/agent-2-key.pem` |
| Operator client for `pmctl` | `./certs/operator.pem`, `./certs/operator-key.pem` |

File paths above are placeholders. Use paths appropriate for the operator
environment and protect private keys with normal host-level controls.

If any TLS file is configured for a component, all three files for that
component are required:

- CA file
- certificate file
- key file

Partial TLS configuration fails at startup or client construction.

## Coordinator Configuration

Example coordinator secure-mode configuration:

```bash
COORDINATOR_TLS_CA_FILE=./certs/ca.pem \
COORDINATOR_TLS_CERT_FILE=./certs/coordinator.pem \
COORDINATOR_TLS_KEY_FILE=./certs/coordinator-key.pem \
COORDINATOR_ALLOWED_NODE_IDENTITIES='agent-1=dns:agent-1.local,agent-2=dns:agent-2.local' \
go run ./cmd/coordinator
```

When coordinator TLS is configured, a node allowlist is required. Use at least
one of:

```bash
COORDINATOR_ALLOWED_NODE_IDENTITIES='agent-1=dns:agent-1.local'
```

```bash
COORDINATOR_ALLOWED_NODE_FINGERPRINTS='agent-1=<sha256-hex-fingerprint>'
```

Identity allowlist values may use:

- `dns:<name>`
- `ip:<address>`
- `uri:<value>`
- `cn:<name>`
- `subject:<value>`

The request `node_id` must match the allowlist entry key. For example, an agent
configured as `NODE_ID=agent-1` must be allowlisted under `agent-1`.

## Agent Configuration

Example secure agent configuration:

```bash
COORDINATOR_URL=https://localhost:8080 \
NODE_ID=agent-1 \
AGENT_ADDR=:8081 \
AGENT_ADVERTISE_ADDR=https://agent-1.local:8081 \
AGENT_TLS_CA_FILE=./certs/ca.pem \
AGENT_TLS_CERT_FILE=./certs/agent-1.pem \
AGENT_TLS_KEY_FILE=./certs/agent-1-key.pem \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep' \
go run ./cmd/agent
```

Secure agents require `COORDINATOR_URL` to use `https://`. If TLS is configured
and no `AGENT_ADVERTISE_ADDR` is set, the agent defaults its advertised address
to an HTTPS localhost URL derived from `AGENT_ADDR`. For multi-host LAN runs,
set `AGENT_ADVERTISE_ADDR` explicitly to a hostname or address the coordinator
can reach.

The coordinator uses HTTPS and presents its configured certificate when
dispatching to secure agent `/execute`.

The allowlist example uses portable no-shell built-in validation targets. Real
private workloads can still map logical keys to external executable paths, but
those tools must exist on the agent host.
Do not treat mTLS plus built-ins as a sandbox or as support for arbitrary user
workloads.

## Secure pmctl Access

The coordinator TLS server requires client certificates. Provide a CA and
operator client certificate/key to `pmctl`:

```bash
go run ./cmd/pmctl \
  --coordinator-url https://localhost:8080 \
  --ca-file ./certs/ca.pem \
  --cert-file ./certs/operator.pem \
  --key-file ./certs/operator-key.pem \
  nodes list
```

Equivalent env-style config keys:

```text
PMCTL_COORDINATOR_URL=https://localhost:8080
PMCTL_TLS_CA_FILE=./certs/ca.pem
PMCTL_TLS_CERT_FILE=./certs/operator.pem
PMCTL_TLS_KEY_FILE=./certs/operator-key.pem
```

`pmctl` remains a thin client. It does not own node admission, scheduling,
validation, storage, or lifecycle transitions.

## Verifying Secure Registration

List nodes:

```bash
go run ./cmd/pmctl \
  --coordinator-url https://localhost:8080 \
  --ca-file ./certs/ca.pem \
  --cert-file ./certs/operator.pem \
  --key-file ./certs/operator-key.pem \
  nodes list
```

Expected outcome:

- allowlisted agents register as `HEALTHY`
- unauthorized agents are rejected before entering coordinator storage
- node inspection includes certificate fingerprint and identity metadata when
  present

JSON output is useful for inspecting certificate fields:

```bash
go run ./cmd/pmctl \
  --coordinator-url https://localhost:8080 \
  --ca-file ./certs/ca.pem \
  --cert-file ./certs/operator.pem \
  --key-file ./certs/operator-key.pem \
  --json nodes list
```

## Common mTLS Failures

| Symptom | Likely cause |
|---|---|
| Startup error mentioning partial TLS config | One of CA, cert, or key file is missing. |
| Coordinator rejects startup with secure mode allowlist error | TLS is configured but no node identity or fingerprint allowlist is set. |
| Coordinator rejects allowlist without TLS | Node allowlists require coordinator TLS config. |
| Agent rejects config because URL uses HTTP | Secure agent mode requires `COORDINATOR_URL=https://...`. |
| Agent does not register | Certificate is missing, not trusted by the CA, expired, or not allowlisted for the configured `NODE_ID`. |
| `pmctl` cannot connect to secure coordinator | Operator client CA/cert/key are missing, partial, or not trusted by the coordinator TLS config. |

## Current Limits

- Certificate generation, distribution, enrollment, and rotation are manual.
- There is no automated fingerprint discovery or allowlist update workflow.
- Secure mode authenticates transport peers; it does not make command execution
  safe for untrusted arbitrary workloads.
- Plain HTTP remains available for local development unless TLS files are
  configured.

For endpoint-level mTLS expectations, see
[HTTP/JSON v0 API inventory](../api-http-json-v0.md).
