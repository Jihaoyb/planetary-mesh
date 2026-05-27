# Product Requirements

This document captures the near-term private mesh product requirements. It is
not a marketplace requirements document.

## Problem Statement

Small teams often have useful compute capacity across machines they already own,
but running repeatable jobs across those machines usually requires too much
operational machinery. Planetary Mesh should provide a lightweight private mesh
for trusted command-based jobs before expanding into remote or shared compute.

## Target Users

- developers with multiple machines
- small AI agencies
- small labs or research groups
- internal automation teams
- businesses with private document, audio, image, or batch processing needs

## MVP Scope

Near-term MVP work should harden the private/local mesh:

- reliable queued job processing, starting with periodic coordinator-owned
  re-dispatch for jobs left queued without a healthy node
- clearer scheduler behavior
- cross-node reassignment after retryable dispatch failure
- explicit, documented, and test-covered job lifecycle transitions
- better node health, capability, and load visibility
- narrow runtime result reporting after coordinator restart, based on the
  accepted agent reconciliation strategy
- stronger execution-risk docs and runbooks
- portable no-shell validation commands for cross-OS private mesh checks
- improved install/onboarding workflow
- optional demo pipeline for private batch/AI-style work
- manual HTTP/JSON v0 API inventory and compatibility policy

The current implemented workload remains allowlisted command execution.
Portable built-ins are validation helpers for cross-OS smoke checks; they are
not the long-term mechanism for adding every product workflow.

## Non-goals

- public compute marketplace
- payment, payout, dispute, or transaction-fee systems
- public-node onboarding
- arbitrary untrusted workload execution
- production multi-tenant cloud platform behavior
- broad GPU/storage/bandwidth marketplace features
- replacing Kubernetes, Ray, Airflow, or Temporal

## Success Metrics

Practical MVP success means:

- a user can run coordinator + agent locally
- the project has sanitized evidence of coordinator and agent running on
  different physical LAN machines
- the project has sanitized evidence of `pmctl` inspecting remote node, job,
  and result state over the LAN
- the project has at least one practical trusted workload recipe beyond local
  smoke commands
- a user can submit an allowlisted command job
- a user can run portable smoke validation commands without relying on
  platform shell built-ins
- a user can inspect job result, stdout, stderr, and errors
- job state transitions are documented and consistent across in-memory and
  Postgres-backed coordinator storage
- restart recovery behavior, best-effort result reporting, and remaining
  limitations are documented without implying full execution recovery exists
- a user can see node state, configured capabilities, and active execution count
- a user can choose in-memory or Postgres coordinator storage
- a user can follow current operator runbooks for local startup, Postgres
  durability, mTLS trusted-LAN setup, command-execution safety,
  troubleshooting, and validation workflows
- contributors have a current manual HTTP/JSON v0 API inventory and
  compatibility policy for endpoint, JSON-field, and metrics review
- default build/test checks pass without external services
- Postgres durability checks are opt-in and documented
- docs accurately describe current behavior
- docs do not imply marketplace or stronger security features than exist
- docs do not imply built-ins replace approved allowlisted commands, wrapper
  scripts, or future workflow/job templates for real workloads

Real multi-device LAN validation is now captured in the repository with
sanitized evidence. Documentation alone is still not product readiness; use a
separate Phase 1 readiness review before recommending Phase 2 work.

## Future Expansion

After the private mesh is reliable and easy to operate, future product work can
evaluate:

- richer operator UX
- remote private mesh
- approved shared compute pools
- trusted overflow compute

Each expansion should come with explicit architecture, security, operational,
and product planning before implementation.
