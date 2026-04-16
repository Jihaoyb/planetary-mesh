# ADR 0005: Use allowlisted direct command execution for the first real workload

- Status: Accepted
- Date: 2026-04-12

## Context

After PR #5, Planetary Mesh has a working control plane, but agent execution is
still a stub that sleeps and returns success. That is sufficient for wiring and
retry tests, but not for demonstrating real distributed work.

The next milestone needs a real workload model that is:

- Useful enough to prove remote execution works
- Safe enough for a trusted-LAN v0
- Simple enough to land without container orchestration, image management, or
  a full task graph model

## Decision

For the first real workload, we use **allowlisted direct command execution** on
the agent.

Details:

- Jobs use `type="command"` with:
  - `command` as the logical allowlist key
  - `args` as the argument vector
- Agents map logical command names to executable paths or names via explicit
  local allowlist configuration
- Agents execute commands with `exec.CommandContext`
- Agents do **not** invoke a shell
- Agents enforce a fixed execution timeout, default `30s`
- Stdout and stderr are captured separately and capped at `1 MiB` per stream
- Non-zero exit is reported as a terminal execution failure

Legacy `payload`-style jobs are not extended in this ADR. For `type="command"`,
non-empty `payload` is rejected rather than silently ignored.

## Alternatives Considered

- **Keep the stub execution path longer**
  - Pros: no execution safety work yet
  - Cons: the system remains a toy and cannot demonstrate useful work

- **Container execution first**
  - Pros: stronger isolation and repeatability
  - Cons: more operational weight, more moving parts, slower feedback

- **Allowlisted direct execution (chosen)**
  - Pros: simplest useful workload model, no shell injection path, easy to test
  - Cons: weaker isolation than containers, OS/tool availability matters

## Consequences

- Positive:
  - Planetary Mesh can now demonstrate real remote work on trusted nodes
  - The job/result model becomes concrete enough for persistence and CLI work
  - The allowlist keeps the v0 trust boundary narrow
- Negative:
  - Command availability is host-dependent
  - Output capture and timeout behavior must be bounded carefully
  - Per-job timeout override is deferred beyond this milestone
