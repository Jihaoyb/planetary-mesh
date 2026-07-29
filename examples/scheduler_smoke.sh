#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

COORD_ADDR="${COORD_ADDR:-:18380}"
COORD_URL="${COORD_URL:-http://localhost:18380}"
NONMATCH_ADDR="${NONMATCH_ADDR:-:18381}"
NONMATCH_URL="${NONMATCH_URL:-http://localhost:18381}"
MATCH_ADDR="${MATCH_ADDR:-:18382}"
MATCH_URL="${MATCH_URL:-http://localhost:18382}"
NONMATCH_ID="${NONMATCH_ID:-scheduler-a-nonmatching}"
MATCH_ID="${MATCH_ID:-scheduler-b-matching}"

LOG_DIR="${LOG_DIR:-${TMPDIR:-/tmp}/planetary-mesh-scheduler-$(date +%Y%m%d%H%M%S)}"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-scheduler-bin.XXXXXX")"
COORD_LOG="${LOG_DIR}/coordinator.log"
NONMATCH_LOG="${LOG_DIR}/nonmatching-agent.log"
MATCH_LOG="${LOG_DIR}/matching-agent.log"

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
  for pid in "${MATCH_PID:-}" "${NONMATCH_PID:-}" "${COORD_PID:-}"; do
    if [[ -n "${pid}" ]]; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
  for pid in "${MATCH_PID:-}" "${NONMATCH_PID:-}" "${COORD_PID:-}"; do
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

wait_for_nodes() {
  local nodes_json

  for _ in {1..80}; do
    nodes_json="$(pmctl --json nodes list 2>/dev/null || true)"
    if [[ -n "${nodes_json}" ]] && python3 -c '
import json
import sys

nodes = {node.get("id"): node for node in json.load(sys.stdin)}
expected = {
    sys.argv[1]: ["profile:scheduler-smoke", "role:other"],
    sys.argv[2]: ["profile:scheduler-smoke", "role:scheduler-match"],
}
ok = all(
    nodes.get(node_id, {}).get("state") == "HEALTHY"
    and nodes.get(node_id, {}).get("capabilities") == capabilities
    for node_id, capabilities in expected.items()
)
sys.exit(0 if ok else 1)
' "${NONMATCH_ID}" "${MATCH_ID}" <<<"${nodes_json}"; then
      return 0
    fi
    sleep 0.25
  done

  echo "Timed out waiting for scheduler smoke agents" >&2
  echo "Logs are in ${LOG_DIR}" >&2
  return 1
}

wait_for_job() {
  local job_id="$1"
  local detail status

  for _ in {1..120}; do
    detail="$(pmctl --json jobs inspect "${job_id}")"
    status="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("status", ""))' <<<"${detail}")"
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
require_command grep

mkdir -p "${LOG_DIR}"

echo "Building scheduler smoke binaries"
(cd "${ROOT}" && go build -o "${BIN_DIR}/coordinator" ./cmd/coordinator)
(cd "${ROOT}" && go build -o "${BIN_DIR}/agent" ./cmd/agent)
(cd "${ROOT}" && go build -o "${BIN_DIR}/pmctl" ./cmd/pmctl)

echo "Starting coordinator at ${COORD_URL}"
run_clean COORDINATOR_ADDR="${COORD_ADDR}" "${BIN_DIR}/coordinator" >"${COORD_LOG}" 2>&1 &
COORD_PID=$!
wait_for_url "coordinator" "${COORD_URL}/healthz"

echo "Starting nonmatching agent ${NONMATCH_ID}"
run_clean \
  NODE_ID="${NONMATCH_ID}" \
  AGENT_ADDR="${NONMATCH_ADDR}" \
  AGENT_ADVERTISE_ADDR="${NONMATCH_URL}" \
  COORDINATOR_URL="${COORD_URL}" \
  AGENT_COMMAND_ALLOWLIST="echo=builtin:echo" \
  AGENT_CAPABILITIES="profile:scheduler-smoke,role:other" \
  "${BIN_DIR}/agent" >"${NONMATCH_LOG}" 2>&1 &
NONMATCH_PID=$!
wait_for_url "nonmatching agent" "${NONMATCH_URL}/healthz"

echo "Starting matching agent ${MATCH_ID}"
run_clean \
  NODE_ID="${MATCH_ID}" \
  AGENT_ADDR="${MATCH_ADDR}" \
  AGENT_ADVERTISE_ADDR="${MATCH_URL}" \
  COORDINATOR_URL="${COORD_URL}" \
  AGENT_COMMAND_ALLOWLIST="echo=builtin:echo" \
  AGENT_CAPABILITIES="profile:scheduler-smoke,role:scheduler-match" \
  "${BIN_DIR}/agent" >"${MATCH_LOG}" 2>&1 &
MATCH_PID=$!
wait_for_url "matching agent" "${MATCH_URL}/healthz"

wait_for_nodes

echo "Submitting constrained command"
CONSTRAINED_JSON="$(
  pmctl --json submit command \
    --require-capability "role:scheduler-match" \
    --require-capability "profile:scheduler-smoke" \
    echo "matching only"
)"
CONSTRAINED_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${CONSTRAINED_JSON}")"
CONSTRAINED_FINAL="$(wait_for_job "${CONSTRAINED_ID}")"
python3 -c '
import json
import sys

job = json.load(sys.stdin)
ok = (
    job.get("status") == "COMPLETED"
    and job.get("node_id") == sys.argv[1]
    and job.get("required_capabilities") == ["profile:scheduler-smoke", "role:scheduler-match"]
    and job.get("stdout") == "matching only\n"
)
sys.exit(0 if ok else 1)
' "${MATCH_ID}" <<<"${CONSTRAINED_FINAL}"
if grep -Fq "job_id=${CONSTRAINED_ID}" "${NONMATCH_LOG}"; then
  echo "Nonmatching agent executed constrained job ${CONSTRAINED_ID}" >&2
  exit 1
fi

echo "Submitting job with unavailable capability"
UNAVAILABLE_JSON="$(pmctl --json submit command --require-capability "role:unavailable" echo "must queue")"
UNAVAILABLE_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${UNAVAILABLE_JSON}")"
sleep 6
UNAVAILABLE_FINAL="$(pmctl --json jobs inspect "${UNAVAILABLE_ID}")"
python3 -c '
import json
import sys

job = json.load(sys.stdin)
ok = (
    job.get("status") == "QUEUED"
    and job.get("attempts") == 0
    and "node_id" not in job
    and job.get("required_capabilities") == ["role:unavailable"]
)
sys.exit(0 if ok else 1)
' <<<"${UNAVAILABLE_FINAL}"
if grep -Fq "job_id=${UNAVAILABLE_ID}" "${NONMATCH_LOG}" ||
  grep -Fq "job_id=${UNAVAILABLE_ID}" "${MATCH_LOG}"; then
  echo "An agent was contacted for unavailable job ${UNAVAILABLE_ID}" >&2
  exit 1
fi

echo "Submitting ordinary unconstrained command"
ORDINARY_JSON="$(pmctl --json submit command echo "ordinary submission")"
ORDINARY_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${ORDINARY_JSON}")"
ORDINARY_FINAL="$(wait_for_job "${ORDINARY_ID}")"
python3 -c '
import json
import sys

job = json.load(sys.stdin)
ok = (
    job.get("status") == "COMPLETED"
    and job.get("required_capabilities") == []
    and job.get("stdout") == "ordinary submission\n"
)
sys.exit(0 if ok else 1)
' <<<"${ORDINARY_FINAL}"

echo
echo "Scheduler smoke completed successfully"
echo "Logs are in ${LOG_DIR}"
