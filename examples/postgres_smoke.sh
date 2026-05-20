#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COMPOSE_PROJECT="${COMPOSE_PROJECT:-planetary-mesh-postgres-smoke}"
POSTGRES_HOST_PORT="${POSTGRES_HOST_PORT:-15432}"
COORDINATOR_HOST_PORT="${COORDINATOR_HOST_PORT:-18080}"
AGENT1_HOST_PORT="${AGENT1_HOST_PORT:-18081}"
AGENT2_HOST_PORT="${AGENT2_HOST_PORT:-18082}"

COORD_URL="${COORD_URL:-http://localhost:${COORDINATOR_HOST_PORT}}"
LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-postgres-smoke-$(date +%Y%m%d%H%M%S)}"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-postgres-smoke-bin.XXXXXX")"

export POSTGRES_HOST_PORT
export COORDINATOR_HOST_PORT
export AGENT1_HOST_PORT
export AGENT2_HOST_PORT

COMPOSE=(docker compose -p "${COMPOSE_PROJECT}" -f "${ROOT}/compose.yaml")

UNSET_PMCTL_ENV=(
  -u PMCTL_CONFIG_FILE
  -u PMCTL_COORDINATOR_URL
  -u PMCTL_TLS_CA_FILE
  -u PMCTL_TLS_CERT_FILE
  -u PMCTL_TLS_KEY_FILE
)

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

print_logs() {
  mkdir -p "${LOG_DIR}"
  echo "Writing Compose logs to ${LOG_DIR}" >&2
  "${COMPOSE[@]}" logs --no-color postgres >"${LOG_DIR}/postgres.log" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color coordinator >"${LOG_DIR}/coordinator.log" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color agent-1 >"${LOG_DIR}/agent-1.log" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color agent-2 >"${LOG_DIR}/agent-2.log" 2>&1 || true
}

cleanup() {
  local status=$?
  set +e

  if [[ "${status}" -ne 0 ]]; then
    print_logs
  fi

  if [[ "${KEEP_POSTGRES_SMOKE:-}" == "1" ]]; then
    echo "Preserving Compose project ${COMPOSE_PROJECT} for inspection"
    echo "Clean up later with: docker compose -p ${COMPOSE_PROJECT} -f ${ROOT}/compose.yaml down -v --remove-orphans"
  else
    "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  fi

  rm -rf "${BIN_DIR}"
  exit "${status}"
}
trap cleanup EXIT

pmctl() {
  env "${UNSET_PMCTL_ENV[@]}" "${BIN_DIR}/pmctl" --coordinator-url "${COORD_URL}" "$@"
}

wait_for_url() {
  local name="$1"
  local url="$2"

  for _ in {1..120}; do
    if curl -sf "${url}" >/dev/null; then
      return 0
    fi
    sleep 0.5
  done

  echo "Timed out waiting for ${name} at ${url}" >&2
  return 1
}

wait_for_postgres_status() {
  local status_json

  for _ in {1..120}; do
    status_json="$(pmctl --json status 2>/dev/null || true)"
    if [[ -n "${status_json}" ]] && python3 -c '
import json
import sys

status = json.load(sys.stdin)
schema = status.get("schema") or {}
ok = (
    status.get("status") == "ok"
    and status.get("storage_backend") == "postgres"
    and schema.get("ready") is True
    and schema.get("version") == 2
    and schema.get("expected_version") == 2
)
sys.exit(0 if ok else 1)
' <<<"${status_json}"; then
      return 0
    fi
    sleep 0.5
  done

  echo "Timed out waiting for coordinator status with postgres storage" >&2
  return 1
}

wait_for_nodes() {
  local nodes_json

  for _ in {1..120}; do
    nodes_json="$(pmctl --json nodes list 2>/dev/null || true)"
    if [[ -n "${nodes_json}" ]] && python3 -c '
import json
import sys

nodes = json.load(sys.stdin)
ids = {node.get("id"): node.get("state") for node in nodes}
ok = ids.get("compose-agent-1") == "HEALTHY" and ids.get("compose-agent-2") == "HEALTHY"
sys.exit(0 if ok else 1)
' <<<"${nodes_json}"; then
      return 0
    fi
    sleep 0.5
  done

  echo "Timed out waiting for compose-agent-1 and compose-agent-2 to register as HEALTHY" >&2
  return 1
}

wait_for_job_status() {
  local job_id="$1"
  local expected="$2"
  local detail status

  for _ in {1..160}; do
    detail="$(pmctl --json jobs inspect "${job_id}" 2>/dev/null || true)"
    status="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("status", ""))' <<<"${detail}" 2>/dev/null || true)"
    if [[ "${status}" == "${expected}" ]]; then
      printf '%s\n' "${detail}"
      return 0
    fi
    sleep 0.5
  done

  echo "Timed out waiting for job ${job_id} to reach ${expected}" >&2
  return 1
}

require_metrics() {
  local metrics
  metrics="$(curl -sf -H 'X-Planetary-Protocol-Version: 1' "${COORD_URL}/metrics")"

  grep -q 'planetary_jobs_created_total' <<<"${metrics}"
  grep -q 'planetary_jobs_completed_total' <<<"${metrics}"
  grep -q 'planetary_jobs_recovered_on_startup_total 1' <<<"${metrics}"
  grep -q 'planetary_nodes{state="HEALTHY"}' <<<"${metrics}"
  grep -q 'planetary_postgres_schema_ready 1' <<<"${metrics}"
  grep -q 'planetary_postgres_schema_version 2' <<<"${metrics}"
  grep -q 'planetary_postgres_schema_expected_version 2' <<<"${metrics}"
}

require_command docker
require_command go
require_command curl
require_command python3
if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose is required: docker compose version failed" >&2
  exit 1
fi

mkdir -p "${LOG_DIR}"

echo "Building pmctl for Postgres smoke"
(cd "${ROOT}" && go build -o "${BIN_DIR}/pmctl" ./cmd/pmctl)

echo "Resetting Compose project ${COMPOSE_PROJECT}"
"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true

echo "Starting Postgres-backed mesh"
"${COMPOSE[@]}" up -d postgres coordinator agent-1 agent-2

wait_for_url "coordinator" "${COORD_URL}/healthz"
wait_for_postgres_status
wait_for_nodes

echo
echo "Coordinator status"
pmctl status

echo
echo "Registered nodes"
pmctl nodes list

echo
echo "Submitting durable echo job"
ECHO_JSON="$(pmctl --json submit command echo "hello from postgres smoke")"
ECHO_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${ECHO_JSON}")"
ECHO_FINAL="$(wait_for_job_status "${ECHO_ID}" "COMPLETED")"
ECHO_STDOUT="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["stdout"].strip())' <<<"${ECHO_FINAL}")"
if [[ "${ECHO_STDOUT}" != "hello from postgres smoke" ]]; then
  echo "Unexpected echo stdout: ${ECHO_STDOUT}" >&2
  exit 1
fi

echo
echo "Submitting long-running job for restart recovery"
SLEEP_JSON="$(pmctl --json submit command sleep 30)"
SLEEP_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${SLEEP_JSON}")"
wait_for_job_status "${SLEEP_ID}" "RUNNING" >/dev/null

echo "Restarting coordinator"
"${COMPOSE[@]}" restart coordinator
wait_for_url "coordinator" "${COORD_URL}/healthz"
wait_for_postgres_status

RECOVERED_JSON="$(wait_for_job_status "${SLEEP_ID}" "FAILED")"
RECOVERED_ERROR="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["last_error"])' <<<"${RECOVERED_JSON}")"
if [[ "${RECOVERED_ERROR}" != "coordinator restarted before result was recorded" ]]; then
  echo "Unexpected recovery error: ${RECOVERED_ERROR}" >&2
  exit 1
fi

wait_for_nodes

echo
echo "Submitting post-restart echo job"
AFTER_JSON="$(pmctl --json submit command echo "after postgres restart")"
AFTER_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${AFTER_JSON}")"
AFTER_FINAL="$(wait_for_job_status "${AFTER_ID}" "COMPLETED")"
AFTER_STDOUT="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["stdout"].strip())' <<<"${AFTER_FINAL}")"
if [[ "${AFTER_STDOUT}" != "after postgres restart" ]]; then
  echo "Unexpected post-restart stdout: ${AFTER_STDOUT}" >&2
  exit 1
fi

require_metrics

echo
echo "Jobs"
pmctl jobs list

echo
echo "Postgres smoke completed successfully"
echo "Logs directory: ${LOG_DIR}"
