# Example Workloads

This directory contains tracked example workloads that can be built locally and
mapped through `AGENT_COMMAND_ALLOWLIST` as external executable targets.

These examples are not agent built-ins, workflow templates, packaging outputs,
or a file-transfer layer. Build outputs are local artifacts and should not be
committed.

The local release build helper also builds `text-stats` into generated dev
artifact layouts under `workloads/`. Those generated layouts and archives are
local artifacts and should not be committed.

## text-stats

`text-stats` is a small cross-platform Go helper that reads one agent-local
text file and prints stable line and word counts.

Build from the repository root:

```bash
go build -o /tmp/planetary-mesh-workloads/text-stats ./examples/workloads/text-stats
```

Map the built helper on the agent host:

```bash
AGENT_COMMAND_ALLOWLIST='text-stats=/tmp/planetary-mesh-workloads/text-stats'
```

Then submit the logical command key with an agent-local input path:

```bash
go run ./cmd/pmctl submit command text-stats /tmp/planetary-mesh-workloads/input.txt
```

See
[docs/runbooks/practical-workload-recipe.md](../../docs/runbooks/practical-workload-recipe.md)
for the full operator workflow.
