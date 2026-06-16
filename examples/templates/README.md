# Example Templates

This directory contains tracked `pmctl` workflow template examples.

Templates are local operator files. They are not coordinator resources, an
agent-side registry, a file-transfer system, artifact storage, or a workflow
engine. `pmctl` validates a template, expands operator parameters into one
existing command job, and submits that job through the existing coordinator API.

The agent still enforces `AGENT_COMMAND_ALLOWLIST`. The template command field
is a logical allowlist key, not an executable path, shell snippet, or
`builtin:<name>` target string.

## text-stats.pmtemplate.json

`text-stats.pmtemplate.json` expands one `input_path` parameter into:

```text
text-stats <agent-local-input-path>
```

Use it with the tracked `examples/workloads/text-stats` helper after mapping the
logical `text-stats` command key to that helper on each eligible agent.

Validate:

```bash
go run ./cmd/pmctl templates validate examples/templates/text-stats.pmtemplate.json
```

Submit:

```bash
go run ./cmd/pmctl submit template examples/templates/text-stats.pmtemplate.json --set input_path=/tmp/planetary-mesh-workloads/input.txt
```
