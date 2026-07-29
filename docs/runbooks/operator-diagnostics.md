# Operator Diagnostics

`pmctl doctor` is the read-only first-line diagnostic for the current private
mesh. It validates local pmctl configuration, then combines the existing
coordinator `/status` and `/nodes` responses into ordered readiness checks.

Doctor does not submit a test job. It does not execute a command, contact an
agent directly, inspect an agent allowlist or filesystem, parse `/metrics`,
collect logs, change configuration, manage services, or manage certificates.

## Commands

Global pmctl flags stay before the command. Doctor-only flags stay after it:

```bash
pmctl doctor
pmctl doctor --strict
pmctl doctor --timeout 5s
pmctl --json doctor
pmctl --json doctor --strict --timeout 5s
pmctl --config config/pmctl.env.example doctor
pmctl --coordinator-url http://localhost:8080 doctor
```

`--timeout` is one total network budget shared by `/status` and `/nodes`.
It defaults to `10s` and accepts values from `100ms` through `60s`. An earlier
caller context deadline still wins. There is no timeout config-file or
environment key.

`--json` is a global flag and therefore this is invalid:

```bash
pmctl doctor --json
```

## Check Order

Checks are emitted in this order when their prerequisites are available:

| Check | What it proves |
|---|---|
| `client_configuration` | The selected config file, coordinator URL, and optional TLS material are locally valid. |
| `coordinator_connectivity` | The configured endpoint returned an HTTP response. A response does not by itself prove that it is a compatible coordinator. |
| `status_endpoint` | `/status` returned one decodable JSON document. |
| `protocol_compatibility` | The coordinator reports protocol version `1`; a `409` is classified here. |
| `coordinator_health` | Coordinator status is `ok` and dispatch runtime metadata is valid. |
| `storage_readiness` | In-memory mode is coherent, or Postgres schema readiness metadata is coherent. |
| `transport_security` | Coordinator secure-mode and node-allowlist metadata agree. |
| `reconciliation` | Reconciliation is not applicable, clear, pending, or invalid. |
| `node_readiness` | Coordinator-reported node metadata is valid and summarizes healthy, suspect, and offline nodes. |

If an earlier response cannot be evaluated, dependent checks are omitted.
Unknown JSON facts remain `null`.

## PASS, WARN, and FAIL

The overall status is FAIL when any check fails, otherwise WARN when any check
warns, otherwise PASS.

Current informational PASS cases include:

- in-memory storage, with an explicit non-durability note
- plain coordinator mode, with an explicit local/trusted-network scope note
- Postgres with schema readiness version `2` or `3`
- zero pending reconciliation jobs
- at least one healthy node with no suspect/offline nodes

WARN cases include:

- no registered nodes
- registered nodes but no `HEALTHY` node
- at least one healthy node plus suspect/offline nodes
- pending Postgres startup reconciliation
- a Postgres schema that is internally ready but newer than this pmctl
  recognizes

No healthy node sets `job_submission_ready` to `false`, but it is not a
connectivity or coordinator-process failure. A mixed healthy/unhealthy mesh can
remain job-ready while warning about degraded nodes.

FAIL cases include:

- malformed/missing explicit config
- invalid or credential/query-bearing coordinator URL
- partial, unreadable, or invalid TLS material
- TLS files configured with a plain HTTP URL
- unreachable coordinator or failed TLS handshake
- access errors, redirect, missing endpoint, `5xx`, malformed JSON, or
  protocol mismatch
- coordinator status/runtime metadata failure
- unknown/inconsistent storage, schema, security, or reconciliation metadata
- malformed required node metadata or a failed `/nodes` request

Doctor validates node IDs, addresses, last-seen timestamps, states,
capabilities, and non-negative load, but reports only aggregate counts. Empty
capabilities, zero load, and missing optional certificate metadata remain
compatible.

## Exit Codes

| Exit | Meaning |
|---:|---|
| `0` | PASS, or WARN in normal mode |
| `1` | Diagnostic FAIL |
| `2` | Doctor usage/flag error |
| `3` | Caller cancellation or diagnostic timeout |
| `4` | Unexpected internal/output failure |
| `5` | WARN under `--strict` |

`--strict` changes only the process exit. WARN remains WARN in human and JSON
output. A FAIL takes precedence over strict-warning exit behavior.

PASS, WARN, diagnostic FAIL, timeout, and cancellation write a complete report
to stdout and leave stderr empty. Usage errors write a concise `pmctl:` error
to stderr. An output-writer failure can leave partial stdout and returns `4`.

## Human Output

Human output has a stable section order:

```text
Planetary Mesh doctor
Overall: WARN
Mode: normal
Timeout: 10s
Ready for job submission: no

CHECK                     STATUS  SUMMARY
client_configuration      PASS    Local pmctl configuration is valid.
...
node_readiness            WARN    No agents are registered.

Summary: PASS=8 WARN=1 FAIL=0

Remediation
- node_readiness: Start and register at least one agent, then rerun pmctl doctor.

Limitations
- Node health is a coordinator-reported heartbeat snapshot, not a direct agent probe.
- A PASS does not prove that a particular constrained workload has a matching node, is allowlisted, or has required agent-local files; it also does not prove strong isolation or production readiness.
```

The report never prints the coordinator URL, node identity/address,
capabilities, timestamps, certificate metadata, config/TLS paths or values,
HTTP bodies/headers, redirects, database URLs, private keys, or arbitrary
remote strings.

## JSON Automation Contract

`pmctl --json doctor` emits diagnostic schema version `1`. Every top-level and
`facts` field is present; unavailable facts are `null`. Arrays such as
`checks`, `remediation`, `endpoints_used`, and `limitations` are always arrays,
including when empty.

Important fields:

- `overall_status`: `PASS`, `WARN`, or `FAIL`
- `strict` and canonical `timeout`
- `summary`: counts by status
- `facts.coordinator_reachable`, `protocol_compatible`,
  `coordinator_healthy`, and `job_submission_ready`
- sanitized storage/schema/security/dispatch/reconciliation facts
- aggregate `nodes` counts only
- ordered checks with stable `name`, `status`, `code`, `summary`, and
  `remediation`
- `scope`, which records the read-only/no-mutation boundaries

Automation should match checks by `name`, not array position, and ignore
unknown fields or future check names. Schema version `1` may receive additive
fields/checks. Removing, renaming, or changing existing meanings requires a
new diagnostic schema version and explicit milestone planning.

Example coordinator-only query:

```bash
report="$(pmctl --json doctor)"
printf '%s\n' "${report}"
```

For CI that must reject warnings:

```bash
pmctl --json doctor --strict >doctor-report.json
```

An exit of `5` means the file contains a valid WARN report. An exit of `1`
means it contains a valid diagnostic FAIL report.

## Failure Remediation

| Code or condition | First action |
|---|---|
| `config_file_invalid` | Check `--config` or `PMCTL_CONFIG_FILE`; do not share secret-bearing values. |
| `coordinator_url_invalid` | Use an absolute HTTP(S) base URL without credentials, query, fragment, or path. |
| `tls_config_partial` | Configure all three pmctl CA/certificate/key settings or none. |
| `tls_file_unreadable`, `tls_ca_invalid`, `tls_keypair_invalid` | Correct local TLS files/permissions without printing their paths or contents. |
| `coordinator_unreachable` | Confirm the coordinator process, network path, and firewall. |
| `tls_handshake_failed` | Check CA trust, client key pair, hostname, expiry, coordinator mTLS, and operator certificate. |
| `status_unauthorized`, `status_forbidden` | Check operator TLS/access configuration. |
| `status_not_found`, `status_redirect_rejected` | Use the direct final coordinator base URL and a compatible binary. |
| `protocol_rejected`, `protocol_missing`, `protocol_mismatch` | Use binaries compatible with `X-Planetary-Protocol-Version: 1`. |
| `status_server_error`, invalid runtime/storage metadata | Inspect coordinator service logs and startup configuration. |
| `reconciliation_pending` | Wait for reports or grace expiry, rerun doctor, then inspect jobs. |
| `no_nodes`, `no_healthy_nodes`, `nodes_degraded` | Inspect agent processes, registration, coordinator URL, firewall, protocol, and mTLS configuration. |

Doctor intentionally does not scrape journald or process logs. Continue with
[Troubleshooting](troubleshooting.md), [Postgres Durability](postgres-durability.md),
or [mTLS Trusted LAN](mtls-trusted-lan.md) after the first check identifies the
relevant subsystem.

## What PASS Does Not Prove

PASS means the configured pmctl reached a compatible healthy coordinator whose
reported storage/security metadata is coherent and whose `/nodes` snapshot
contains at least one healthy agent.

PASS does not prove:

- that any healthy node matches a particular job's required capabilities
- direct agent endpoint reachability
- agent allowlist coverage
- workload executable, wrapper, input, or output-path availability
- safe wrapper arguments or trustworthy workloads
- command success
- strong sandbox/container/VM isolation
- full restart recovery or durable agent result history
- automated certificate lifecycle
- production, remote-mesh, shared-pool, or marketplace readiness

Use a deliberately selected allowlisted workload only when an operator
actually intends to create and execute a job. Doctor never does that on the
operator's behalf.

## Validation

The dedicated safe smoke creates no diagnostic job:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-doctor \
./examples/doctor_smoke.sh
```

It verifies coordinator-only WARN, strict exit `5`, healthy-agent PASS,
schema-versioned JSON, empty stderr for diagnostic outcomes, and an empty jobs
list before and after every doctor call. Installed-binary behavior is covered
by:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-release \
./examples/release_smoke.sh
```
