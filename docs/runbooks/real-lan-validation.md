# Real LAN Validation Runbook

This runbook captures the manual validation workflow for proving Planetary Mesh
as a private/local mesh across real machines on the same LAN. It uses
placeholder values only. Do not commit private IP addresses, private hostnames,
certificates, keys, credentials, local env files, or machine-specific notes.

If this is your first source-based run, start with
[First-Run Private Mesh Onboarding](first-run-private-mesh.md). This runbook is
the deeper validation and evidence-capture reference.

Milestone 18 completion evidence was captured on 2026-05-26 from manual
multi-device LAN validation across macOS, Linux, and Windows coordinator/agent
pairs. The evidence below is intentionally sanitized and uses placeholders.

## Hardware and Network Assumptions

Required topology:

| Role | Requirement |
|---|---|
| Coordinator host | One physical machine running `cmd/coordinator` and reachable on the LAN. |
| Agent host | A different physical machine on the same LAN running `cmd/agent`. |
| Operator client | The coordinator host or another trusted LAN client running `pmctl`. |

Network assumptions:

- The coordinator binds to a LAN-reachable address such as `:<coordinator-port>`.
- The remote agent advertises an address the coordinator can reach, such as
  `http://<agent-lan-host>:<agent-port>`.
- The remote agent can reach
  `http://<coordinator-lan-host>:<coordinator-port>`.
- Host firewalls allow inbound coordinator traffic on `<coordinator-port>` and
  inbound agent traffic on `<agent-port>`.
- Plain HTTP is acceptable only for this trusted LAN validation. Use the
  [mTLS Trusted LAN](mtls-trusted-lan.md) runbook when validating secure mode.

Use placeholders in committed notes:

```text
<coordinator-lan-host>
<agent-lan-host>
<coordinator-port>
<agent-port>
<node-id>
<job-id>
```

## Coordinator Startup

On the coordinator host, from the repository root:

```bash
COORDINATOR_ADDR=:<coordinator-port> \
go run ./cmd/coordinator
```

Confirm the coordinator health endpoint from the coordinator host:

```bash
curl http://localhost:<coordinator-port>/healthz
```

Confirm the coordinator health endpoint from the operator client:

```bash
curl http://<coordinator-lan-host>:<coordinator-port>/healthz
```

Expected response:

```text
ok
```

Health endpoints are unversioned. All other coordinator control-plane
endpoints require `X-Planetary-Protocol-Version: 1`; `pmctl` sends that header
automatically.

## Remote Agent Startup

On the remote agent host, from the repository root:

```bash
NODE_ID=<node-id> \
AGENT_ADDR=:<agent-port> \
AGENT_ADVERTISE_ADDR=http://<agent-lan-host>:<agent-port> \
COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
AGENT_COMMAND_ALLOWLIST='echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count' \
AGENT_CAPABILITIES='profile:lan,role:worker' \
go run ./cmd/agent
```

The submitted command names remain logical allowlist keys such as `echo`,
`sleep`, and `line-count`. The `builtin:<name>` values are explicit portable
no-shell validation targets. They are not platform shell built-ins and cannot
run unless mapped in `AGENT_COMMAND_ALLOWLIST`.

Confirm the agent health endpoint from the agent host:

```bash
curl http://localhost:<agent-port>/healthz
```

Confirm the agent health endpoint from the coordinator host:

```bash
curl http://<agent-lan-host>:<agent-port>/healthz
```

Expected response:

```text
ok
```

If the agent registers but jobs do not dispatch, verify
`AGENT_ADVERTISE_ADDR`. It must be reachable by the coordinator, not just by
the agent itself.

## Operator Inspection With pmctl

From the operator client:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl status
```

Expected status summary:

```text
STATUS  PROTOCOL  STORAGE    SECURE  NODE_ALLOWLIST  DISPATCH                         RECONCILIATION
ok      1         in_memory  false   false           attempts=3 timeout=10s backoff=500ms  -
```

List nodes:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl nodes list
```

Expected node summary:

```text
ID         STATE    ACTIVE  CAPABILITIES             ADDRESS                              LAST_SEEN             CERTIFICATE
<node-id>  HEALTHY  0       profile:lan,role:worker  http://<agent-lan-host>:<agent-port>  <timestamp>           -
```

Use JSON output for evidence capture:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json nodes list
```

Sanitize timestamps only if needed for privacy. Do not commit private hostnames
or private IP addresses.

## Cross-Device Command Dispatch

Submit an allowlisted command job from the operator client:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl --json submit command echo "hello from real lan"
```

This uses the logical command key `echo`, which the agent allowlist above maps
to `builtin:echo`.

Record the returned job id as `<job-id>`.

List jobs:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl jobs list
```

Inspect the completed job:

```bash
PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
go run ./cmd/pmctl jobs inspect <job-id>
```

Expected result:

- `Status` is `COMPLETED`.
- `Node` is `<node-id>`.
- `Attempts` is at least `1`.
- `Stdout` is `hello from real lan`.
- `Last Error` is absent.

## Historical Validation Finding: Windows Command Portability

Partial real LAN validation on 2026-05-25 used a macOS coordinator/operator
host and a Windows remote agent host.

Observed working behavior:

- the coordinator started on the macOS host
- the Windows agent started and reached the coordinator
- registration and heartbeats worked
- `pmctl` submitted a job to the coordinator over the LAN
- coordinator dispatch reached the Windows agent

The validation job did not complete because the previous default example
mapping `echo=echo` is not portable to Windows. Planetary Mesh uses
`exec.CommandContext` directly and intentionally does not invoke a shell. On
Windows, `echo` is usually a shell built-in rather than a standalone
`echo.exe`, so the agent cannot execute it through a no-shell allowlist entry.

Sanitized failure shape:

```text
Dispatch:
- coordinator host: physical machine A, macOS, operator and coordinator role
- agent host: physical machine B, Windows, remote agent role
- <job-id>: command=echo, status=FAILED, node=<node-id>, attempts=3
- last_error: exec: "echo": executable file not found in %PATH%

Agent log:
- execute internal error: exec: "echo": executable file not found in %PATH%
```

Interpretation:

- basic LAN networking, registration, heartbeat, `pmctl` submission, and
  dispatch reached the remote machine
- the blocker is command portability for validation examples, not the basic
  coordinator-agent LAN path
- the no-shell execution model is behaving as designed

Milestone 19 added portable no-shell validation built-ins. This runbook now
uses `builtin:echo`, `builtin:sleep`, and `builtin:line-count` through explicit
allowlist mappings to avoid platform shell built-ins during validation.

This partial finding is retained because it explains why the portable built-in
validation targets are used below. It has been superseded by the completed
2026-05-26 validation evidence.

## Failure and Restart Observation

Use this flow with a single healthy agent. If another healthy agent remains
registered, the coordinator may dispatch queued work to that other node.

1. Stop the remote agent process on the agent host.
2. Keep the coordinator running.
3. Wait until the coordinator health checker marks the node `OFFLINE`. The
   current implementation marks stale nodes `SUSPECT` after roughly 15 seconds
   and `OFFLINE` after roughly 30 seconds, checked periodically.
4. Confirm the node state from the operator client:

   ```bash
   PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
   go run ./cmd/pmctl nodes list
   ```

5. Submit a job while no healthy agent is available:

   ```bash
   PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
   go run ./cmd/pmctl --json submit command echo "queued while agent offline"
   ```

6. Inspect the job and confirm it remains `QUEUED` with no completed result:

   ```bash
   PMCTL_COORDINATOR_URL=http://<coordinator-lan-host>:<coordinator-port> \
   go run ./cmd/pmctl jobs inspect <job-id>
   ```

7. Restart the remote agent with the same `NODE_ID`, `AGENT_ADVERTISE_ADDR`,
   and `COORDINATOR_URL`.
8. Confirm the node returns to `HEALTHY`.
9. Wait for the queued-job scheduler to re-dispatch the job, then inspect it
   again and confirm it reaches `COMPLETED`.

Expected observation:

- Agent stop causes the remote node to move from `HEALTHY` to `SUSPECT` and
  then `OFFLINE`.
- A job submitted with no healthy agents remains `QUEUED`.
- Restarting the remote agent returns it to `HEALTHY`.
- The queued job is later dispatched and completed by the remote agent.

This validates basic LAN failure/restart behavior only. It does not validate
full in-progress execution recovery, durable agent result history, or
multi-coordinator high availability.

## Sanitized Evidence Checklist

Capture evidence as sanitized text in the milestone docs or PR description.
Use placeholders for network identifiers.

Required evidence:

For a cross-OS validation matrix, repeat the evidence block for each tested
coordinator/agent OS pair. At minimum, committed completion evidence must show
the coordinator and agent running on different physical LAN machines.

- [x] Coordinator ran on one physical machine.
- [x] Agent ran on a different physical LAN machine.
- [x] Operator ran `pmctl status` against the coordinator LAN address.
- [x] `pmctl nodes list` showed the remote node as `HEALTHY`.
- [x] An allowlisted command job dispatched across devices and reached
      `COMPLETED`.
- [x] `pmctl jobs inspect <job-id>` showed the remote node id, attempts,
      terminal status, and stdout.
- [x] Remote agent stop/restart behavior was observed.
- [x] A job queued while no healthy agent was available later completed after
      agent restart.
- [x] A practical workload beyond `echo`, `sleep`, and `false` was validated
      using [Practical Workload Recipe](practical-workload-recipe.md).
- [x] Remaining limitations and friction were recorded.

## Captured Sanitized Evidence

Validation date: 2026-05-26.

Validation scope:

- coordinator and agent ran on different physical machines on the same LAN
- operator used `pmctl` against the coordinator LAN address
- agent allowlist used
  `echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count`
- all evidence below uses placeholders instead of private addresses, hostnames,
  usernames, certificates, credentials, or raw local notes

Validated OS matrix:

| Coordinator host | Agent host | Result |
|---|---|---|
| macOS physical machine | Windows physical machine | Passed |
| macOS physical machine | Linux physical machine | Passed |
| Windows physical machine | macOS physical machine | Passed |
| Windows physical machine | Linux physical machine | Passed |
| Linux physical machine | macOS physical machine | Passed |
| Linux physical machine | Windows physical machine | Passed |

For each coordinator/agent pair:

```text
Topology:
- Coordinator host: physical machine A, <coordinator-os>/<arch>, listening on <coordinator-port>
- Agent host: physical machine B, <agent-os>/<arch>, listening on <agent-port>
- Operator client: coordinator host or trusted LAN client

Status:
- pmctl status: status=ok protocol=1 storage=in_memory secure=false

Nodes:
- <node-id>: HEALTHY, address=http://<agent-lan-host>:<agent-port>, capabilities=profile:lan,role:worker

Portable command dispatch:
- <job-id>: command=echo, allowlist=echo=builtin:echo, status=COMPLETED, node=<node-id>, attempts>=1, stdout="hello from real lan"

Practical workload:
- <job-id>: command=line-count <agent-local-input-path>, allowlist=line-count=builtin:line-count, status=COMPLETED, node=<node-id>, stdout="3"

Failure/restart:
- remote agent stopped
- node observed as OFFLINE
- <job-id>: command=echo, status=QUEUED while no healthy agent was available
- remote agent restarted with the same NODE_ID and advertised address
- <job-id>: status=COMPLETED after queued scheduler re-dispatch
```

Validation outcome:

- coordinator-to-agent registration and heartbeat worked across each OS pair
- `pmctl` inspected remote status, node, job, and result state over the LAN
- allowlisted `builtin:echo` command dispatch completed across devices
- practical `builtin:line-count` workload completed against an agent-local
  input file
- basic stop/restart behavior was observed: the node became `OFFLINE`, a job
  stayed `QUEUED` while no healthy agent was available, and the job completed
  after the agent restarted
- no private LAN addresses, private hostnames, credentials, certificates, keys,
  local env files, or raw machine-specific notes were committed

Suggested sanitized evidence shape:

```text
Topology:
- Coordinator host: physical machine A, <os>/<arch>, listening on <coordinator-port>
- Agent host: physical machine B, <os>/<arch>, listening on <agent-port>
- Operator client: coordinator host or physical machine C

Status:
- pmctl status: status=ok protocol=1 storage=in_memory secure=false

Nodes:
- <node-id>: HEALTHY, address=http://<agent-lan-host>:<agent-port>, capabilities=profile:lan,role:worker

Dispatch:
- <job-id>: command=echo, allowlist=echo=builtin:echo, status=COMPLETED, node=<node-id>, attempts=1, stdout="hello from real lan"

Failure/restart:
- remote agent stopped
- node observed as OFFLINE
- <job-id>: status=QUEUED while no healthy agent was available
- remote agent restarted
- <job-id>: status=COMPLETED after queued scheduler re-dispatch

Practical workload:
- command=line-count <agent-local-input-path>
- allowlist=line-count=builtin:line-count
- status=COMPLETED
- stdout="<line-count>"

Limitations:
- plain HTTP validation only unless mTLS was explicitly configured
- no file transfer; workload input existed on the agent host
- no strong sandboxing
- manual process startup and firewall setup
```

## Cleanup

Stop the agent and coordinator processes with `Ctrl-C` in their terminals. If
local env files, certificates, logs, or notes were created during validation,
keep them outside commits unless they have been intentionally sanitized.
