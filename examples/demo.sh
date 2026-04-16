#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HEADER="X-Planetary-Protocol-Version: 1"
COORD_URL="${COORD_URL:-http://localhost:8080}"
AGENT_ADDR="${AGENT_ADDR:-:8081}"
COORD_ADDR="${COORD_ADDR:-:8080}"
ALLOWLIST="${AGENT_COMMAND_ALLOWLIST:-echo=echo,false=false,sleep=sleep}"

cleanup() {
  if [[ -n "${AGENT_PID:-}" ]]; then
    kill "${AGENT_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${COORD_PID:-}" ]]; then
    kill "${COORD_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "Starting coordinator on ${COORD_ADDR}"
(cd "${ROOT}" && COORDINATOR_ADDR="${COORD_ADDR}" go run ./cmd/coordinator) >/tmp/planetary-mesh-coordinator.log 2>&1 &
COORD_PID=$!

for _ in {1..40}; do
  if curl -sf "${COORD_URL}/healthz" >/dev/null; then
    break
  fi
  sleep 0.25
done

echo "Starting agent on ${AGENT_ADDR}"
(cd "${ROOT}" && AGENT_ADDR="${AGENT_ADDR}" COORDINATOR_URL="${COORD_URL}" AGENT_COMMAND_ALLOWLIST="${ALLOWLIST}" go run ./cmd/agent) >/tmp/planetary-mesh-agent.log 2>&1 &
AGENT_PID=$!

for _ in {1..40}; do
  if curl -sf "http://localhost${AGENT_ADDR}/healthz" >/dev/null; then
    break
  fi
  sleep 0.25
done

for _ in {1..40}; do
  NODES_JSON="$(curl -sf "${COORD_URL}/nodes" -H "${HEADER}" || true)"
  if [[ -n "${NODES_JSON}" ]] && python3 -c 'import json,sys; sys.exit(0 if len(json.loads(sys.stdin.read())) > 0 else 1)' <<<"${NODES_JSON}"; then
    break
  fi
  sleep 0.25
done

JOB_JSON="$(curl -sf -X POST "${COORD_URL}/jobs" \
  -H "${HEADER}" \
  -H 'Content-Type: application/json' \
  -d '{"type":"command","command":"echo","args":["hello from planetary mesh"]}')"

JOB_ID="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])' <<<"${JOB_JSON}")"
echo "Created job ${JOB_ID}"

for _ in {1..60}; do
  DETAIL="$(curl -sf "${COORD_URL}/jobs/${JOB_ID}" -H "${HEADER}")"
  STATUS="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["status"])' <<<"${DETAIL}")"
  if [[ "${STATUS}" == "COMPLETED" || "${STATUS}" == "FAILED" ]]; then
    echo "${DETAIL}" | python3 -m json.tool
    exit 0
  fi
  sleep 0.25
done

echo "Timed out waiting for job ${JOB_ID}" >&2
exit 1
