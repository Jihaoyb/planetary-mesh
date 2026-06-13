# ADR 0015: Define private workflow template model

- Status: Accepted
- Date: 2026-06-13

## Context

Phase 2 has made the private mesh easier to try and validate:

- Milestone 21 documented source-based first-run onboarding.
- Milestone 22 added the tracked external `text-stats` workload helper and
  wrapper-pattern runbook.
- Milestone 23 added pre-release local binary artifact generation and
  installed-binary smoke validation for coordinator, agent, `pmctl`, and
  `text-stats`.

The remaining usability gap is that repeatable private workflows still require
operators to remember direct command keys, argument order, and agent-local path
conventions. A template model can make approved work easier to run for a small
team, but it touches command safety, `pmctl` UX, API ownership, file/result
boundaries, and future workflow engine scope.

The current runtime baseline remains intentionally narrow:

- HTTP/JSON v0 with `X-Planetary-Protocol-Version: 1`.
- Command jobs use `type="command"`, a logical `command` allowlist key, and an
  argument vector.
- Agents map logical command keys to explicit local executable paths or
  reserved `builtin:<name>` validation targets through
  `AGENT_COMMAND_ALLOWLIST`.
- Agents execute external targets with `exec.CommandContext` and never invoke a
  shell.
- Built-ins are validation helpers, not a workflow extension mechanism.
- Real private workflows use explicit allowlisted external commands or wrapper
  executables.
- Stdout and stderr are bounded result fields, not artifact storage.
- There is no file transfer, strong sandboxing, workflow engine, cancellation
  API, per-job timeout override, scheduler policy, or secret-management layer.

Milestone 24 records the template decision only. It does not implement runtime
behavior.

## Decision

The first private workflow template model is **client-side `pmctl` expansion to
existing command jobs**.

Templates are operator-facing files that describe a named, approved way to build
one existing command job. `pmctl` will parse a template, validate operator
inputs, expand the template into a logical command key plus argument vector, and
submit the existing `POST /jobs` command-job request. The coordinator remains
the authority for job validation, scheduling, lifecycle transitions, result
recording, storage, metrics, and API behavior.

The initial implementation milestone should not add coordinator-owned template
resources, agent-side templates, new job types, new endpoints, new protocol
fields, or database tables.

### Template format

Templates use JSON so the future implementation can use Go's standard library
without adding dependencies.

The first schema version is `version: 1`. A v1 template has this shape:

```json
{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {
      "name": "input_path",
      "description": "Agent-local input path.",
      "required": true
    }
  ],
  "args": [
    {"param": "input_path"}
  ]
}
```

Required v1 fields:

- `version`: integer, currently exactly `1`.
- `name`: stable operator-facing template name.
- `command`: logical allowlist key to submit as the command job's `command`.
- `args`: ordered argument token list.

Optional v1 fields:

- `description`: human-facing summary.
- `parameters`: list of string parameters accepted by the template.

Parameter fields:

- `name`: unique parameter name.
- `description`: optional human-facing explanation.
- `required`: true when the operator must supply a value.
- `default`: optional string default. If `required` is false, `default` must be
  present.

Argument tokens are intentionally structured. Each token must contain exactly
one of:

- `{"literal": "fixed-string"}`
- `{"param": "parameter_name"}`

There is no string interpolation syntax. Parameter values expand to exactly one
argument vector element. Literal values also expand to exactly one argument
vector element.

### Validation and expansion policy

The future `pmctl` implementation should validate before submitting any job:

- reject unknown template versions
- reject missing or duplicate template names, parameter names, and parameter
  references
- reject unsupported fields in argument tokens
- reject missing required parameters
- reject unknown operator-supplied parameters
- reject optional parameters without defaults
- reject template commands that are empty, contain path separators, contain
  whitespace/control characters, are `.` or `..`, or start with `builtin:`

The template `command` field is a logical allowlist key only. Templates must not
submit executable paths, shell snippets, or reserved built-in target strings.
The agent still performs final allowlist enforcement because `pmctl` cannot know
which node will execute a job or which allowlist that node has configured.

Template expansion should produce the same request shape as:

```json
{
  "type": "command",
  "command": "text-stats",
  "args": ["/agent/local/input.txt"]
}
```

Client-side template validation errors should be reported as `pmctl` operator
errors and should not create jobs. Coordinator or agent failures after
successful submission should continue to surface through the existing job state,
stdout, stderr, truncation flags, and `last_error` fields.

### Template location

V1 templates are loaded from explicit operator-provided file paths. The first
implementation should not add implicit config directory discovery.

Example templates may be added under a tracked `examples/templates/` directory
in the implementation milestone. Release layouts may later copy selected example
templates under a `templates/` directory, but that is a packaging decision for
the implementation milestone and should be covered by releasebuild tests if
added.

### Result and file boundary

Templates do not change result handling:

- stdout and stderr remain the existing bounded job result fields
- `stdout_truncated` and `stderr_truncated` keep their existing meanings
- `last_error`, `exit_code`, attempts, timestamps, and node id keep their
  current meanings
- large artifacts remain out of scope

Templates also do not transfer files. If a template parameter represents an
input or output path, that path is interpreted by the wrapper executable on the
selected agent host. Operators must still prepare agent-local files or mounts
outside Planetary Mesh.

### Boundaries

Validation built-ins, wrapper executables, templates, and future workflow
engines remain separate concepts:

- Built-ins are narrow portable validation helpers and remain available only
  when explicitly mapped through `AGENT_COMMAND_ALLOWLIST`.
- Wrapper executables are the current runtime unit for real private workflows.
  They should validate domain-specific arguments and interact with host-local
  tools or files as needed.
- Templates are an operator UX layer that turns named parameters into one
  logical command job.
- A future workflow engine, file/result layer, artifact store, or scheduler
  policy would require separate planning and likely separate ADRs.

## Milestone 25 implementation direction

The concrete follow-up should be **Milestone 25: `pmctl` template submission and
example templates**.

Expected implementation scope:

- Add an internal template parser/validator/expander package, likely
  `internal/workflowtemplate`.
- Add a `pmctl templates validate <template-file>` command.
- Add a `pmctl submit template <template-file> --set name=value` command that
  expands to the existing command-job submission path.
- Add `examples/templates/text-stats.pmtemplate.json` using the existing
  `text-stats` logical command key and one required `input_path` parameter.
- Keep `pmctl` a thin client after expansion; coordinator-owned behavior remains
  in the coordinator.
- Do not add endpoints, protocol fields, job statuses, storage tables,
  scheduler behavior, validation built-ins, file transfer, or artifact storage.

Expected tests:

- DB-free unit tests for template parse, schema version validation, parameter
  validation, defaults, duplicate/unknown parameter handling, argument
  expansion, command-key safety checks, and JSON examples.
- `pmctl` command tests proving template validation errors do not call the
  client and successful expansion calls `CreateCommandJob` with the expected
  command and args.
- If example templates are copied into release layouts, releasebuild layout
  tests must cover the copied files.
- Default `go test ./...` remains DB-free. Postgres tests are unnecessary unless
  coordinator/Postgres behavior changes, which Milestone 25 should avoid.

Expected validation:

```bash
git diff --check
gofmt -l .
GOCACHE=/private/tmp/planetary-mesh-gocache-build go build ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-test go test ./...
GOCACHE=/private/tmp/planetary-mesh-gocache-vet go vet ./...
```

## Alternatives Considered

### Continue wrapper convention only

Pros:

- No new template syntax or CLI behavior.
- Keeps the current runtime and docs simple.

Cons:

- Operators must continue remembering command keys, argument order, and
  agent-local path conventions.
- It does not provide a repeatable team-facing workflow surface.

### Agent-host templates

Pros:

- Keeps workflow definitions close to executable availability.
- Could eventually reflect node-specific tool paths.

Cons:

- Fragments the operator experience across agents.
- Makes template availability depend on node selection before the scheduler can
  reason about templates.
- Risks turning agents into workflow-definition authorities.

### Coordinator-owned template resources

Pros:

- Gives one central registry for templates.
- Could support future operator listing, audit, and access-control workflows.

Cons:

- Requires new API and storage ownership before the template shape is proven.
- Raises compatibility and migration questions too early.
- Encourages runtime behavior changes in a milestone whose goal is to settle the
  safe model first.

### Env-style templates

Pros:

- Matches existing local config ergonomics.
- Easy to edit.

Cons:

- Too flat for a structured parameter list and ordered argument tokens.
- Would require custom parsing rules for nested data.

### YAML templates

Pros:

- Human-friendly for structured configuration.

Cons:

- Adds a parser dependency or a custom parser.
- Introduces more syntax surface than this v1 model needs.

### Shell or interpolation templates

Pros:

- Very flexible for experienced operators.

Cons:

- Conflicts with the no-shell execution model.
- Makes quoting, expansion, injection, and audit behavior harder to reason
  about.
- Blurs the boundary between templates and arbitrary scripts.

### Client-side JSON expansion to command jobs (chosen)

Pros:

- Gives operators a repeatable workflow surface without changing runtime
  protocol or storage.
- Preserves coordinator ownership of job state and scheduling.
- Preserves agent ownership of final allowlist enforcement.
- Uses the existing command-job API and result model.
- Can be implemented with the Go standard library and DB-free tests.

Cons:

- The coordinator stores the expanded command and args, not the template name or
  parameter map.
- Template availability is local to the operator environment unless teams share
  files out of band.
- `pmctl` gains pre-submission validation logic, so docs must keep clear that
  runtime authority remains with the coordinator and agent allowlists.

## Consequences

Positive:

- Milestone 25 can implement templates without reopening the basic model.
- The template design improves repeatability while preserving the current
  command execution trust boundary.
- The model keeps built-ins, wrappers, templates, and future workflow engines
  distinct.
- No public API, coordinator-agent protocol, storage schema, or job lifecycle
  behavior changes are needed for v1.

Negative:

- Templates do not solve node selection, file placement, result artifacts,
  secret management, cancellation, or long-running workflows.
- Operators must still ensure wrapper executables and input paths exist on
  eligible agents.
- A future coordinator-owned registry may still be useful after the file-based
  model proves itself.

Open questions:

- Whether template names should eventually be recorded as additive job metadata.
- Whether release artifacts should include example templates in Milestone 25.
- Whether future scheduler policy should use template requirements or node
  capabilities after templates exist.
- Whether future file/result handling should be template-aware or remain a
  separate workflow layer.

## Non-goals

This decision does not add:

- runtime feature changes
- public API or protocol changes
- coordinator-owned template resources
- storage or Postgres schema changes
- new job states
- validation built-ins
- file upload or result download
- artifact storage
- DAGs or a workflow engine
- scheduler policy
- cancellation
- per-job timeout overrides
- secret management
- strong sandbox, container, VM, or multi-tenant isolation
- dashboard or GUI work
- remote private mesh, shared pool, marketplace, payment, or public-node work
- production packaging, signed installers, package-manager distribution,
  production Docker images, GitHub Releases, or version tags
- new dependencies
