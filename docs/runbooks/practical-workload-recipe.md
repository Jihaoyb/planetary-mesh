# Practical External Workload Recipe

This runbook documents the current real private workload pattern: build or
install a trusted executable on the agent host, map a logical command key to
that executable through `AGENT_COMMAND_ALLOWLIST`, submit the logical command
with `pmctl`, and inspect the result through the coordinator.

The tracked example workload is `text-stats`, a small cross-platform Go helper
under `examples/workloads/text-stats/`. It reads one agent-local text file and
prints stable text statistics:

```text
lines=3
non_empty_lines=3
words=3
```

Portable built-ins such as `builtin:echo`, `builtin:sleep`,
`builtin:false`, and `builtin:line-count` remain validation helpers. They are
not the product workflow extension model.

For the shorter first-run path, see
[First-Run Private Mesh Onboarding](first-run-private-mesh.md).

## Current Workflow Boundary

Planetary Mesh does not transfer files today. Workload inputs must already
exist on the selected agent host or be reachable by normal host-local means,
such as a mounted directory managed outside Planetary Mesh.

Scheduling is still first healthy node with retryable cross-node reassignment.
Reported capabilities are operator-visible only and do not select nodes. For
this recipe, run one eligible agent or prepare the same helper path and input
path on every healthy agent that might receive the job.

The `text-stats` logical command used below maps to an external executable path
in the agent allowlist. The submitted command name is the logical key
`text-stats`; clients do not submit executable paths.

Command execution remains allowlisted direct process execution with
`exec.CommandContext`. The agent does not invoke a shell, so shell features such
as redirection, glob expansion, command substitution, and pipelines are not
available inside the submitted job.

The helper is an example wrapper-style executable, not a workflow template
system, file-transfer contract, or stronger isolation boundary.

## Automated Local Smoke

From the repository root:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-workload ./examples/external_workload_smoke.sh
```

The script:

- builds temporary `coordinator`, `agent`, `pmctl`, and `text-stats` binaries
- starts one in-memory coordinator and one agent on local ports
- creates a temporary agent-local input file
- maps `text-stats` to the built helper path in `AGENT_COMMAND_ALLOWLIST`
- submits `text-stats <agent-local-input-path>` through `pmctl`
- verifies the job reaches `COMPLETED` with the expected stdout

Expected final output includes:

```text
External workload smoke completed successfully
```

The script prints its temporary log directory. Do not commit those logs, temp
inputs, or generated binaries.

## Build the Helper Manually

Build the helper on each agent host that should run this workload.

macOS/Linux:

```bash
mkdir -p /tmp/planetary-mesh-workloads
go build -o /tmp/planetary-mesh-workloads/text-stats ./examples/workloads/text-stats
```

Windows PowerShell:

```powershell
$dir = Join-Path $env:TEMP "planetary-mesh-workloads"
New-Item -ItemType Directory -Force $dir | Out-Null
go build -o (Join-Path $dir "text-stats.exe") ./examples/workloads/text-stats
```

The build output is local to the agent host. Do not commit generated helper
binaries.

## Agent-Local Input

Prepare a small text input file on the target agent host. Keep real workload
data outside commits.

macOS/Linux:

```bash
printf 'alpha\nbeta\ngamma\n' > /tmp/planetary-mesh-workloads/input.txt
```

Windows PowerShell:

```powershell
$dir = Join-Path $env:TEMP "planetary-mesh-workloads"
[System.IO.File]::WriteAllText((Join-Path $dir "input.txt"), "alpha`nbeta`ngamma`n")
```

Record paths in committed evidence only as placeholders:

```text
<agent-local-helper-path>
<agent-local-input-path>
```

## Local One-Machine Run

Start the coordinator:

```bash
go run ./cmd/coordinator --config config/coordinator.env.example
```

Start one agent with the external helper mapped into the allowlist:

```bash
NODE_ID=text-stats-agent \
AGENT_ADDR=:8081 \
AGENT_ADVERTISE_ADDR=http://localhost:8081 \
COORDINATOR_URL=http://localhost:8080 \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=/tmp/planetary-mesh-workloads/text-stats' \
AGENT_CAPABILITIES='profile:local,role:text-worker' \
go run ./cmd/agent
```

On Windows, use the `.exe` helper path in the `text-stats=<path>` mapping.

Submit the workload:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example --json submit command text-stats /tmp/planetary-mesh-workloads/input.txt
```

Record the returned job id, then inspect it:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example jobs inspect <job-id>
go run ./cmd/pmctl --config config/pmctl.env.example --json jobs inspect <job-id>
```

Expected result:

- `Status` is `COMPLETED`
- `Command` is `text-stats <agent-local-input-path>`
- `Node` is the agent that has the helper and input file
- `Attempts` is at least `1`
- `Stdout` is:

  ```text
  lines=3
  non_empty_lines=3
  words=3
  ```

- `Stderr` is empty
- truncation flags are `false`
- `Last Error` is empty or absent

## Two-Machine LAN Run

On the coordinator host:

```bash
COORDINATOR_ADDR=:<coordinator-port> \
go run ./cmd/coordinator
```

On the agent host, build `text-stats`, create the input file, and start the
agent:

```bash
NODE_ID=<node-id> \
AGENT_ADDR=:<agent-port> \
AGENT_ADVERTISE_ADDR=http://<agent-lan-host>:<agent-port> \
COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=<agent-local-helper-path>' \
AGENT_CAPABILITIES='profile:lan,role:text-worker' \
go run ./cmd/agent
```

From the operator client:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl nodes list

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json submit command text-stats <agent-local-input-path>
```

Inspect the returned `<job-id>` and expect the same completed result shape as
the local run.

## Failure Checks

| Symptom | Likely cause | Check |
|---|---|---|
| Job fails with command not allowlisted | Agent allowlist lacks the logical key `text-stats` | Inspect `AGENT_COMMAND_ALLOWLIST` on the agent host. |
| Job has retryable dispatch failures or agent `5xx` responses | The mapped helper path is missing, not executable, or not valid for that OS | Confirm the `text-stats=<agent-local-helper-path>` mapping and file permissions. |
| Job fails with non-zero exit and `text-stats:` in stderr | Input path is missing or unreadable on the executing agent | Inspect job `stderr` and confirm the file exists on the agent host. |
| Job runs on an unexpected node | Multiple healthy agents are available and scheduler is first-healthy-node | Use one eligible agent or install the helper and input path on every eligible agent. |
| Unexpected counts | The selected agent read different host-local content than expected | Compare submitted args with the actual agent-local input path and file contents. |

Non-zero helper exit is terminal and is not retried by the coordinator.
Transport errors and agent `5xx` responses remain retryable under the current
dispatch policy.

## Cleanup and Evidence

For local or LAN runs:

- stop agent and coordinator processes with `Ctrl-C`
- remove temporary helper binaries and input files when no longer needed
- remove copied local config files such as `config/agent-text-stats.env` if you
  created them
- keep generated logs and notes outside commits unless intentionally sanitized

Safe committed evidence should use placeholders and stable summaries:

```text
Practical external workload:
- command=text-stats <agent-local-input-path>
- allowlist=text-stats=<agent-local-helper-path>
- status=COMPLETED
- node=<node-id>
- attempts>=1
- stdout="lines=3\nnon_empty_lines=3\nwords=3\n"
```

Do not commit private IPs, private hostnames, credentials, certificates, keys,
local config files, generated binaries, raw logs, or real workload data.

## Safety Notes

- Keep command allowlists narrow and workflow-specific.
- Prefer small wrapper executables that validate arguments before calling
  broader local tools.
- Avoid allowlisting shells or broad interpreters for this recipe.
- Treat stdout and stderr as bounded result fields, not an artifact store.
- This is allowlisted direct host process execution, not strong sandboxing.
- Built-ins remain smoke/validation helpers; real private workflows should use
  external commands or wrappers until a future workflow/template layer is
  explicitly designed.
