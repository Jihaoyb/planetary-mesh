# Current Limitations

This register separates current private-mesh limitations from future shared-pool
and marketplace risks. It is intentionally conservative: if a capability is not
implemented, docs should not imply that it exists.

| Area | Current limitation/risk | Why it matters | Mitigation / future direction | Phase |
|---|---|---|---|---|
| Command execution | Agents run allowlisted direct processes on trusted hosts. | A compromised or misconfigured allowlisted command can still affect the host. | Keep no-shell execution and explicit allowlists; add stronger isolation before shared/untrusted workloads. | Current / Phase 1 |
| Isolation | No strong sandbox, container, VM, or multi-tenant isolation. | The system is not safe for arbitrary third-party workloads. | Evaluate container or VM/microVM execution after private mesh basics are stable. | Phase 1-2 |
| mTLS lifecycle | mTLS and node allowlists are opt-in and certificate lifecycle is manual. | Manual cert setup is error-prone and limits remote/private adoption. | Add certificate/onboarding helper planning, then lifecycle tooling. | Phase 1-3 |
| Scheduling | Dispatch starts with the first healthy node and can reassign after retryable failures, but it is still not load-aware or capability-aware. | It ignores load, capabilities, queue depth, and historical reliability. | Add node capabilities/load reporting and scheduler policy after reassignment behavior is reliable. | Phase 1 |
| Queued jobs | Queued jobs are revisited periodically and expire after a fixed 24-hour window. | Operators may see a short delay before a queued job runs after a node becomes healthy, and long outages fail queued jobs instead of retrying forever. | Keep the scheduler simple for v0; consider configurable queue TTLs, event-driven wakeups, or richer queue policy after reliability basics are stable. | Phase 1 |
| Restart recovery | Persisted `RUNNING` jobs are failed on coordinator startup; agents do not reconcile. | A completed agent result can be lost if the coordinator crashed before persisting it. | Design agent reconciliation or idempotent result reporting. | Phase 1 |
| Operator UX | `pmctl` exists, but there is no dashboard or rich logs UX. | Operational troubleshooting is still command-line heavy. | Improve CLI, runbooks, logs UX, or add a scoped dashboard later. | Phase 2 |
| API contract | No OpenAPI/protobuf contract is generated. | External clients can drift from the HTTP/JSON wire shape. | Add API inventory, then decide whether OpenAPI/protobuf is warranted. | Phase 1-2 |
| Packaging | No production Dockerfile, release artifact, or install workflow. | New users must run from source or Compose examples. | Add packaging/release workflow after private mesh hardening. | Phase 2 |
| Docs drift | Historical docs can overstate future-state capabilities. | Misleading docs create unsafe expectations around security and maturity. | Keep README, roadmap, architecture, product positioning, and limitations as current sources of truth. | Current |
| Remote networking | No remote private mesh support today. | Running trusted nodes outside the LAN needs stronger identity and network handling. | Plan secure remote registration, access control, and failure handling. | Phase 3 |
| Shared pool trust | No trust levels, approval workflow, or workload templates. | Shared compute without controls creates security and abuse risk. | Add admin approval, trust levels, quotas, and approved workloads first. | Phase 4 |
| Marketplace cold start | No provider/buyer network exists. | Marketplaces need both sides and reliable supply/demand. | Treat overflow compute as long-term exploration after private value is proven. | Phase 5 |
| Provider/buyer trust | No reputation, verification, or acceptable-use controls. | External compute requires trust, compliance, and abuse prevention. | Add provider verification, reputation, strict workload policies, and auditing. | Phase 5 |
| Hardware variance | External hardware quality and availability can vary widely. | Unreliable providers can produce poor job outcomes. | Benchmark providers, track uptime, and constrain approved workloads. | Phase 5 |
| Payment/disputes | No pricing, metering, payouts, refunds, or dispute system. | Marketplace billing creates financial and support obligations. | Defer until overflow compute is explicitly scoped and trust controls exist. | Phase 5 |
| Abuse prevention | No public abuse-prevention model. | Public arbitrary compute can be misused. | Require stronger isolation, identity, acceptable-use policy, and review before public/shared features. | Phase 4-5 |
