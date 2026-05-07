# ADR 0009: Use env-style files for local runtime configuration

- Status: Accepted
- Date: 2026-05-05

## Context

After Milestone 5, the coordinator, agents, and `pmctl` can be configured with
environment variables and selected CLI flags. That keeps runtime behavior simple,
but it makes repeated local runs awkward, especially when running multiple
agents with distinct node ids and addresses.

Milestone 6 needs an editable local configuration story without changing the v0
control plane, mTLS model, storage behavior, or command execution rules.

## Decision

Use env-style `KEY=value` config files for local runtime configuration.

Details:

- Config files reuse the existing environment variable names.
- Config files are optional; env-only runs continue to work.
- Each binary accepts `--config <path>`.
- Each binary also supports a config path environment variable:
  - `COORDINATOR_CONFIG_FILE`
  - `AGENT_CONFIG_FILE`
  - `PMCTL_CONFIG_FILE`
- If present, each binary auto-loads its local default file:
  - `config/coordinator.env`
  - `config/agent.env`
  - `config/pmctl.env`
- Precedence is:
  1. compiled defaults
  2. config file values
  3. non-empty environment variables
  4. CLI flags, where supported
- Example files are tracked as `*.env.example`; local `*.env` files are ignored.

## Alternatives Considered

- **JSON config files**
  - Pros: standard-library parser and structured data.
  - Cons: less pleasant for operators to edit and would require mapping new
    field names back to existing env vars.

- **TOML/YAML config files**
  - Pros: friendly for structured configuration.
  - Cons: would add a parser dependency or custom parser for a small v0 need.

- **Env-style files (chosen)**
  - Pros: standard-library implementation, matches existing config names, and
    preserves current env-based operations.
  - Cons: flatter than structured config and not intended to replace a richer
    production configuration system later.

## Consequences

- Positive:
  - Local coordinator, agent, and `pmctl` runs can be repeated without long
    terminal commands.
  - Multi-agent local development is easier to document and reproduce.
  - Existing deployment scripts that use environment variables keep working.
- Negative:
  - Config remains intentionally flat.
  - Secrets can be placed in local files, so local `*.env` files are ignored and
    should not be committed.
