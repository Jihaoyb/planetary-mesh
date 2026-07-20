# Product Positioning

## One-liner

Planetary Mesh is a lightweight private compute mesh for running jobs across
machines you own or control, with a future path to trusted overflow compute.

## Current Product Stage

Planetary Mesh is currently a trusted LAN/private-network command-job runner
prototype. It has a Go coordinator, agent daemon, and thin `pmctl` CLI. The
current implementation supports node registration, heartbeat, health states,
allowlisted direct command execution with explicit portable validation built-ins,
bounded output capture, a tracked external `text-stats` workload example,
optional Postgres persistence, opt-in mTLS, node allowlists, metrics, local
smoke workflows, pre-release local binary artifact/install-smoke tooling, and
`pmctl` client-side workflow template validation, inspection, preview, and
submission. Linux release archives also provide a pre-release systemd path for
installing coordinator and agent independently under stable unprivileged
service identities.

It is not a public marketplace, production multi-tenant compute platform,
signed distribution, package-manager distributed product, automatic-update
system, or production-certified installer.

## Target Early Users

- developers with multiple local machines
- small AI agencies
- research groups and small labs
- internal automation teams
- businesses with private batch processing needs
- teams that want simpler job execution without Kubernetes-style platforms

## Primary Early Use Cases

These are product directions, not all implemented features today:

- command-based batch jobs
- private AI preprocessing
- OCR and document processing
- audio transcription
- embedding generation
- image and file conversion
- developer automation jobs

The current implemented workload is allowlisted command execution. `pmctl`
templates can validate and inspect approved JSON files, preview operator
parameters as one command-job vector, and submit one existing command job. File
handling, richer job types, coordinator-owned template registries, and workflow
engines are not implemented today.

Portable built-ins are smoke/validation helpers, not the intended product
surface for adding every user workflow. Near-term real workflows should remain
explicit allowlisted external commands or wrapper scripts, following the
tracked `text-stats` example pattern. Current templates can expose approved
actions such as OCR, transcription, embeddings, image conversion, or batch
processing after those wrappers are explicitly built, allowlisted, and
validated.

## Product Path

### 1. Private / Local Compute Mesh

Run jobs across machines owned or controlled by the same user/team. This is the
current wedge and closest to the implementation today.

Primary value:

- privacy
- local-first control
- simple operations
- lower complexity than heavier orchestration platforms for small private job
  workloads

### 2. Remote Private Mesh

Support trusted machines outside the LAN while still owned or controlled by the
same user/team.

Required direction:

- secure remote registration
- authenticated communication
- encrypted transport
- stronger node identity
- access control
- remote health and failure handling
- better operator tooling

This remains private/team-owned infrastructure, not a marketplace.

### 3. Trusted Shared Compute Pool

Allow approved users, devices, contractors, labs, teams, or partner machines to
contribute compute inside a controlled group.

Required direction:

- admin approval
- trust levels
- usage accounting
- quotas or credits
- approved workload templates
- stronger audit and isolation model

This is a controlled bridge between private mesh and possible overflow compute.

### 4. Overflow Compute Marketplace

Explore verified external capacity only after the private mesh and trusted shared
pool are mature.

Long-term direction:

- users rent verified external capacity when local/private resources are not
  enough
- providers may earn money for approved idle compute
- Planetary Mesh may take a transaction fee

This should be framed as trusted overflow compute, not an early open arbitrary
public compute marketplace.

## What It Is Not

Planetary Mesh is not currently:

- a public marketplace
- arbitrary untrusted compute
- a crypto or token network
- a Kubernetes replacement
- a Ray, Airflow, or Temporal replacement
- a general-purpose cloud platform
- a GPU/storage/bandwidth marketplace
- production-ready secure multi-tenant infrastructure
- a signed/package-manager distributed application
- an automatically upgraded or cross-platform managed-service product

## Monetization Hypothesis

These are hypotheses, not current revenue claims:

- paid setup/customization for private deployments
- private AI/batch processing workflow implementation
- team subscription for productized private mesh features
- support and maintenance for teams running private meshes
- later transaction fees for trusted overflow compute

Near-term credibility depends on making the private mesh reliable and easy to
operate before expanding into shared or marketplace models.
