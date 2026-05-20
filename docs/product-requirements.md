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
- better node health, capability, and load visibility
- agent reconciliation strategy after coordinator restart
- stronger execution-risk docs and runbooks
- improved install/onboarding workflow
- optional demo pipeline for private batch/AI-style work
- API inventory and contract decision

The current implemented workload remains allowlisted command execution.

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
- a user can submit an allowlisted command job
- a user can inspect job result, stdout, stderr, and errors
- a user can see node state, configured capabilities, and active execution count
- a user can choose in-memory or Postgres coordinator storage
- default build/test checks pass without external services
- Postgres durability checks are opt-in and documented
- docs accurately describe current behavior
- docs do not imply marketplace or stronger security features than exist

## Future Expansion

After the private mesh is reliable and easy to operate, future product work can
evaluate:

- richer operator UX
- remote private mesh
- approved shared compute pools
- trusted overflow compute

Each expansion should come with explicit architecture, security, operational,
and product planning before implementation.
