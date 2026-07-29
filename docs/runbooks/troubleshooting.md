# Troubleshooting Runbook

This runbook maps common current failure symptoms to the inspection surfaces
available today: health checks, `pmctl`, `/status`, `/metrics`, process logs,
and smoke script output.

For the ordered source-based setup path, use
[First-Run Private Mesh Onboarding](first-run-private-mesh.md) first.

For endpoint details and status-code expectations, use the
[HTTP/JSON v0 API inventory](../api-http-json-v0.md).

## First Checks

Run the consolidated read-only diagnostic first:

```bash
go run ./cmd/pmctl doctor
go run ./cmd/pmctl --json doctor
go run ./cmd/pmctl doctor --strict
```

Doctor uses only `GET /status` and `GET /nodes`; it does not create jobs,
execute commands, contact agents directly, inspect allowlists or agent-local
files, collect logs, or modify configuration. Normal warnings exit `0`;
`--strict` exits `5` for warnings. See
[Operator Diagnostics](operator-diagnostics.md) for the complete check, JSON,
exit-code, redaction, and limitation contract.

From the repository root, check the branch and local changes before debugging
code or docs drift:

```bash
git status --short --branch
```

Check coordinator and agent health:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
```

Health endpoints are unversioned. Versioned coordinator endpoints require:

```text
X-Planetary-Protocol-Version: 1
```

Check coordinator status:

```bash
go run ./cmd/pmctl status
go run ./cmd/pmctl --json status
```

Check nodes and jobs:

```bash
go run ./cmd/pmctl nodes list
go run ./cmd/pmctl jobs list
go run ./cmd/pmctl jobs inspect <job-id>
```

## Protocol Version Mismatch

Symptom:

- HTTP `409 Conflict`
- text body similar to `protocol version mismatch`

Likely cause:

- missing or incorrect `X-Planetary-Protocol-Version` header on a versioned
  endpoint

Check:

```bash
curl http://localhost:8080/status \
  -H 'X-Planetary-Protocol-Version: 1'
```

Notes:

- coordinator `/healthz` and agent `/healthz` do not need the header
- coordinator `/status`, `/register`, `/nodes`, `/jobs`, `/jobs/{id}`,
  `/jobs/{id}/result`, and `/metrics` do need the header
- agent `/execute` needs the header
- `pmctl` sends the header automatically

## Missing or Invalid Config

Symptom:

- startup exits before serving
- error mentions loading or parsing a config file

Likely causes:

- explicit `--config` path does not exist
- malformed env-style config line
- invalid duration such as `AGENT_EXEC_TIMEOUT=not-a-duration`
- invalid `COORDINATOR_RECONCILIATION_GRACE`
- invalid `AGENT_COMMAND_ALLOWLIST`
- unknown built-in allowlist target such as `builtin:not-real`
- invalid `AGENT_CAPABILITIES`

Check the tracked examples:

```bash
go run ./cmd/coordinator --config config/coordinator.env.example
go run ./cmd/agent --config config/agent-1.env.example
go run ./cmd/pmctl --config config/pmctl.env.example status
```

Config precedence is defaults, config file values, non-empty environment
variables, then CLI flags where supported.

## Partial TLS Config

Symptom:

- error mentions `partial TLS config`

Likely cause:

- only one or two of CA, cert, and key files are configured

Fix:

- configure all three TLS file paths for that component, or configure none for
  plain local development

Coordinator TLS keys:

```text
COORDINATOR_TLS_CA_FILE
COORDINATOR_TLS_CERT_FILE
COORDINATOR_TLS_KEY_FILE
```

Agent TLS keys:

```text
AGENT_TLS_CA_FILE
AGENT_TLS_CERT_FILE
AGENT_TLS_KEY_FILE
```

`pmctl` TLS keys:

```text
PMCTL_TLS_CA_FILE
PMCTL_TLS_CERT_FILE
PMCTL_TLS_KEY_FILE
```

## Node Not Registered or Unhealthy

Symptom:

- `pmctl nodes list` does not show the expected node
- node is `SUSPECT` or `OFFLINE`
- jobs remain `QUEUED`

Likely causes:

- agent process is not running
- agent cannot reach `COORDINATOR_URL`
- `AGENT_ADVERTISE_ADDR` is not reachable by the coordinator
- protocol or TLS configuration prevents registration
- in secure mode, certificate identity or fingerprint is not allowlisted for
  the configured `NODE_ID`

Check:

```bash
curl http://localhost:8081/healthz
go run ./cmd/pmctl nodes list
go run ./cmd/pmctl --json nodes list
```

For a local two-agent sanity check:

```bash
./examples/demo.sh
```

Notes:

- health state affects new dispatch selection
- node state changes do not cancel an already in-flight execution attempt
- reported active execution count is a heartbeat snapshot and may be stale for
  unhealthy nodes

## Capability-Constrained Job Placement

Use job inspection and node listing together:

```bash
go run ./cmd/pmctl --json jobs inspect <job-id>
go run ./cmd/pmctl --json nodes list
```

Current failure and waiting behavior:

- malformed, empty, overlong, or excessive `--require-capability` labels produce
  a concise `pmctl:` stderr error, exit `1`, and no coordinator request
- exact duplicate labels are accepted once and output is sorted canonically
- an old coordinator returns `404` or `405` for constrained submission; pmctl
  exits `1` with
  `coordinator does not support required capabilities; upgrade the coordinator`
  and never falls back to an unconstrained request
- an accepted constrained job with no matching healthy node is printed
  normally, exits `0`, and remains `QUEUED` with `attempts=0`; nonmatching
  healthy nodes are not contacted
- a later heartbeat with every required label makes the queued job eligible on
  a later scheduler pass
- retryable failures exhaust the configured attempts on each node in the fixed
  matching snapshot; if all fail, the job becomes `FAILED` with the last
  retryable error, without trying a nonmatching node
- validation, allowlist rejection, command failure, timeout, and other terminal
  classifications are unchanged
- if stdout fails while printing a successfully submitted job, pmctl exits `1`
  and the job may already exist; inspect jobs before retrying. Preview output
  failures also exit `1`, but preview remains local and creates no job

Capability labels are operator assertions. A match does not verify executable
installation, allowlist coverage, files, identity, hardware, or capacity.

## Allowlist Rejection

Symptom:

- job becomes `FAILED`
- `last_error` mentions the command is not allowlisted

Likely cause:

- submitted command key is not present in the agent's
  `AGENT_COMMAND_ALLOWLIST`

Check:

```bash
go run ./cmd/pmctl --json jobs inspect <job-id>
```

Then inspect the agent configuration. Example local allowlist:

```bash
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep'
```

The submitted command must be the logical key, such as `echo`, not an arbitrary
path or built-in target string. A job submitted as `builtin:echo` does not run
unless that exact logical key is explicitly present in `AGENT_COMMAND_ALLOWLIST`.

For portable smoke validation, prefer built-in targets:

```text
echo=builtin:echo
sleep=builtin:sleep
line-count=builtin:line-count
```

For real external commands, confirm the mapped executable exists on the agent
host. Shell built-ins such as Windows `echo` are not executable targets because
Planetary Mesh does not invoke a shell.
If a workflow needs richer behavior, prefer a narrow wrapper script or
allowlisted executable, such as the tracked `text-stats` example in the
[Practical External Workload Recipe](practical-workload-recipe.md). Do not add
or expect arbitrary agent built-ins for each workflow.

## Command Timeout or Non-Zero Exit

Timeout symptom:

- agent `/execute` returns `504 Gateway Timeout`
- job becomes `FAILED`
- `last_error` is similar to `command timed out after 30s`

Non-zero exit symptom:

- agent `/execute` returns `422 Unprocessable Entity`
- job becomes `FAILED`
- `exit_code` is set
- `last_error` is similar to `command exited with code 1`

Check:

```bash
go run ./cmd/pmctl jobs inspect <job-id>
go run ./cmd/pmctl --json jobs inspect <job-id>
```

Notes:

- non-zero command exits are terminal and are not retried
- timeouts are terminal command failures
- stdout and stderr may still contain useful command output
- `stdout_truncated` or `stderr_truncated` indicates a stream hit the `1 MiB`
  cap

## Postgres Startup or Schema Readiness Issues

Symptom:

- coordinator exits during startup
- `pmctl status` does not report expected schema metadata
- `/metrics` lacks Postgres schema gauges

Likely causes:

- `COORDINATOR_DATABASE_URL` is wrong or Postgres is unreachable
- database credentials or network path are wrong
- database has a schema version newer than the coordinator expects
- Compose services are not healthy yet

Check status:

```bash
go run ./cmd/pmctl --json status
```

Check metrics:

```bash
curl http://localhost:8080/metrics \
  -H 'X-Planetary-Protocol-Version: 1'
```

Expected current schema metadata:

```text
version=3
expected_version=3
```

For an end-to-end durable sanity check:

```bash
./examples/postgres_smoke.sh
```

## Reconciliation Pending or Expired Jobs

Symptom:

- `pmctl status` shows reconciliation pending jobs
- `planetary_jobs_reconciliation_pending` is greater than zero
- startup-running jobs later fail with a restart recovery error

Current behavior:

- Postgres startup captures persisted `RUNNING` job IDs
- captured jobs remain `RUNNING` during reconciliation grace
- matching agent result reports can complete or fail those jobs during grace
- unreconciled captured jobs fail when grace expires

Exact expired recovery error:

```text
coordinator restarted before result was recorded
```

Check:

```bash
go run ./cmd/pmctl status
go run ./cmd/pmctl --json jobs inspect <job-id>
curl http://localhost:8080/metrics \
  -H 'X-Planetary-Protocol-Version: 1'
```

Remaining limits:

- agent restart loses cached terminal reports
- in-memory coordinator restart loses state
- this is not full in-progress execution recovery

## Smoke Script Output

Local smoke:

```bash
./examples/demo.sh
```

The script prints a log directory. Inspect coordinator and agent logs there
after failure.

Postgres smoke:

```bash
./examples/postgres_smoke.sh
```

The script writes Compose logs on failure. Set this when you want to preserve
the Compose project for manual inspection:

```bash
KEEP_POSTGRES_SMOKE=1 ./examples/postgres_smoke.sh
```

Then clean up with the command printed by the script.

## When Behavior and Docs Disagree

Treat runtime behavior, tests, and `docs/api-http-json-v0.md` as the current
source for endpoint behavior. Update docs to match current behavior unless a
separate milestone explicitly plans a runtime change.
