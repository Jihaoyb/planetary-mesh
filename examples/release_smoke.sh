#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COORD_ADDR="${COORD_ADDR:-:19080}"
COORD_URL="${COORD_URL:-http://localhost:19080}"
AGENT_ADDR="${AGENT_ADDR:-:19081}"
AGENT_URL="${AGENT_URL:-http://localhost:19081}"
NODE_ID="${NODE_ID:-release-smoke-agent}"

LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-release-smoke-$(date +%Y%m%d%H%M%S)}"
DIST_DIR="${DIST_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-release-dist.XXXXXX")}"
VERSION="${VERSION:-dev}"

COORD_LOG="${LOG_DIR}/coordinator.log"
AGENT_LOG="${LOG_DIR}/agent.log"
INPUT_PATH="${LOG_DIR}/input.txt"

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
  if [[ "${KEEP_RELEASE_SMOKE:-}" == "1" ]]; then
    echo "Preserving release smoke dist directory: ${DIST_DIR}"
  elif [[ "${DIST_DIR}" == "${TMPDIR:-/tmp}"/planetary-mesh-release-dist.* || "${DIST_DIR}" == /tmp/planetary-mesh-release-dist.* ]]; then
    rm -rf "${DIST_DIR}"
  fi
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

require_executable() {
  local path="$1"
  if [[ ! -x "${path}" ]]; then
    echo "Expected executable at ${path}" >&2
    exit 1
  fi
}

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    echo "Expected file at ${path}" >&2
    exit 1
  fi
}

pmctl() {
  run_clean "${PMCTL_BIN}" --coordinator-url "${COORD_URL}" "$@"
}

require_command go
require_command curl
require_command awk
require_command grep
require_command sed

HOST_GOOS="$(go env GOOS)"
HOST_GOARCH="$(go env GOARCH)"
EXE=""
ARCHIVE_EXT=".tar.gz"
if [[ "${HOST_GOOS}" == "windows" ]]; then
  EXE=".exe"
  ARCHIVE_EXT=".zip"
fi

INSTALL_DIR="${DIST_DIR}/planetary-mesh-${VERSION}-${HOST_GOOS}-${HOST_GOARCH}"
ARCHIVE_PATH="${DIST_DIR}/planetary-mesh-${VERSION}-${HOST_GOOS}-${HOST_GOARCH}${ARCHIVE_EXT}"
COORD_BIN="${INSTALL_DIR}/bin/coordinator${EXE}"
AGENT_BIN="${INSTALL_DIR}/bin/agent${EXE}"
PMCTL_BIN="${INSTALL_DIR}/bin/pmctl${EXE}"
WORKLOAD_BIN="${INSTALL_DIR}/workloads/text-stats${EXE}"
TEMPLATE_PATH="${INSTALL_DIR}/templates/text-stats.pmtemplate.json"

mkdir -p "${LOG_DIR}"
printf 'alpha\nbeta\ngamma\n' >"${INPUT_PATH}"

echo "Building host release layout in ${DIST_DIR}"
(cd "${ROOT}" && go run ./tools/releasebuild --out "${DIST_DIR}" --version "${VERSION}" --targets host)

require_executable "${COORD_BIN}"
require_executable "${AGENT_BIN}"
require_executable "${PMCTL_BIN}"
require_executable "${WORKLOAD_BIN}"
require_file "${TEMPLATE_PATH}"
if [[ ! -f "${ARCHIVE_PATH}" ]]; then
  echo "Expected archive at ${ARCHIVE_PATH}" >&2
  exit 1
fi

echo "Starting installed coordinator at ${COORD_URL}"
run_clean COORDINATOR_ADDR="${COORD_ADDR}" "${COORD_BIN}" >"${COORD_LOG}" 2>&1 &
COORD_PID=$!
wait_for_url "coordinator" "${COORD_URL}/healthz"

echo "Starting installed agent ${NODE_ID} at ${AGENT_URL}"
run_clean \
  NODE_ID="${NODE_ID}" \
  AGENT_ADDR="${AGENT_ADDR}" \
  AGENT_ADVERTISE_ADDR="${AGENT_URL}" \
  COORDINATOR_URL="${COORD_URL}" \
  AGENT_COMMAND_ALLOWLIST="echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,text-stats=${WORKLOAD_BIN}" \
  AGENT_CAPABILITIES="profile:local-release,role:text-worker" \
  "${AGENT_BIN}" >"${AGENT_LOG}" 2>&1 &
AGENT_PID=$!
wait_for_url "agent" "${AGENT_URL}/healthz"

echo "Waiting for ${NODE_ID} to register"
wait_for_node

echo
echo "Installed layout"
echo "${INSTALL_DIR}"

echo
echo "Registered nodes"
pmctl nodes list

echo
echo "Validating installed text-stats template"
TEMPLATE_JSON="$(pmctl --json templates validate "${TEMPLATE_PATH}")"
if ! require_json_contains "${TEMPLATE_JSON}" '"valid": true,' ||
  ! require_json_contains "${TEMPLATE_JSON}" '"name": "text-stats",' ||
  ! require_json_contains "${TEMPLATE_JSON}" '"command": "text-stats"'; then
  echo "Unexpected installed template validation result" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  exit 1
fi

echo
echo "Submitting installed text-stats template"
JOB_JSON="$(pmctl --json submit template "${TEMPLATE_PATH}" --set "input_path=${INPUT_PATH}")"
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
  echo "Unexpected release smoke workload result" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  exit 1
fi

echo
echo "Job detail"
pmctl jobs inspect "${JOB_ID}"

echo
echo "Release smoke completed successfully"
echo "Archive: ${ARCHIVE_PATH}"
echo "Logs are in ${LOG_DIR}"
