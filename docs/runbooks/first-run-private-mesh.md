# First-Run Private Mesh Onboarding

This runbook is the source-based path from a fresh checkout to a working
private mesh. It starts with the local smoke workflow, then shows the same
operator model across two LAN machines with portable validation commands beyond
`echo`.

It documents behavior that exists today. It does not add packaging, file
transfer, remote private mesh, automated certificate lifecycle, a dashboard, or
stronger execution isolation.

## Prerequisites

From a fresh checkout, use a shell from the repository root and confirm these
tools are available:

- Go matching `go.mod`
- `bash`
- `curl`
- `python3`

For two-machine LAN onboarding, also prepare:

- one coordinator host and one different agent host on the same trusted LAN
- inbound firewall access for the chosen coordinator and agent ports
- the same repository checkout on both machines
- placeholder-based notes for evidence, not private addresses or hostnames

Do not commit local config files, private IP addresses, private hostnames,
certificates, keys, credentials, local workload inputs, raw logs, or
machine-specific LAN notes.

## 1. Run the Local Smoke Path

From the repository root:

```bash
./examples/demo.sh
```

The script uses tracked config examples:

- `config/coordinator.env.example`
- `config/agent-1.env.example`
- `config/agent-2.env.example`
- `config/pmctl.env.example`

Expected result:

```text
Smoke demo completed successfully
Logs are in <local-temp-log-dir>
```

During the run, the output should show:

- coordinator status is `ok`
- protocol version is `1`
- storage is `in_memory`
- `local-agent-1` and `local-agent-2` are `HEALTHY`
- the submitted `echo` command reaches `COMPLETED`
- job stdout is `hello from planetary mesh`

The script starts local processes, cleans them up on exit, removes temporary
binaries, and leaves logs under the printed temp directory for inspection.
In-memory coordinator state is gone when the coordinator exits.

If the smoke fails, inspect the printed log directory first. Common causes are
missing prerequisites, ports `8080`, `8081`, or `8082` already in use, or local
environment variables overriding the tracked examples.

## 2. Run the Pieces Manually on One Machine

Use the tracked examples when you want to see the components directly.

Start the coordinator:

```bash
go run ./cmd/coordinator --config config/coordinator.env.example
```

Start agent 1 in another terminal:

```bash
go run ./cmd/agent --config config/agent-1.env.example
```

Start agent 2 in another terminal:

```bash
go run ./cmd/agent --config config/agent-2.env.example
```

Check unversioned health endpoints:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
curl http://localhost:8082/healthz
```

Each should return:

```text
ok
```

Inspect with `pmctl`:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example status
go run ./cmd/pmctl --config config/pmctl.env.example nodes list
```

Expected node result:

```text
local-agent-1  HEALTHY
local-agent-2  HEALTHY
```

Submit and inspect a command job:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example submit command echo "hello mesh"
go run ./cmd/pmctl --config config/pmctl.env.example jobs list
go run ./cmd/pmctl --config config/pmctl.env.example jobs inspect job-1
```

Expected job result:

- `Status` is `COMPLETED`
- `Command` is `echo hello mesh`
- `Attempts` is at least `1`
- `Stdout` is `hello mesh`
- `Last Error` is empty or absent

For precise output in scripts or evidence, use JSON:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example --json nodes list
go run ./cmd/pmctl --config config/pmctl.env.example --json jobs inspect job-1
```

## 3. Run a Portable Agent-Local Validation Workload

The portable validation workload is `line-count`, which counts lines in one
agent-local text file. This proves the mesh can run an allowlisted action
against data on a trusted agent host without adding file transfer.

For the current real private workflow pattern using an external executable or
wrapper, use [Practical External Workload Recipe](practical-workload-recipe.md)
after this first-run path.

`line-count` must be explicitly mapped in `AGENT_COMMAND_ALLOWLIST`. The
submitted command name is the logical key `line-count`; clients do not submit
an executable path or a `builtin:<name>` target directly.

On the agent host, create a small local input file outside the repository:

macOS/Linux:

```bash
mkdir -p /tmp/planetary-mesh-first-run
printf 'alpha\nbeta\ngamma\n' > /tmp/planetary-mesh-first-run/input.txt
```

Windows PowerShell:

```powershell
$dir = Join-Path $env:TEMP "planetary-mesh-first-run"
New-Item -ItemType Directory -Force $dir | Out-Null
Set-Content -Path (Join-Path $dir "input.txt") -Value @("alpha", "beta", "gamma")
```

Start an agent with `line-count` in the allowlist. For a local one-machine
trial, stop any existing local agents first, then run:

```bash
NODE_ID=first-run-agent \
AGENT_ADDR=:8081 \
AGENT_ADVERTISE_ADDR=http://localhost:8081 \
COORDINATOR_URL=http://localhost:8080 \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count' \
AGENT_CAPABILITIES='profile:local,role:text-worker' \
go run ./cmd/agent
```

Submit the workload:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example --json submit command \
  --require-capability role:text-worker \
  line-count /tmp/planetary-mesh-first-run/input.txt
```

Record the returned job id, then inspect it:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example jobs inspect <job-id>
```

Expected result:

- `Status` is `COMPLETED`
- `Command` is `line-count /tmp/planetary-mesh-first-run/input.txt`
- `Node` is the agent that has the input file
- `Stdout` is `3`
- `Stderr` is empty
- truncation flags are `false`
- `Last Error` is empty or absent

The requirement routes only to a `HEALTHY` agent reporting
`role:text-worker`. It does not prove that `line-count` is allowlisted or that
the input path exists. If multiple matching agents are registered, prepare the
input on each one or use more specific operator-managed labels.

## 4. Run Across Two LAN Machines

Use placeholders in notes and docs:

```text
<coordinator-lan-host>
<agent-lan-host>
<coordinator-port>
<agent-port>
<node-id>
<job-id>
<agent-local-input-path>
```

On the coordinator host:

```bash
COORDINATOR_ADDR=:<coordinator-port> \
go run ./cmd/coordinator
```

Verify from the coordinator host and from the operator client:

```bash
curl http://localhost:<coordinator-port>/healthz
curl http://<coordinator-lan-host>:<coordinator-port>/healthz
```

Expected response:

```text
ok
```

On the agent host, create the `line-count` input file using the earlier
agent-local input steps. Then start the agent:

```bash
NODE_ID=<node-id> \
AGENT_ADDR=:<agent-port> \
AGENT_ADVERTISE_ADDR=http://<agent-lan-host>:<agent-port> \
COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count' \
AGENT_CAPABILITIES='profile:lan,role:text-worker' \
go run ./cmd/agent
```

Verify the agent health endpoint from the agent host and coordinator host:

```bash
curl http://localhost:<agent-port>/healthz
curl http://<agent-lan-host>:<agent-port>/healthz
```

From the operator client, inspect the mesh:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl status

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl nodes list
```

Expected summary:

```text
STATUS  ok
PROTOCOL  1
STORAGE  in_memory
<node-id>  HEALTHY  profile:lan,role:text-worker
```

Submit a basic cross-device command:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json submit command echo "hello from first lan"
```

Inspect the returned `<job-id>`:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl jobs inspect <job-id>
```

Expected result:

- `Status` is `COMPLETED`
- `Node` is `<node-id>`
- `Stdout` is `hello from first lan`

Submit the portable validation workload:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json submit command \
  --require-capability role:text-worker \
  line-count <agent-local-input-path>
```

Inspect the returned `<job-id>` and expect:

- `Status` is `COMPLETED`
- `Command` is `line-count <agent-local-input-path>`
- `Node` is `<node-id>`
- `Stdout` is `3`
- `Last Error` is empty or absent

## Cleanup

For local or LAN source-based runs:

- stop agent and coordinator processes with `Ctrl-C`
- remove temporary workload input files when no longer needed
- remove copied local config files such as `config/coordinator.env` or
  `config/agent-1.env` if you created them
- keep generated logs and notes outside commits unless intentionally sanitized

For the local smoke script, the printed temp log directory is safe to delete
after inspection.

## Common Failure Handling

| Symptom | Likely cause | First check |
|---|---|---|
| Health check fails | Process is not running or port is blocked/in use | `curl .../healthz` and process logs |
| `pmctl` gets protocol conflict | Raw `curl` command missed `X-Planetary-Protocol-Version: 1` | Use `pmctl` or add the protocol header |
| Node missing or `OFFLINE` | Agent cannot reach coordinator or advertised address is unreachable | `COORDINATOR_URL`, `AGENT_ADVERTISE_ADDR`, firewall rules |
| Job stays `QUEUED` | No healthy node matches every requirement | Compare job `Required Capabilities` with `pmctl nodes list` |
| `line-count` not allowlisted | Agent allowlist lacks `line-count=builtin:line-count` | Agent startup config |
| `line-count` exits non-zero | Input path is missing or unreadable on the selected agent | Job `stderr` and agent-local file path |
| External helper fails with `text-stats:` in stderr | Input path is missing or unreadable on the selected agent | Job `stderr` and the [Practical External Workload Recipe](practical-workload-recipe.md) |
| Job runs on unexpected matching node | Multiple matching agents report equivalent or lower load | Prepare every matching agent or use a narrower capability label; node ID breaks equal-load ties |

For deeper troubleshooting, use [Troubleshooting](troubleshooting.md).

## Current Limits to Keep in Mind

- Plain HTTP is the default for local development and trusted LAN onboarding.
- mTLS is available, but certificate generation, distribution, enrollment, and
  rotation are manual.
- Command execution is allowlisted direct execution on trusted hosts. It is not
  strong sandboxing.
- The agent does not invoke a shell. Shell features such as pipes,
  redirection, glob expansion, and command substitution are not available
  inside submitted jobs.
- Planetary Mesh does not transfer files today. Workload inputs must already
  exist on the selected agent host.
- Built-ins such as `echo`, `sleep`, `false`, and `line-count` are explicit
  validation helpers. They are not a product workflow extension model.
- Real private workflows should use explicit allowlisted external commands or
  wrapper executables such as the tracked `text-stats` example. `pmctl`
  workflow templates can make those approved wrapper invocations repeatable,
  but this first-run path does not require them.
- There is no production image, packaged release workflow, dashboard, remote
  private mesh, shared pool, marketplace, payment system, or cancellation API.

Use the [Local Private Mesh](local-private-mesh.md),
[Practical External Workload Recipe](practical-workload-recipe.md),
[Workflow Templates](workflow-templates.md), and
[Real LAN Validation](real-lan-validation.md) runbooks for more detailed
operator procedures after first-run onboarding.
