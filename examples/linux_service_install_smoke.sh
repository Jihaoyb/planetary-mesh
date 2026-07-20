#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-service-smoke}"
GOARCH_TARGET="${GOARCH_TARGET:-$(go env GOARCH)}"
DIST_DIR="${DIST_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-linux-service-dist.XXXXXX")}"
CASE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/planetary-mesh-linux-service-cases.XXXXXX")"
INSTALL_DIR="${DIST_DIR}/planetary-mesh-${VERSION}-linux-${GOARCH_TARGET}"
ARCHIVE_PATH="${INSTALL_DIR}.tar.gz"

cleanup() {
  if [[ "${KEEP_LINUX_SERVICE_SMOKE:-}" == "1" ]]; then
    echo "Preserving Linux service smoke dist directory: ${DIST_DIR}"
    echo "Preserving Linux service smoke case directory: ${CASE_DIR}"
    return
  fi
  case "${DIST_DIR}" in
    "${TMPDIR:-/tmp}"/planetary-mesh-linux-service-dist.* | /tmp/planetary-mesh-linux-service-dist.*)
      rm -rf "${DIST_DIR}"
      ;;
  esac
  case "${CASE_DIR}" in
    "${TMPDIR:-/tmp}"/planetary-mesh-linux-service-cases.* | /tmp/planetary-mesh-linux-service-cases.*)
      rm -rf "${CASE_DIR}"
      ;;
  esac
}
trap cleanup EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_file() {
  if [[ ! -f "$1" ]]; then
    echo "Expected file at $1" >&2
    exit 1
  fi
}

require_executable() {
  if [[ ! -x "$1" ]]; then
    echo "Expected executable at $1" >&2
    exit 1
  fi
}

file_mode() {
  if stat -c '%a' "$1" >/dev/null 2>&1; then
    stat -c '%a' "$1"
  else
    stat -f '%Lp' "$1"
  fi
}

require_mode() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(file_mode "${path}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Expected mode ${expected} at ${path}, got ${actual}" >&2
    exit 1
  fi
}

expect_failure() {
  local expected_code="$1"
  local expected_text="$2"
  shift 2
  local output_path="${CASE_DIR}/failure-output"
  local actual_code=0

  "$@" >"${output_path}" 2>&1 || actual_code=$?
  if [[ "${actual_code}" -ne "${expected_code}" ]]; then
    echo "Expected exit ${expected_code}, got ${actual_code}: $*" >&2
    sed -n '1,20p' "${output_path}" >&2
    exit 1
  fi
  if ! grep -Fq "${expected_text}" "${output_path}"; then
    echo "Expected failure output to contain: ${expected_text}" >&2
    sed -n '1,20p' "${output_path}" >&2
    exit 1
  fi
}

require_empty_file_inventory() {
  local root="$1"
  local inventory="${CASE_DIR}/inventory"
  find "${root}" \( -type f -o -type l \) -print >"${inventory}"
  if [[ -s "${inventory}" ]]; then
    echo "Expected no files or symlinks under ${root}" >&2
    sed -n '1,40p' "${inventory}" >&2
    exit 1
  fi
}

verify_systemd_units() {
  if ! command -v systemd-analyze >/dev/null 2>&1; then
    echo "Systemd unit verification: Not run (systemd-analyze unavailable)"
    return
  fi

  local verify_root="${CASE_DIR}/systemd-root"
  local system_units="${verify_root}/etc/systemd/system"
  mkdir -p "${system_units}" \
    "${verify_root}/opt/planetary-mesh/bin" \
    "${verify_root}/var/lib/planetary-mesh/coordinator" \
    "${verify_root}/var/lib/planetary-mesh/agent" \
    "${verify_root}/etc/planetary-mesh" \
    "${verify_root}/usr/lib/systemd/system"
  cp "${INSTALL_DIR}/install/systemd/planetary-mesh-coordinator.service" "${system_units}/"
  cp "${INSTALL_DIR}/install/systemd/planetary-mesh-agent.service" "${system_units}/"
  printf '#!/bin/sh\nexit 0\n' >"${verify_root}/opt/planetary-mesh/bin/coordinator"
  printf '#!/bin/sh\nexit 0\n' >"${verify_root}/opt/planetary-mesh/bin/agent"
  chmod 0755 "${verify_root}/opt/planetary-mesh/bin/coordinator" "${verify_root}/opt/planetary-mesh/bin/agent"
  printf 'planetary-mesh-coordinator:x:998:998::/var/lib/planetary-mesh/coordinator:/usr/sbin/nologin\nplanetary-mesh-agent:x:997:997::/var/lib/planetary-mesh/agent:/usr/sbin/nologin\n' >"${verify_root}/etc/passwd"
  printf 'planetary-mesh-coordinator:x:998:\nplanetary-mesh-agent:x:997:\n' >"${verify_root}/etc/group"
  for target in basic sysinit shutdown network-online multi-user; do
    printf '[Unit]\nDescription=Smoke target\n' >"${verify_root}/usr/lib/systemd/system/${target}.target"
  done

  if systemd-analyze verify --root="${verify_root}" planetary-mesh-coordinator.service planetary-mesh-agent.service; then
    echo "Systemd unit verification: Passed"
  else
    echo "Systemd unit verification: Failed" >&2
    exit 1
  fi
}

require_command go
require_command bash
require_command cmp
require_command cp
require_command find
require_command grep
require_command sed
require_command stat
require_command tar

echo "Building Linux release layout in ${DIST_DIR}"
(cd "${ROOT}" && go run ./tools/releasebuild --out "${DIST_DIR}" --version "${VERSION}" --targets "linux/${GOARCH_TARGET}")

INSTALL_SCRIPT="${INSTALL_DIR}/install/install-linux.sh"
UNINSTALL_SCRIPT="${INSTALL_DIR}/install/uninstall-linux.sh"
COORD_UNIT="${INSTALL_DIR}/install/systemd/planetary-mesh-coordinator.service"
AGENT_UNIT="${INSTALL_DIR}/install/systemd/planetary-mesh-agent.service"
require_executable "${INSTALL_SCRIPT}"
require_executable "${UNINSTALL_SCRIPT}"
require_file "${COORD_UNIT}"
require_file "${AGENT_UNIT}"
require_file "${ARCHIVE_PATH}"
require_mode "${INSTALL_SCRIPT}" 755
require_mode "${UNINSTALL_SCRIPT}" 755
require_mode "${COORD_UNIT}" 644
require_mode "${AGENT_UNIT}" 644

archive_inventory="$(tar -tzf "${ARCHIVE_PATH}")"
for expected_path in \
  "planetary-mesh-${VERSION}-linux-${GOARCH_TARGET}/install/install-linux.sh" \
  "planetary-mesh-${VERSION}-linux-${GOARCH_TARGET}/install/uninstall-linux.sh" \
  "planetary-mesh-${VERSION}-linux-${GOARCH_TARGET}/install/systemd/planetary-mesh-coordinator.service" \
  "planetary-mesh-${VERSION}-linux-${GOARCH_TARGET}/install/systemd/planetary-mesh-agent.service"; do
  if ! grep -Fxq "${expected_path}" <<<"${archive_inventory}"; then
    echo "Expected archive entry: ${expected_path}" >&2
    exit 1
  fi
done

COORD_CONFIG="${CASE_DIR}/coordinator.env"
AGENT_CONFIG="${CASE_DIR}/agent.env"
BAD_CONFIG="${CASE_DIR}/bad.env"
SYMLINK_CONFIG="${CASE_DIR}/config-link.env"
printf 'COORDINATOR_ADDR=:8080\n' >"${COORD_CONFIG}"
printf 'NODE_ID=service-smoke-agent\nAGENT_ADDR=:8081\nAGENT_ADVERTISE_ADDR=http://localhost:8081\nCOORDINATOR_URL=http://localhost:8080\nAGENT_COMMAND_ALLOWLIST=echo=builtin:echo\n' >"${AGENT_CONFIG}"
printf 'BROKEN CONFIG secret-sentinel\n' >"${BAD_CONFIG}"
ln -s "${COORD_CONFIG}" "${SYMLINK_CONFIG}"

expect_failure 2 "ERROR[2] role is required; expected coordinator or agent" "${INSTALL_SCRIPT}"
expect_failure 2 "ERROR[2] unsupported role: worker; expected coordinator or agent" "${INSTALL_SCRIPT}" worker
expect_failure 2 "ERROR[2] exactly one of --config or --reuse-config is required" "${INSTALL_SCRIPT}" coordinator --root "${CASE_DIR}" --no-start
expect_failure 2 "ERROR[2] --config and --reuse-config are mutually exclusive" "${INSTALL_SCRIPT}" coordinator --config "${COORD_CONFIG}" --reuse-config --root "${CASE_DIR}" --no-start
expect_failure 2 "ERROR[2] --config must be an absolute path" "${INSTALL_SCRIPT}" coordinator --config relative.env --root "${CASE_DIR}" --no-start
expect_failure 3 "ERROR[3] config must be a readable regular file" "${INSTALL_SCRIPT}" coordinator --config "${SYMLINK_CONFIG}" --root "${CASE_DIR}" --no-start
expect_failure 5 "ERROR[5] invalid env-style config" "${INSTALL_SCRIPT}" coordinator --config "${BAD_CONFIG}" --root "${CASE_DIR}" --no-start
if grep -Fq 'secret-sentinel' "${CASE_DIR}/failure-output"; then
  echo "Installer failure output exposed a config value" >&2
  exit 1
fi
expect_failure 3 "ERROR[3] missing release binary" "${ROOT}/packaging/linux/install-linux.sh" coordinator --config "${COORD_CONFIG}" --root "${CASE_DIR}" --no-start

BROKEN_RELEASE="${CASE_DIR}/broken-release"
cp -R "${INSTALL_DIR}" "${BROKEN_RELEASE}"
rm -f "${BROKEN_RELEASE}/install/systemd/planetary-mesh-agent.service"
expect_failure 3 "ERROR[3] missing release binary" "${BROKEN_RELEASE}/install/install-linux.sh" agent --config "${AGENT_CONFIG}" --root "${CASE_DIR}" --no-start

COORD_ROOT="${CASE_DIR}/coordinator-root"
AGENT_ROOT="${CASE_DIR}/agent-root"
mkdir -p "${COORD_ROOT}" "${AGENT_ROOT}"

"${INSTALL_SCRIPT}" coordinator --config "${COORD_CONFIG}" --root "${COORD_ROOT}" --no-start
require_executable "${COORD_ROOT}/opt/planetary-mesh/bin/coordinator"
require_executable "${COORD_ROOT}/usr/local/bin/pmctl"
require_file "${COORD_ROOT}/usr/share/planetary-mesh/templates/text-stats.pmtemplate.json"
require_file "${COORD_ROOT}/etc/planetary-mesh/coordinator.env"
require_file "${COORD_ROOT}/etc/systemd/system/planetary-mesh-coordinator.service"
require_file "${COORD_ROOT}/var/lib/planetary-mesh/.managed-coordinator"
[[ ! -e "${COORD_ROOT}/opt/planetary-mesh/bin/agent" ]] || { echo "Coordinator install unexpectedly installed agent" >&2; exit 1; }
require_mode "${COORD_ROOT}/opt/planetary-mesh/bin/coordinator" 755
require_mode "${COORD_ROOT}/usr/local/bin/pmctl" 755
require_mode "${COORD_ROOT}/etc/planetary-mesh/coordinator.env" 640
require_mode "${COORD_ROOT}/etc/systemd/system/planetary-mesh-coordinator.service" 644
require_mode "${COORD_ROOT}/var/lib/planetary-mesh/.managed-coordinator" 600
expect_failure 4 "ERROR[4] coordinator is already installed" "${INSTALL_SCRIPT}" coordinator --reuse-config --root "${COORD_ROOT}" --no-start

cp "${COORD_ROOT}/etc/planetary-mesh/coordinator.env" "${CASE_DIR}/coordinator-preserved.env"
"${UNINSTALL_SCRIPT}" coordinator --root "${COORD_ROOT}"
cmp "${CASE_DIR}/coordinator-preserved.env" "${COORD_ROOT}/etc/planetary-mesh/coordinator.env"
expect_failure 4 "ERROR[4] managed config already exists" "${INSTALL_SCRIPT}" coordinator --config "${COORD_CONFIG}" --root "${COORD_ROOT}" --no-start
"${INSTALL_SCRIPT}" coordinator --reuse-config --root "${COORD_ROOT}" --no-start
"${UNINSTALL_SCRIPT}" coordinator --purge --root "${COORD_ROOT}"
require_empty_file_inventory "${COORD_ROOT}"

"${INSTALL_SCRIPT}" agent --config "${AGENT_CONFIG}" --root "${AGENT_ROOT}" --no-start
require_executable "${AGENT_ROOT}/opt/planetary-mesh/bin/agent"
require_executable "${AGENT_ROOT}/opt/planetary-mesh/workloads/text-stats"
require_file "${AGENT_ROOT}/etc/planetary-mesh/agent.env"
require_file "${AGENT_ROOT}/etc/systemd/system/planetary-mesh-agent.service"
require_file "${AGENT_ROOT}/var/lib/planetary-mesh/.managed-agent"
[[ ! -e "${AGENT_ROOT}/opt/planetary-mesh/bin/coordinator" ]] || { echo "Agent install unexpectedly installed coordinator" >&2; exit 1; }
printf 'operator data\n' >"${AGENT_ROOT}/var/lib/planetary-mesh/agent/work/input.txt"
expect_failure 4 "agent work directory is not empty; refusing purge" "${UNINSTALL_SCRIPT}" agent --purge --root "${AGENT_ROOT}"
require_executable "${AGENT_ROOT}/opt/planetary-mesh/bin/agent"
rm -f "${AGENT_ROOT}/var/lib/planetary-mesh/agent/work/input.txt"
"${UNINSTALL_SCRIPT}" agent --root "${AGENT_ROOT}"
"${UNINSTALL_SCRIPT}" agent --root "${AGENT_ROOT}"
"${INSTALL_SCRIPT}" agent --reuse-config --root "${AGENT_ROOT}" --no-start
"${UNINSTALL_SCRIPT}" agent --purge --root "${AGENT_ROOT}"
require_empty_file_inventory "${AGENT_ROOT}"

FAULT_ROOT="${CASE_DIR}/fault-root"
mkdir -p "${FAULT_ROOT}"
expect_failure 6 "ERROR[6] injected install failure after unit; rollback completed" env PLANETARY_MESH_INSTALL_FAIL_AFTER=unit "${INSTALL_SCRIPT}" coordinator --config "${COORD_CONFIG}" --root "${FAULT_ROOT}" --no-start
require_empty_file_inventory "${FAULT_ROOT}"

verify_systemd_units

echo "Linux service install smoke completed successfully"
echo "Validated archive: ${ARCHIVE_PATH}"
