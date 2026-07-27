#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COORD_ADDR="${COORD_ADDR:-:18280}"
COORD_URL="${COORD_URL:-http://localhost:18280}"
AGENT_ADDR="${AGENT_ADDR:-:18281}"
AGENT_URL="${AGENT_URL:-http://localhost:18281}"
NODE_ID="${NODE_ID:-doctor-smoke-agent}"

LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-doctor-$(date +%Y%m%d%H%M%S)}"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-doctor-bin.XXXXXX")"
COORD_LOG="${LOG_DIR}/coordinator.log"
AGENT_LOG="${LOG_DIR}/agent.log"

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
  -u AGENT_CAPABILITIES
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
  for pid in "${AGENT_PID:-}" "${COORD_PID:-}"; do
    if [[ -n "${pid}" ]]; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
  for pid in "${AGENT_PID:-}" "${COORD_PID:-}"; do
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
  run_clean "${BIN_DIR}/pmctl" --coordinator-url "${COORD_URL}" "$@"
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

wait_for_node() {
  local nodes_json

  for _ in {1..80}; do
    nodes_json="$(pmctl --json nodes list 2>/dev/null || true)"
    if [[ -n "${nodes_json}" ]] && python3 -c '
import json
import sys

nodes = json.load(sys.stdin)
ok = any(node.get("id") == sys.argv[1] and node.get("state") == "HEALTHY" for node in nodes)
sys.exit(0 if ok else 1)
' "${NODE_ID}" <<<"${nodes_json}"; then
      return 0
    fi
    sleep 0.25
  done

  echo "Timed out waiting for ${NODE_ID} to register as HEALTHY" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

require_empty_jobs() {
  local jobs_json
  jobs_json="$(pmctl --json jobs list)"
  python3 -c '
import json
import sys

jobs = json.load(sys.stdin)
sys.exit(0 if jobs == [] else 1)
' <<<"${jobs_json}"
}

require_doctor_json() {
  local json="$1"
  local overall="$2"
  local ready="$3"
  local total="$4"
  local healthy="$5"

  python3 -c '
import json
import sys

report = json.load(sys.stdin)
expected_ready = sys.argv[2] == "true"
ok = (
    report.get("schema_version") == 1
    and report.get("overall_status") == sys.argv[1]
    and report.get("facts", {}).get("job_submission_ready") is expected_ready
    and report.get("facts", {}).get("nodes", {}).get("total") == int(sys.argv[3])
    and report.get("facts", {}).get("nodes", {}).get("healthy") == int(sys.argv[4])
    and report.get("scope", {}).get("endpoints_used") == ["/status", "/nodes"]
    and report.get("scope", {}).get("creates_jobs") is False
    and report.get("scope", {}).get("contacts_agents_directly") is False
)
sys.exit(0 if ok else 1)
' "${overall}" "${ready}" "${total}" "${healthy}" <<<"${json}"
}

require_command go
require_command curl
require_command python3

mkdir -p "${LOG_DIR}"

echo "Building doctor smoke binaries"
(cd "${ROOT}" && go build -o "${BIN_DIR}/coordinator" ./cmd/coordinator)
(cd "${ROOT}" && go build -o "${BIN_DIR}/agent" ./cmd/agent)
(cd "${ROOT}" && go build -o "${BIN_DIR}/pmctl" ./cmd/pmctl)

echo "Starting coordinator-only diagnostic case at ${COORD_URL}"
run_clean COORDINATOR_ADDR="${COORD_ADDR}" "${BIN_DIR}/coordinator" >"${COORD_LOG}" 2>&1 &
COORD_PID=$!
wait_for_url "coordinator" "${COORD_URL}/healthz"

require_empty_jobs
COORD_ONLY_JSON="$(pmctl --json doctor)"
require_doctor_json "${COORD_ONLY_JSON}" "WARN" "false" "0" "0"
require_empty_jobs

STRICT_STDOUT="${LOG_DIR}/strict-doctor.json"
STRICT_STDERR="${LOG_DIR}/strict-doctor.stderr"
strict_exit=0
pmctl --json doctor --strict >"${STRICT_STDOUT}" 2>"${STRICT_STDERR}" || strict_exit=$?
if [[ "${strict_exit}" -ne 5 ]]; then
  echo "Expected strict coordinator-only doctor to exit 5, got ${strict_exit}" >&2
  exit 1
fi
if [[ -s "${STRICT_STDERR}" ]]; then
  echo "Strict diagnostic warning unexpectedly wrote stderr" >&2
  exit 1
fi
require_doctor_json "$(cat "${STRICT_STDOUT}")" "WARN" "false" "0" "0"
require_empty_jobs

echo "Starting agent ${NODE_ID} at ${AGENT_URL}"
run_clean \
  NODE_ID="${NODE_ID}" \
  AGENT_ADDR="${AGENT_ADDR}" \
  AGENT_ADVERTISE_ADDR="${AGENT_URL}" \
  COORDINATOR_URL="${COORD_URL}" \
  AGENT_COMMAND_ALLOWLIST="echo=builtin:echo" \
  AGENT_CAPABILITIES="profile:doctor-smoke,role:worker" \
  "${BIN_DIR}/agent" >"${AGENT_LOG}" 2>&1 &
AGENT_PID=$!
wait_for_url "agent" "${AGENT_URL}/healthz"
wait_for_node

echo
echo "Healthy private-mesh doctor"
DOCTOR_HUMAN="$(pmctl doctor)"
printf '%s\n' "${DOCTOR_HUMAN}"
grep -Fq "Overall: PASS" <<<"${DOCTOR_HUMAN}"
grep -Fq "Ready for job submission: yes" <<<"${DOCTOR_HUMAN}"

DOCTOR_JSON="$(pmctl --json doctor)"
require_doctor_json "${DOCTOR_JSON}" "PASS" "true" "1" "1"
require_empty_jobs

echo
echo "Doctor smoke completed successfully"
echo "Logs are in ${LOG_DIR}"
