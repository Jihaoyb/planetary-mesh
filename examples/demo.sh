#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COORD_CONFIG="${COORD_CONFIG:-${ROOT}/config/coordinator.env.example}"
AGENT1_CONFIG="${AGENT1_CONFIG:-${ROOT}/config/agent-1.env.example}"
AGENT2_CONFIG="${AGENT2_CONFIG:-${ROOT}/config/agent-2.env.example}"
PMCTL_CONFIG="${PMCTL_CONFIG:-${ROOT}/config/pmctl.env.example}"

COORD_URL="${COORD_URL:-http://localhost:8080}"
AGENT1_URL="${AGENT1_URL:-http://localhost:8081}"
AGENT2_URL="${AGENT2_URL:-http://localhost:8082}"

LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-smoke-$(date +%Y%m%d%H%M%S)}"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-smoke-bin.XXXXXX")"

COORD_LOG="${LOG_DIR}/coordinator.log"
AGENT1_LOG="${LOG_DIR}/agent-1.log"
AGENT2_LOG="${LOG_DIR}/agent-2.log"

UNSET_ENV=(
  -u COORDINATOR_CONFIG_FILE
  -u COORDINATOR_ADDR
  -u COORDINATOR_DATABASE_URL
  -u COORDINATOR_TLS_CA_FILE
  -u COORDINATOR_TLS_CERT_FILE
  -u COORDINATOR_TLS_KEY_FILE
  -u COORDINATOR_ALLOWED_NODE_IDENTITIES
  -u COORDINATOR_ALLOWED_NODE_FINGERPRINTS
  -u AGENT_CONFIG_FILE
  -u AGENT_ADDR
  -u AGENT_ADVERTISE_ADDR
  -u COORDINATOR_URL
  -u NODE_ID
  -u AGENT_EXEC_TIMEOUT
  -u AGENT_COMMAND_ALLOWLIST
  -u AGENT_TLS_CA_FILE
  -u AGENT_TLS_CERT_FILE
  -u AGENT_TLS_KEY_FILE
  -u PMCTL_CONFIG_FILE
  -u PMCTL_COORDINATOR_URL
  -u PMCTL_TLS_CA_FILE
  -u PMCTL_TLS_CERT_FILE
  -u PMCTL_TLS_KEY_FILE
)

cleanup() {
  set +e
  for pid in "${AGENT2_PID:-}" "${AGENT1_PID:-}" "${COORD_PID:-}"; do
    if [[ -n "${pid}" ]]; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
  for pid in "${AGENT2_PID:-}" "${AGENT1_PID:-}" "${COORD_PID:-}"; do
    if [[ -n "${pid}" ]]; then
      wait "${pid}" >/dev/null 2>&1 || true
    fi
  done
  rm -rf "${BIN_DIR}"
}
trap cleanup EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

run_clean() {
  env "${UNSET_ENV[@]}" "$@"
}

pmctl() {
  run_clean "${BIN_DIR}/pmctl" --config "${PMCTL_CONFIG}" "$@"
}

wait_for_url() {
  local name="$1"
  local url="$2"

  for _ in {1..80}; do
    if curl -sf "${url}" >/dev/null; then
      return 0
    fi
    sleep 0.25
  done

  echo "Timed out waiting for ${name} at ${url}" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

wait_for_nodes() {
  local nodes_json

  for _ in {1..80}; do
    nodes_json="$(pmctl --json nodes list 2>/dev/null || true)"
    if [[ -n "${nodes_json}" ]] && python3 -c '
import json
import sys

nodes = json.load(sys.stdin)
ids = {node.get("id"): node.get("state") for node in nodes}
ok = ids.get("local-agent-1") == "HEALTHY" and ids.get("local-agent-2") == "HEALTHY"
sys.exit(0 if ok else 1)
' <<<"${nodes_json}"; then
      return 0
    fi
    sleep 0.25
  done

  echo "Timed out waiting for local-agent-1 and local-agent-2 to register as HEALTHY" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

wait_for_job() {
  local job_id="$1"
  local detail status

  for _ in {1..120}; do
    detail="$(pmctl --json jobs inspect "${job_id}")"
    status="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])' <<<"${detail}")"
    if [[ "${status}" == "COMPLETED" || "${status}" == "FAILED" ]]; then
      printf '%s\n' "${detail}"
      [[ "${status}" == "COMPLETED" ]]
      return
    fi
    sleep 0.25
  done

  echo "Timed out waiting for job ${job_id}" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

require_command go
require_command curl
require_command python3

mkdir -p "${LOG_DIR}"

echo "Building local smoke binaries"
(cd "${ROOT}" && go build -o "${BIN_DIR}/coordinator" ./cmd/coordinator)
(cd "${ROOT}" && go build -o "${BIN_DIR}/agent" ./cmd/agent)
(cd "${ROOT}" && go build -o "${BIN_DIR}/pmctl" ./cmd/pmctl)

echo "Starting coordinator with ${COORD_CONFIG}"
run_clean "${BIN_DIR}/coordinator" --config "${COORD_CONFIG}" >"${COORD_LOG}" 2>&1 &
COORD_PID=$!
wait_for_url "coordinator" "${COORD_URL}/healthz"

echo "Starting agent 1 with ${AGENT1_CONFIG}"
run_clean "${BIN_DIR}/agent" --config "${AGENT1_CONFIG}" >"${AGENT1_LOG}" 2>&1 &
AGENT1_PID=$!
wait_for_url "agent 1" "${AGENT1_URL}/healthz"

echo "Starting agent 2 with ${AGENT2_CONFIG}"
run_clean "${BIN_DIR}/agent" --config "${AGENT2_CONFIG}" >"${AGENT2_LOG}" 2>&1 &
AGENT2_PID=$!
wait_for_url "agent 2" "${AGENT2_URL}/healthz"

echo "Waiting for both agents to register"
wait_for_nodes

echo
echo "Coordinator status"
pmctl status

echo
echo "Registered nodes"
pmctl nodes list

echo
echo "Submitting command job"
JOB_JSON="$(pmctl --json submit command echo "hello from planetary mesh")"
JOB_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${JOB_JSON}")"
echo "Created ${JOB_ID}"

FINAL_JOB_JSON="$(wait_for_job "${JOB_ID}")"
FINAL_STDOUT="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["stdout"].strip())' <<<"${FINAL_JOB_JSON}")"
if [[ "${FINAL_STDOUT}" != "hello from planetary mesh" ]]; then
  echo "Unexpected job stdout: ${FINAL_STDOUT}" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  exit 1
fi

echo
echo "Job detail"
pmctl jobs inspect "${JOB_ID}"

echo
echo "Jobs"
pmctl jobs list

echo
echo "Smoke demo completed successfully"
echo "Logs are in ${LOG_DIR}"
