# ADR 0001: Use iterative SDLC with lightweight documentation and ADRs

- Status: Accepted
- Date: 2025-11-25

## Context

Planetary Mesh is a new system with:
- Uncertain requirements around scheduling, security, and workloads.
- A small team (or solo dev) working in short bursts.
- A need to reason carefully about distributed systems and security.

We need some structure (requirements, architecture, decisions), but a heavy process 
(e.g., strict Waterfall or RUP) would slow down iteration without clear benefit.

## Decision

We will:

- Use an **iterative, incremental SDLC**:
  - Short iterations (1–2 weeks).
  - Working code at the end of each iteration.
  - Regular refinement of requirements and design.

- Maintain a small set of core docs:
  - `docs/kickoff.md` – goals, SDLC, scope.
  - `docs/architecture.md` – components, data model, flows.
  - `docs/tech-choices.md` – options and rationale.
  - `docs/adr/*.md` – specific architecture decisions.

## Alternatives Considered

- **Strict Waterfall**
  - Pros: high upfront clarity; familiar in some orgs.
  - Cons: assumes stable requirements; hard to adjust once coding reveals new constraints.

- **Heavy RUP / Spiral**
  - Pros: thorough risk management; strong structure.
  - Cons: too heavyweight for a small, exploratory project; lots of ceremony.

- **Ad-hoc Coding (no process)**
  - Pros: fast to start coding.
  - Cons: architecture and security decisions become implicit; harder to reason about the system later.

## Consequences

- Positive:
  - We get enough documentation to understand and evolve the system.
  - We can change course when real-world tests show better options.
  - Each major decision has a written rationale.

- Negative:
  - Some discipline is still required to keep docs and ADRs updated.
  - Not as rigid as some organizations might want for later, production-grade phases.

- Open questions:
  - How often to review and potentially retire old ADRs.
  - How to adapt the process if the team or scope grows significantly.
