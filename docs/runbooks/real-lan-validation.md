# Real LAN Validation Runbook

This runbook captures the manual validation workflow for proving Planetary Mesh
as a private/local mesh across real machines on the same LAN. It uses
placeholder values only. Do not commit private IP addresses, private hostnames,
certificates, keys, credentials, local env files, or machine-specific notes.

Milestone 18 is not complete until the validation evidence section is filled
with sanitized results from at least two physical machines.

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
AGENT_COMMAND_ALLOWLIST='echo=echo,false=false,sleep=sleep' \
AGENT_CAPABILITIES='profile:lan,role:worker' \
go run ./cmd/agent
```

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

- [ ] Coordinator ran on one physical machine.
- [ ] Agent ran on a different physical LAN machine.
- [ ] Operator ran `pmctl status` against the coordinator LAN address.
- [ ] `pmctl nodes list` showed the remote node as `HEALTHY`.
- [ ] An allowlisted command job dispatched across devices and reached
      `COMPLETED`.
- [ ] `pmctl jobs inspect <job-id>` showed the remote node id, attempts,
      terminal status, and stdout.
- [ ] Remote agent stop/restart behavior was observed.
- [ ] A job queued while no healthy agent was available later completed after
      agent restart.
- [ ] A practical workload beyond `echo`, `sleep`, and `false` was validated
      using [Practical Workload Recipe](practical-workload-recipe.md).
- [ ] Remaining limitations and friction were recorded.

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
- <job-id>: command=echo, status=COMPLETED, node=<node-id>, attempts=1, stdout="hello from real lan"

Failure/restart:
- remote agent stopped
- node observed as OFFLINE
- <job-id>: status=QUEUED while no healthy agent was available
- remote agent restarted
- <job-id>: status=COMPLETED after queued scheduler re-dispatch

Practical workload:
- command=wc -l <agent-local-input-path>
- status=COMPLETED
- stdout="<line-count> <agent-local-input-path>"

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
