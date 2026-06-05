# Practical Workload Recipe

This runbook documents a small trusted workload beyond the local smoke commands
`echo`, `sleep`, and `false`: counting lines in an agent-local text file with
the portable `builtin:line-count` validation target.

For a shorter first-run path that includes this workload after local smoke, see
[First-Run Private Mesh Onboarding](first-run-private-mesh.md).

The recipe is intentionally narrow. It validates that Planetary Mesh can run a
useful allowlisted command on a remote private machine and return the result
through the coordinator. It does not add file transfer, job templates, shell
execution, or stronger isolation.

## Current Workflow Boundary

Planetary Mesh does not transfer files today. Workload inputs must already
exist on the selected agent host or be reachable by normal host-local means
such as a mounted directory managed outside Planetary Mesh.

Scheduling is still first healthy node, with retryable cross-node reassignment.
Reported capabilities are operator-visible only and do not select nodes. For
this recipe, use one healthy agent or prepare the same input path and allowlist
on every healthy agent that might receive the job.

The `line-count` logical command used below maps to `builtin:line-count` in the
agent allowlist. The built-in is portable across macOS, Linux, and Windows, but
the input path is still host-local and must be valid on whichever agent receives
the job.

Command execution remains allowlisted direct process execution with
`exec.CommandContext`. The agent does not invoke a shell, so shell features such
as redirection, glob expansion, command substitution, and pipelines are not
available inside the submitted job.

## Agent-Local Input

On the target agent host, prepare a text input file outside the repository. Use
a path appropriate for the host and keep local data out of commits.

macOS/Linux example:

```bash
mkdir -p /tmp/planetary-mesh-lan-workload
printf 'alpha\nbeta\ngamma\n' > /tmp/planetary-mesh-lan-workload/input.txt
```

Windows PowerShell example:

```powershell
$dir = Join-Path $env:TEMP "planetary-mesh-lan-workload"
New-Item -ItemType Directory -Force $dir | Out-Null
Set-Content -Path (Join-Path $dir "input.txt") -Value @("alpha", "beta", "gamma")
```

Record the path as a placeholder in committed evidence:

```text
<agent-local-input-path>
```

Do not commit the input file unless it is intentionally created as a generic
tracked fixture in a future milestone.

## Allowlist Mapping

Configure the remote agent with an allowlist that includes the logical key
`line-count` mapped to the portable built-in target.

```bash
NODE_ID=<node-id> \
AGENT_ADDR=:<agent-port> \
AGENT_ADVERTISE_ADDR=http://<agent-lan-host>:<agent-port> \
COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count' \
AGENT_CAPABILITIES='profile:lan,role:text-worker' \
go run ./cmd/agent
```

The submitted command name is the logical allowlist key `line-count`, not an
arbitrary path or built-in target string supplied by the operator.

## Submit the Workload

From the operator client:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json submit command line-count <agent-local-input-path>
```

Record the returned job id as `<job-id>`.

Inspect the job:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl jobs inspect <job-id>
```

Expected human output shape:

```text
ID        <job-id>
Status    COMPLETED
Type      command
Command   line-count <agent-local-input-path>
Node      <node-id>
Attempts  1
Stdout    <line-count>
```

Use JSON output when capturing evidence:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json jobs inspect <job-id>
```

Expected JSON fields:

- `status` is `COMPLETED`
- `command` is `line-count`
- `args` includes `<agent-local-input-path>`
- `node_id` is the remote agent node id
- `attempts` is at least `1`
- `stdout` contains the line count
- `stderr` is empty for the happy path
- `stdout_truncated` and `stderr_truncated` are `false`
- `last_error` is empty

## Failure Checks

These checks are useful when the workload fails:

| Symptom | Likely cause | Check |
|---|---|---|
| Job fails with command not allowlisted | Agent allowlist does not include `line-count` | Inspect `AGENT_COMMAND_ALLOWLIST` on the agent host. |
| Job fails with non-zero exit | Input path is missing or unreadable on the executing agent | Inspect `stderr` and confirm the file exists on the agent host. |
| Job runs on an unexpected node | Multiple healthy agents are available and scheduler is first-healthy-node | Prepare the input on every eligible agent or run the recipe with one healthy agent. |
| Unexpected line count | The job executed against different host-local content than expected | Compare submitted args with the actual agent-local input path and file contents. |

Non-zero command exit is terminal and is not retried by the coordinator.
Transport errors and agent `5xx` responses remain retryable under the current
dispatch policy.

## Safety Notes

- Keep command allowlists narrow and workflow-specific.
- Prefer simple tools with predictable arguments.
- Avoid allowlisting shells or broad interpreters for this recipe.
- Treat `builtin:line-count` as a narrow validation helper, not a general file
  processing framework.
- Keep input data on trusted hosts and outside commits unless intentionally
  sanitized.
- Treat stdout and stderr as bounded result fields, not an artifact store.
- This is allowlisted direct host process execution, not strong sandboxing.

For the end-to-end multi-device validation workflow, use
[Real LAN Validation](real-lan-validation.md).
