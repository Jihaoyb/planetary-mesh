# Local Release Build and Install Smoke

This runbook covers the current pre-release local binary artifact workflow. It
builds coordinator, agent, `pmctl`, and the tracked `text-stats` external
workload plus example workflow templates into predictable install layouts, then
validates that the mesh runs from those files instead of `go run`.

This is Phase 2 install ergonomics work. It is not a production installer,
signed binary distribution, package-manager release, GitHub Release, Docker
image, remote private mesh, coordinator-owned template registry, file-transfer
layer, workflow engine, or stronger execution sandbox.

## Prerequisites

From the repository root:

- Go matching `go.mod`
- `bash`
- `curl`
- `awk`, `grep`, and `sed` for the smoke script

Do not commit generated release layouts, archives, local logs, local config
files, certs, keys, credentials, private hostnames, private IP addresses, or
machine-specific notes.

## Build Local Release Artifacts

Build the default dev artifact matrix:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-releasebuild \
go run ./tools/releasebuild --version dev --out dist
```

Expected generated artifacts:

```text
dist/planetary-mesh-dev-darwin-arm64/
dist/planetary-mesh-dev-darwin-arm64.tar.gz
dist/planetary-mesh-dev-darwin-amd64/
dist/planetary-mesh-dev-darwin-amd64.tar.gz
dist/planetary-mesh-dev-linux-amd64/
dist/planetary-mesh-dev-linux-amd64.tar.gz
dist/planetary-mesh-dev-linux-arm64/
dist/planetary-mesh-dev-linux-arm64.tar.gz
dist/planetary-mesh-dev-windows-amd64/
dist/planetary-mesh-dev-windows-amd64.zip
```

Use `--targets host` for a faster host-only local check:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-releasebuild \
go run ./tools/releasebuild --version dev --out dist --targets host
```

Use comma-separated `GOOS/GOARCH` entries for a narrower matrix:

```bash
go run ./tools/releasebuild --version dev --out dist --targets linux/amd64,windows/amd64
```

The `dev` label is an artifact naming label, not a stable release version or
tag. This milestone does not publish a GitHub Release.

## Artifact Layout

Each unpacked directory uses this layout:

```text
planetary-mesh-dev-<goos>-<goarch>/
  bin/
    coordinator
    agent
    pmctl
  workloads/
    text-stats
  templates/
    text-stats.pmtemplate.json
    README.md
  config/
    coordinator.env.example
    agent-1.env.example
    agent-2.env.example
    pmctl.env.example
  docs/
    runbooks/
      local-release-install.md
      practical-workload-recipe.md
      command-execution-safety.md
  README.md
```

Windows artifacts use `.exe` names:

```text
bin/coordinator.exe
bin/agent.exe
bin/pmctl.exe
workloads/text-stats.exe
```

The copied config files are examples. For personal runs, copy them to local
`*.env` files or override values with environment variables. Keep secrets and
machine-specific settings out of commits.

## Automated Installed-Binary Smoke

Run the installed-binary smoke from the repository root:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-release \
./examples/release_smoke.sh
```

The script:

- builds a temporary host release layout with `--targets host`
- starts installed `bin/coordinator`
- starts installed `bin/agent`
- maps installed `workloads/text-stats` through `AGENT_COMMAND_ALLOWLIST`
- creates a temporary agent-local input file
- validates and previews installed `templates/text-stats.pmtemplate.json`
- submits the installed template with `bin/pmctl submit template`
- verifies that the job reaches `COMPLETED`

Expected final output includes:

```text
Release smoke completed successfully
```

Expected job stdout:

```text
lines=3
non_empty_lines=3
words=3
```

The script prints its log directory. It removes the temporary release layout by
default. Set `KEEP_RELEASE_SMOKE=1` to preserve the temporary layout for
inspection.

## Manual Local Run From an Install Layout

Build a host layout:

```bash
go run ./tools/releasebuild --version dev --out dist --targets host
```

Use the generated host directory for your platform. On macOS arm64, for example:

```bash
cd dist/planetary-mesh-dev-darwin-arm64
```

Start the coordinator:

```bash
./bin/coordinator --config config/coordinator.env.example
```

Start one agent in another terminal:

```bash
./bin/agent --config config/agent-1.env.example
```

Inspect with `pmctl`:

```bash
./bin/pmctl --config config/pmctl.env.example status
./bin/pmctl --config config/pmctl.env.example nodes list
```

Expected result:

- coordinator status is `ok`
- protocol version is `1`
- storage is `in_memory`
- `local-agent-1` is `HEALTHY`

To run the installed `text-stats` external workload, use one eligible agent and
override the allowlist with the installed helper path.

macOS/Linux:

```bash
printf 'alpha\nbeta\ngamma\n' > /tmp/planetary-mesh-release-input.txt

NODE_ID=text-stats-release-agent \
AGENT_ADDR=:8081 \
AGENT_ADVERTISE_ADDR=http://localhost:8081 \
COORDINATOR_URL=http://localhost:8080 \
AGENT_COMMAND_ALLOWLIST="echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=$(pwd)/workloads/text-stats" \
AGENT_CAPABILITIES='profile:local-release,role:text-worker' \
./bin/agent
```

Windows PowerShell:

```powershell
$inputPath = Join-Path $env:TEMP "planetary-mesh-release-input.txt"
[System.IO.File]::WriteAllText($inputPath, "alpha`nbeta`ngamma`n")
$workload = Join-Path (Get-Location) "workloads\text-stats.exe"
$env:NODE_ID = "text-stats-release-agent"
$env:AGENT_ADDR = ":8081"
$env:AGENT_ADVERTISE_ADDR = "http://localhost:8081"
$env:COORDINATOR_URL = "http://localhost:8080"
$env:AGENT_COMMAND_ALLOWLIST = "echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=$workload"
$env:AGENT_CAPABILITIES = "profile:local-release,role:text-worker"
.\bin\agent.exe
```

Validate, inspect, preview, and submit the installed template, then inspect the
expanded job:

```bash
./bin/pmctl --config config/pmctl.env.example templates validate templates/text-stats.pmtemplate.json
./bin/pmctl --config config/pmctl.env.example templates inspect templates/text-stats.pmtemplate.json
./bin/pmctl --config config/pmctl.env.example templates preview templates/text-stats.pmtemplate.json --set input_path=/tmp/planetary-mesh-release-input.txt
./bin/pmctl --config config/pmctl.env.example --json submit template templates/text-stats.pmtemplate.json --set input_path=/tmp/planetary-mesh-release-input.txt
./bin/pmctl --config config/pmctl.env.example jobs inspect <job-id>
```

On Windows, pass the Windows input path you created with PowerShell.

Expected result:

- `Status` is `COMPLETED`
- `Command` is `text-stats <agent-local-input-path>`
- `Node` is the agent that has the helper and input file
- `Attempts` is at least `1`
- `Stdout` is the three-line `text-stats` output above
- `Stderr` is empty
- truncation flags are `false`
- `Last Error` is empty or absent

## Optional Two-Machine LAN Run

Use matching artifacts for each host OS and CPU architecture.

On the coordinator host:

```bash
COORDINATOR_ADDR=:<coordinator-port> \
./bin/coordinator
```

On the agent host, start the installed agent with an installed `text-stats`
mapping:

```bash
NODE_ID=<node-id> \
AGENT_ADDR=:<agent-port> \
AGENT_ADVERTISE_ADDR=http://<agent-lan-host>:<agent-port> \
COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=<install-dir>/workloads/text-stats' \
AGENT_CAPABILITIES='profile:lan-release,role:text-worker' \
./bin/agent
```

On Windows, use `<install-dir>\workloads\text-stats.exe` in the allowlist.

From an operator host with a matching `pmctl` binary:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
./bin/pmctl nodes list

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
./bin/pmctl templates validate templates/text-stats.pmtemplate.json

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
./bin/pmctl templates inspect templates/text-stats.pmtemplate.json

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
./bin/pmctl templates preview templates/text-stats.pmtemplate.json --set input_path=<agent-local-input-path>

PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
./bin/pmctl --json submit template templates/text-stats.pmtemplate.json --set input_path=<agent-local-input-path>
```

Scheduling is still first healthy node with retryable cross-node reassignment.
For this recipe, run one eligible agent or install the helper and input path on
every healthy agent that might receive the job.

Record committed evidence only with placeholders such as
`<coordinator-lan-host>`, `<agent-lan-host>`, `<node-id>`,
`<agent-local-input-path>`, and `<install-dir>`.

## Failure Handling

| Symptom | Likely cause | Check |
|---|---|---|
| Release helper fails while building | Missing Go toolchain or unsupported local environment | Run `go version` and retry with `GOCACHE` under `/private/tmp` if needed. |
| Expected binary is missing | Wrong target or output directory | Confirm `--out`, `--targets`, and the generated `planetary-mesh-dev-<goos>-<goarch>` directory. |
| Coordinator health check fails | Port unavailable, firewall block, or coordinator startup error | Inspect the coordinator log printed by the smoke script. |
| Agent does not become `HEALTHY` | Wrong `COORDINATOR_URL`, `AGENT_ADVERTISE_ADDR`, or blocked agent port | Inspect agent logs and `pmctl nodes list`. |
| Job fails with command not allowlisted | `text-stats` logical key is missing or points at the wrong path | Check `AGENT_COMMAND_ALLOWLIST` on the selected agent. |
| Job has retryable dispatch failures or agent `5xx` responses | Installed helper path is missing, not executable, or invalid for that OS | Confirm the helper exists under `workloads/` and has the expected `.exe` suffix on Windows. |
| Job fails with `text-stats:` in stderr | Input file is missing or unreadable on the executing agent | Confirm the submitted path exists on that agent host. |
| Template validation fails | The copied template was edited or the wrong file path was used | Validate `templates/text-stats.pmtemplate.json` from the install layout. |
| Template preview looks correct but submit fails | Preview is local and does not check installed agent allowlists or agent-local files | Confirm `text-stats=<install-dir>/workloads/text-stats[.exe]` and the submitted input path on the selected agent. |

Non-zero helper exit is terminal and is not retried by the coordinator.
Transport errors and agent `5xx` responses remain retryable under the current
dispatch policy.

## Cleanup and Artifact Rules

Generated layouts and archives are local artifacts. They are ignored by git and
must not be committed.

For local runs:

- stop coordinator and agent processes with `Ctrl-C`
- remove temporary input files when no longer needed
- remove generated `dist/` output when done
- keep logs outside commits unless intentionally sanitized

Safe committed evidence should use summaries:

```text
Local release smoke:
- artifact=planetary-mesh-dev-<goos>-<goarch>
- command=text-stats <agent-local-input-path>
- allowlist=text-stats=<install-dir>/workloads/text-stats[.exe]
- status=COMPLETED
- node=<node-id>
- attempts>=1
- stdout="lines=3\nnon_empty_lines=3\nwords=3\n"
```

Do not commit generated binaries, archives, `dist/`, local logs, local config
files, certs, keys, credentials, private hostnames, private IP addresses, raw
machine-specific notes, or real workload data.

## Safety Notes

- Built-ins remain portable validation helpers only.
- Real private workflows should use explicit allowlisted external commands or
  wrapper executables, as `text-stats` does here.
- The agent never invokes a shell for command jobs.
- Command output remains bounded result text, not an artifact store.
- This is allowlisted direct host process execution on trusted machines, not
  strong sandbox, container, VM, or multi-tenant isolation.
- mTLS remains opt-in with manual certificate generation, distribution,
  enrollment, and rotation.
