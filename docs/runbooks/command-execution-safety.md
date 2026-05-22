# Command Execution Safety Runbook

This runbook describes the current command execution safety model. Planetary
Mesh runs allowlisted direct processes on trusted agent hosts. It does not
provide strong sandbox, container, VM, or multi-tenant isolation.

Use this model only with machines and workloads you trust.

## Current Execution Model

Command jobs are submitted with:

```json
{
  "type": "command",
  "command": "echo",
  "args": ["hello mesh"]
}
```

The `command` value is a logical allowlist key. It is not an executable path
from the client.

Agents map logical keys to local executables through
`AGENT_COMMAND_ALLOWLIST`:

```bash
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep'
```

The current agent executes the mapped executable with:

```text
exec.CommandContext
```

Arguments are passed as an argument vector. The agent does not invoke a shell.

## What This Protects Against

The current model reduces several common risks:

- clients cannot submit arbitrary executable paths
- command keys must exist in the agent allowlist
- arguments are not shell-expanded by Planetary Mesh
- stdout and stderr capture is bounded
- command runtime is bounded by a fixed agent timeout

This model is still direct host process execution. A dangerous allowlisted
command or dangerous arguments can still affect the agent host.

## What This Does Not Provide

Current command execution does not provide:

- strong sandboxing
- container isolation
- VM or microVM isolation
- OS-level resource controls
- per-job timeout overrides
- user-level authorization
- multi-tenant workload isolation
- safety for arbitrary third-party commands

Do not use Planetary Mesh today for untrusted arbitrary workloads.

## Allowlist Guidance

Keep allowlists narrow and task-specific:

- allow only commands needed for the private workflow
- prefer stable wrapper scripts when a workflow needs argument validation
- avoid allowlisting broad interpreters or shells
- avoid commands that mutate host state unless that is the intended private
  workflow
- keep node-specific allowlists aligned with the node's trusted role

Example narrow local allowlist:

```bash
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep'
```

This example is for local smoke workflows. It is not a production safety
policy.

## Timeout and Output Bounds

Current timeout behavior:

- configured per agent with `AGENT_EXEC_TIMEOUT`
- default is `30s`
- no per-job timeout override exists
- command timeout is terminal for that execution attempt

Current output behavior:

- stdout and stderr are captured separately
- each stream is capped at `1 MiB`
- `stdout_truncated` and `stderr_truncated` identify capped streams

Operators should treat large output as an operational signal. If a workflow
needs large artifacts, the current command result fields are not a file-transfer
system.

## Terminal Failures

The coordinator treats these as terminal command failures:

- command is not allowlisted
- command times out
- command exits non-zero
- validation or protocol mismatch fails

Non-zero command exit is not retried by the coordinator. Retryable dispatch
failures are limited to transport errors, coordinator request timeout, and agent
`5xx` responses.

Typical terminal execution errors include:

```text
command "name" is not allowlisted
command timed out after 30s
command exited with code 1
```

Inspect terminal details with:

```bash
go run ./cmd/pmctl jobs inspect <job-id>
```

or:

```bash
go run ./cmd/pmctl --json jobs inspect <job-id>
```

Relevant fields include `status`, `exit_code`, `stdout`, `stderr`,
`stdout_truncated`, `stderr_truncated`, and `last_error`.

## Coordinator and Agent Responsibilities

Coordinator-owned behavior:

- validates job submissions
- stores job state
- owns scheduling and retry policy
- owns lifecycle transitions
- accepts matching terminal result reports from agents

Agent-owned behavior:

- registers with the coordinator
- sends heartbeats with metadata
- exposes `/execute`
- maps logical command keys to local executable paths
- enforces fixed execution timeout and bounded output capture
- keeps a bounded in-memory cache for best-effort terminal result reporting

`pmctl` is a thin client and does not own validation, scheduling, lifecycle,
storage, or result acceptance.

## Operator Checklist

Before enabling an agent for real private work:

- Review `AGENT_COMMAND_ALLOWLIST`.
- Confirm commands do not require shell expansion.
- Confirm arguments expected from operators are safe for the mapped command.
- Set `AGENT_EXEC_TIMEOUT` to a bounded value appropriate for the host.
- Decide whether the node should use mTLS trusted-LAN mode.
- Confirm operators understand that this is not strong sandboxing.
- Run a small known-safe command job and inspect the result.

For endpoint details and public job fields, see
[HTTP/JSON v0 API inventory](../api-http-json-v0.md).
