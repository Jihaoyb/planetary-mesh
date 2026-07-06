# Workflow Templates

This runbook covers the current `pmctl` workflow template layer. Templates are
local JSON files that `pmctl` validates and expands into one existing
`type="command"` job.

Templates do not add coordinator-owned resources, agent-side registries, new
endpoints, storage tables, file transfer, artifact storage, scheduler policy,
secret management, job cancellation, or workflow orchestration.

## Current Boundary

The runtime unit is still an allowlisted command or wrapper executable on a
trusted agent host. A template only helps operators provide named parameters in
a repeatable way.

Current behavior:

- templates are loaded from explicit operator-provided file paths
- template files use JSON `version: 1`
- `pmctl templates validate <template-file>` validates a template locally
- `pmctl templates inspect <template-file>` shows local template metadata,
  parameters, and argument token structure
- `pmctl templates preview <template-file> --set name=value` expands parameter
  values locally without creating a job
- `pmctl submit template <template-file> --set name=value` expands the template
  and submits the existing command-job request shape
- template commands are logical allowlist keys, not executable paths, shell
  snippets, or `builtin:<name>` target strings
- template args are structured `literal` or `param` tokens
- parameter values expand to one argument vector element each
- the agent still enforces `AGENT_COMMAND_ALLOWLIST`

There is no interpolation syntax and no shell expansion.

## Template Shape

Example:

```json
{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {
      "name": "input_path",
      "description": "Agent-local text file path.",
      "required": true
    }
  ],
  "args": [
    {
      "param": "input_path"
    }
  ]
}
```

Required top-level fields:

- `version`: exactly `1`
- `name`: stable operator-facing template name
- `command`: logical allowlist key
- `args`: ordered argument tokens

Optional top-level fields:

- `description`
- `parameters`

Parameter names must use letters, numbers, `_`, or `-`, and start with a
letter. Optional parameters must declare a default. Required parameters do not
declare defaults.

Argument tokens must contain exactly one of:

```json
{"literal": "fixed-string"}
```

or:

```json
{"param": "parameter_name"}
```

## Validate a Template

From the repository root:

```bash
go run ./cmd/pmctl templates validate examples/templates/text-stats.pmtemplate.json
```

Expected human output:

```text
Template:    examples/templates/text-stats.pmtemplate.json
Status:      valid
Name:        text-stats
Version:     1
Command:     text-stats
Parameters:  input_path(required)
Args:        1
```

Use JSON output for automation:

```bash
go run ./cmd/pmctl --json templates validate examples/templates/text-stats.pmtemplate.json
```

Template validation is local. It does not contact the coordinator and does not
prove that any agent currently allowlists the command key.

## Inspect a Template

Inspect shows the template metadata, parameter table, and argument token
structure:

```bash
go run ./cmd/pmctl templates inspect examples/templates/text-stats.pmtemplate.json
```

Expected human output:

```text
Template:     examples/templates/text-stats.pmtemplate.json
Status:       valid
Name:         text-stats
Description:  Count text statistics for one agent-local file.
Version:      1
Command:      text-stats

Parameters:
NAME        REQUIRED  DEFAULT  DESCRIPTION
input_path  true      -        Agent-local text file path.

Args:
INDEX  TYPE   VALUE
1      param  input_path
```

Use JSON output for automation:

```bash
go run ./cmd/pmctl --json templates inspect examples/templates/text-stats.pmtemplate.json
```

Example JSON shape:

```json
{
  "valid": true,
  "path": "examples/templates/text-stats.pmtemplate.json",
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "version": 1,
  "command": "text-stats",
  "parameters": [
    {
      "name": "input_path",
      "description": "Agent-local text file path.",
      "required": true
    }
  ],
  "args": [
    {
      "index": 1,
      "type": "param",
      "value": "input_path"
    }
  ]
}
```

Inspection is local and does not contact the coordinator.

## Preview a Template

Preview expands operator-supplied parameter values locally and shows the command
job vector that submission would create:

```bash
go run ./cmd/pmctl templates preview examples/templates/text-stats.pmtemplate.json \
  --set input_path=/tmp/planetary-mesh-workloads/input.txt
```

Expected human output:

```text
Template:                examples/templates/text-stats.pmtemplate.json
Status:                  preview
Name:                    text-stats
Expanded Job Type:       command
Expanded Command:        text-stats
Creates Job:             false
Contacts Coordinator:    false
Checks Agent Allowlist:  false

Args:
INDEX  VALUE
1      "/tmp/planetary-mesh-workloads/input.txt"
```

Use JSON output for automation:

```bash
go run ./cmd/pmctl --json templates preview examples/templates/text-stats.pmtemplate.json \
  --set input_path=/tmp/planetary-mesh-workloads/input.txt
```

Example JSON shape:

```json
{
  "valid": true,
  "path": "examples/templates/text-stats.pmtemplate.json",
  "name": "text-stats",
  "expanded_job": {
    "type": "command",
    "command": "text-stats",
    "args": [
      "/tmp/planetary-mesh-workloads/input.txt"
    ]
  },
  "creates_job": false,
  "contacts_coordinator": false,
  "checks_agent_allowlist": false
}
```

Preview does not create a job, does not contact the coordinator, and does not
prove any agent currently allowlists `text-stats`. The expanded command and
args are separate vector elements, not a shell command line.

## Submit a Template

Prepare the underlying workload first. For `text-stats`, build or install the
helper on the agent host and map the logical command key:

```bash
AGENT_COMMAND_ALLOWLIST='text-stats=/tmp/planetary-mesh-workloads/text-stats'
```

Create or stage the input file on the target agent host:

```bash
printf 'alpha\nbeta\ngamma\n' > /tmp/planetary-mesh-workloads/input.txt
```

Submit:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example \
  submit template examples/templates/text-stats.pmtemplate.json \
  --set input_path=/tmp/planetary-mesh-workloads/input.txt
```

For automation:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example --json \
  submit template examples/templates/text-stats.pmtemplate.json \
  --set input_path=/tmp/planetary-mesh-workloads/input.txt
```

The submitted job is the same as:

```json
{
  "type": "command",
  "command": "text-stats",
  "args": ["/tmp/planetary-mesh-workloads/input.txt"]
}
```

Inspect the returned job with the existing job commands:

```bash
go run ./cmd/pmctl --config config/pmctl.env.example jobs inspect <job-id>
go run ./cmd/pmctl --config config/pmctl.env.example --json jobs inspect <job-id>
```

## Automated Smoke

Run the template smoke from the repository root:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-template ./examples/template_smoke.sh
```

The script:

- builds temporary coordinator, agent, `pmctl`, and `text-stats` binaries
- starts a local in-memory coordinator and one agent
- maps `text-stats` to the built helper path
- validates `examples/templates/text-stats.pmtemplate.json`
- previews the expanded command job without creating a job
- submits the template with an agent-local input path
- verifies the resulting command job reaches `COMPLETED`

Expected final output:

```text
Template smoke completed successfully
```

## Failure Checks

| Symptom | Likely cause | Check |
|---|---|---|
| `invalid template ... unknown field` | Template JSON includes unsupported fields | Use the v1 shape above and remove unsupported fields. |
| Missing required parameter | A required template parameter has no `--set` value | Pass `--set name=value` for every required parameter. |
| Unknown parameter | A `--set` name is not declared by the template | Compare the command line with the template `parameters` list. |
| Duplicate `--set` value | The same parameter was supplied more than once | Keep one value for each parameter. |
| Preview shows `checks_agent_allowlist=false` | Preview is local and cannot know which agent will run the job | Inspect `AGENT_COMMAND_ALLOWLIST` on eligible agents before submission. |
| Job fails with command not allowlisted | The selected agent does not allowlist the template command key | Inspect `AGENT_COMMAND_ALLOWLIST` on eligible agents. |
| Job fails in the wrapper | The expanded args refer to missing agent-local files or invalid wrapper inputs | Inspect job `stderr`, `last_error`, and the agent-local file path. |

## Safety Notes

- Templates are not sandboxing.
- Templates do not make untrusted workloads safe.
- Templates do not transfer input files or store large result artifacts.
- Keep command allowlists narrow and workflow-specific.
- Prefer wrappers that validate their own domain-specific arguments.
- Do not allowlist shells or broad interpreters just to make templates more
  flexible.
