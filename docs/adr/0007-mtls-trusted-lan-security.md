# ADR 0007: Use mTLS and node allowlists for trusted LAN security

- Status: Accepted
- Date: 2026-05-01

## Context

Milestone 4 secures the coordinator-agent control plane without changing the v0
HTTP/JSON protocol recorded in ADR 0003. The mesh still targets trusted LAN use,
but command execution and coordinator-controlled dispatch need authenticated
peers and explicit node admission before the operator CLI milestone.

The project also needs to keep plain local development simple and keep default
tests free of external services.

## Decision

For v0 trusted LAN security, coordinator-agent traffic uses opt-in mutual TLS
with manually distributed certificates.

Details:

- Coordinator and agents load CA, certificate, and key files from environment
  variables.
- If any TLS file is configured, all three files are required.
- Coordinator secure mode requires a node allowlist by certificate identity or
  SHA-256 fingerprint.
- Agents validate the coordinator certificate against the configured CA.
- Coordinator validates agent client certificates against the configured CA and
  rejects `/register` unless the request node id matches an allowlisted
  certificate identity or fingerprint.
- Coordinator dispatch to agent `/execute` uses HTTPS and presents the
  coordinator certificate as a client certificate.
- Node inspection includes certificate subject, DNS/IP/URI identities,
  fingerprint, and expiration metadata.
- Certificate issuance, enrollment, and rotation automation are out of scope for
  v0.

## Alternatives Considered

- **Plain HTTP for another milestone**
  - Pros: no certificate setup yet.
  - Cons: leaves command-capable dispatch unauthenticated between coordinator
    and agents.

- **Token-based node authentication**
  - Pros: simpler to configure than certificates.
  - Cons: does not provide mutual transport authentication or a strong
    certificate identity for operators.

- **Automated certificate enrollment**
  - Pros: better operator experience.
  - Cons: adds an issuance protocol and trust bootstrap problem before the v0
    control plane is otherwise complete.

- **mTLS with manual distribution and allowlists (chosen)**
  - Pros: standard-library support, strong peer authentication, and explicit
    node admission with limited new machinery.
  - Cons: operators must provision certificates and keep allowlists current.

## Consequences

- Positive:
  - Coordinator-agent registration, heartbeat, and dispatch can run with mutual
    authentication and encrypted transport.
  - Unauthorized nodes are rejected before they enter coordinator storage.
  - Operators can inspect node certificate metadata through the existing node
    API.
- Negative:
  - Local secure runs require manual CA/certificate/key setup.
  - There is no certificate rotation or enrollment automation in v0.
- Open questions:
  - Whether future operator tooling should manage certificate generation,
    fingerprint discovery, and allowlist updates.
