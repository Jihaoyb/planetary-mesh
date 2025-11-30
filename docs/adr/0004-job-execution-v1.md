# 0004 – Job Execution v1

- Status: Accepted
- Date: 2025-11-30

## Context

Planetary Mesh now has:

- A coordinator tracking nodes and their health.
- An in-memory JobStore with a `/jobs` API to create and list jobs.
- Agents that register with the coordinator and expose `/healthz`.

Previously, jobs were only created and stored; they were never actually executed by agents. To make the system useful and demonstrate the core concept of "submit work → distributed execution", we need a minimal execution path from coordinator to agents.

For v1, we want:

- A simple, understandable job lifecycle.
- A very naive scheduler (good enough for LAN demos).
- A lightweight execution API on the agent.
- Tests around these behaviors.

We explicitly do **not** attempt advanced scheduling, retries, or security in this version.

## Decision

1. **Job lifecycle and model**

   - Jobs are represented by:

     - `ID`, `Type`, `Payload`
     - `Status` ∈ {`QUEUED`, `RUNNING`, `COMPLETED`, `FAILED`}
     - `NodeID` – the node that is (or was) responsible for the job
     - `CreatedAt`, `UpdatedAt` timestamps

   - The coordinator owns an in-memory `JobStore` that:
     - Creates new jobs with status `QUEUED`.
     - Lists all jobs.
     - Updates job status and (optionally) `NodeID` via `UpdateStatus`.

2. **Coordinator dispatch and scheduling**

   - The coordinator exposes `POST /jobs` to create jobs.
   - On job creation, the coordinator:
     - Stores the job as `QUEUED`.
     - Returns the job immediately to the client.
     - Asynchronously calls `dispatchJob(jobID)` in a goroutine.
   - `dispatchJob`:
     - Reads the node registry and selects the **first** node in state `HEALTHY`.
     - Updates the job using `UpdateStatus(jobID, RUNNING, nodeID)`.
     - Builds the agent URL using the node's `Address` and calls `POST /execute`.
     - If the agent responds with HTTP 200:
       - Updates status to `COMPLETED`.
     - On any error or non-200 response:
       - Updates status to `FAILED`.

   - The scheduler is intentionally naive for v1:
     - Single coordinator, in-memory state.
     - No load balancing, weights, or retries.
     - No queueing beyond the single attempt.

3. **Agent execution API**

   - Each agent exposes `POST /execute`.
   - Request body:

     - `job_id` – ID of the job created on the coordinator.
     - `type` – job type (e.g. `"echo"`).
     - `payload` – opaque string payload.

   - V1 "execution" behavior on the agent:
     - Validate JSON and `job_id`.
     - Log that the job started.
     - Simulate work (e.g. short sleep).
     - Log that the job finished.
     - Return `200 OK` with JSON `{ "status": "ok" }`.

4. **HTTP addressing**

   - Node `Address` values are interpreted as:
     - `:8081`           → `http://localhost:8081`
     - `host:8081`       → `http://host:8081`
     - `http://host:8081` (or https) → used as-is.
   - This logic is implemented in a helper `buildAgentBaseURL`.

## Consequences

- The system now supports a complete demo flow:
  - Client calls `POST /jobs`.
  - Coordinator creates a job, picks a healthy node, and calls the agent's `/execute`.
  - Agent performs dummy work and responds.
  - Coordinator marks the job `COMPLETED` or `FAILED`.
  - Client can query `GET /jobs` to see job status and which node executed it.

- The implementation is intentionally simple:
  - Suitable for a single LAN coordinator and a small number of agents.
  - Easy to reason about and test.

- Known limitations (to be addressed in future ADRs/stages):
  - No retries or backoff on agent failure.
  - No job timeout handling.
  - No per-node capacity limits or load-aware scheduling.
  - No security: plain HTTP, no authentication or mTLS.
  - No `GET /jobs/{id}` or filtered queries.

These limitations are acceptable for v1 and provide a clear foundation for future scheduling, resilience, and security improvements.
