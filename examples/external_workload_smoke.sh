#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COORD_ADDR="${COORD_ADDR:-:18080}"
COORD_URL="${COORD_URL:-http://localhost:18080}"
AGENT_ADDR="${AGENT_ADDR:-:18081}"
AGENT_URL="${AGENT_URL:-http://localhost:18081}"
NODE_ID="${NODE_ID:-external-workload-agent}"

LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-external-workload-$(date +%Y%m%d%H%M%S)}"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-external-workload-bin.XXXXXX")"

COORD_LOG="${LOG_DIR}/coordinator.log"
AGENT_LOG="${LOG_DIR}/agent.log"
INPUT_PATH="${LOG_DIR}/input.txt"
WORKLOAD_BIN="${BIN_DIR}/text-stats"

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
  local nodes_text

  for _ in {1..80}; do
    nodes_text="$(pmctl nodes list 2>/dev/null || true)"
    if [[ -n "${nodes_text}" ]] && awk -v id="${NODE_ID}" '$1 == id && $2 == "HEALTHY" { found = 1 } END { exit found ? 0 : 1 }' <<<"${nodes_text}"; then
      return 0
    fi
    sleep 0.25
  done

  echo "Timed out waiting for ${NODE_ID} to register as HEALTHY" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

wait_for_job() {
  local job_id="$1"
  local detail status

  for _ in {1..120}; do
    detail="$(pmctl --json jobs inspect "${job_id}")"
    status="$(json_string_field "${detail}" "status")"
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

json_string_field() {
  local json="$1"
  local field="$2"

  sed -n "s/^[[:space:]]*\"${field}\": \"\\([^\"]*\\)\",\\{0,1\\}$/\\1/p" <<<"${json}" | head -n 1
}

json_int_field() {
  local json="$1"
  local field="$2"

  sed -n "s/^[[:space:]]*\"${field}\": \\([0-9][0-9]*\\),\\{0,1\\}$/\\1/p" <<<"${json}" | head -n 1
}

require_json_contains() {
  local json="$1"
  local expected="$2"

  if ! grep -Fq "${expected}" <<<"${json}"; then
    echo "Expected job JSON to contain: ${expected}" >&2
    echo "${json}" >&2
    return 1
  fi
}

require_command go
require_command curl
require_command awk
require_command grep
require_command sed

mkdir -p "${LOG_DIR}"
printf 'alpha\nbeta\ngamma\n' >"${INPUT_PATH}"

echo "Building external workload smoke binaries"
(cd "${ROOT}" && go build -o "${BIN_DIR}/coordinator" ./cmd/coordinator)
(cd "${ROOT}" && go build -o "${BIN_DIR}/agent" ./cmd/agent)
(cd "${ROOT}" && go build -o "${BIN_DIR}/pmctl" ./cmd/pmctl)
(cd "${ROOT}" && go build -o "${WORKLOAD_BIN}" ./examples/workloads/text-stats)

echo "Starting coordinator at ${COORD_URL}"
run_clean COORDINATOR_ADDR="${COORD_ADDR}" "${BIN_DIR}/coordinator" >"${COORD_LOG}" 2>&1 &
COORD_PID=$!
wait_for_url "coordinator" "${COORD_URL}/healthz"

echo "Starting agent ${NODE_ID} at ${AGENT_URL}"
run_clean \
  NODE_ID="${NODE_ID}" \
  AGENT_ADDR="${AGENT_ADDR}" \
  AGENT_ADVERTISE_ADDR="${AGENT_URL}" \
  COORDINATOR_URL="${COORD_URL}" \
  AGENT_COMMAND_ALLOWLIST="echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=${WORKLOAD_BIN}" \
  AGENT_CAPABILITIES="profile:local,role:text-worker" \
  "${BIN_DIR}/agent" >"${AGENT_LOG}" 2>&1 &
AGENT_PID=$!
wait_for_url "agent" "${AGENT_URL}/healthz"

echo "Waiting for ${NODE_ID} to register"
wait_for_node

echo
echo "Registered nodes"
pmctl nodes list

echo
echo "Submitting external text-stats workload"
JOB_JSON="$(pmctl --json submit command text-stats "${INPUT_PATH}")"
JOB_ID="$(json_string_field "${JOB_JSON}" "id")"
if [[ -z "${JOB_ID}" ]]; then
  echo "Could not parse submitted job id" >&2
  echo "${JOB_JSON}" >&2
  exit 1
fi
echo "Created ${JOB_ID}"

FINAL_JOB_JSON="$(wait_for_job "${JOB_ID}")"
ATTEMPTS="$(json_int_field "${FINAL_JOB_JSON}" "attempts")"
if [[ -z "${ATTEMPTS}" || "${ATTEMPTS}" -lt 1 ]]; then
  echo "Expected job attempts to be at least 1" >&2
  echo "${FINAL_JOB_JSON}" >&2
  exit 1
fi
if ! require_json_contains "${FINAL_JOB_JSON}" '"status": "COMPLETED",' ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"command": "text-stats",' ||
  ! require_json_contains "${FINAL_JOB_JSON}" "\"node_id\": \"${NODE_ID}\"," ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"stdout": "lines=3\nnon_empty_lines=3\nwords=3\n",' ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"stderr": "",' ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"stdout_truncated": false,' ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"stderr_truncated": false,' ||
  ! require_json_contains "${FINAL_JOB_JSON}" '"last_error": ""'; then
  echo "Unexpected external workload result" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  exit 1
fi

echo
echo "Job detail"
pmctl jobs inspect "${JOB_ID}"

echo
echo "External workload smoke completed successfully"
echo "Logs are in ${LOG_DIR}"
