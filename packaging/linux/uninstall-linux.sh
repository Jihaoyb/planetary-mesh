#!/usr/bin/env bash
set -uo pipefail

die() {
  local code="$1"
  shift
  printf 'ERROR[%s] %s\n' "${code}" "$*" >&2
  exit "${code}"
}

path_exists() {
  [[ -e "$1" || -L "$1" ]]
}

validate_marker() {
  local path="$1"
  local expected_role="$2"
  local marker_value

  [[ -f "${path}" && ! -L "${path}" ]] || return 1
  marker_value="$(<"${path}")" || return 1
  [[ "${marker_value}" == "role=${expected_role}" ]]
}

is_absolute() {
  [[ "$1" == /* ]]
}

ROLE="${1:-}"
if [[ -z "${ROLE}" ]]; then
  die 2 "role is required; expected coordinator or agent"
fi
shift
case "${ROLE}" in
  coordinator | agent) ;;
  *) die 2 "unsupported role: ${ROLE}; expected coordinator or agent" ;;
esac

PURGE=0
DEST_ROOT="/"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge)
      PURGE=1
      shift
      ;;
    --root)
      [[ $# -ge 2 ]] || die 2 "--root requires a path"
      DEST_ROOT="$2"
      shift 2
      ;;
    -h | --help)
      printf 'Usage: uninstall-linux.sh coordinator|agent [--purge] [--root ABSOLUTE_PATH]\n'
      exit 0
      ;;
    *) die 2 "unknown option: $1" ;;
  esac
done

STAGING=0
if [[ "${DEST_ROOT}" != "/" ]]; then
  is_absolute "${DEST_ROOT}" || die 2 "--root must be an absolute path"
  [[ -d "${DEST_ROOT}" && ! -L "${DEST_ROOT}" ]] || die 3 "staging root is not a regular directory: ${DEST_ROOT}"
  DEST_ROOT="$(cd "${DEST_ROOT}" && pwd -P)"
  [[ "${DEST_ROOT}" != "/" ]] || die 2 "--root must not resolve to /"
  STAGING=1
fi

rooted() {
  if [[ "${DEST_ROOT}" == "/" ]]; then
    printf '%s' "$1"
  else
    printf '%s%s' "${DEST_ROOT}" "$1"
  fi
}

SERVICE_USER="planetary-mesh-${ROLE}"
SERVICE_GROUP="${SERVICE_USER}"
UNIT="planetary-mesh-${ROLE}.service"
BINARY_DEST="$(rooted "/opt/planetary-mesh/bin/${ROLE}")"
UNIT_DEST="$(rooted "/etc/systemd/system/${UNIT}")"
CONFIG_DEST="$(rooted "/etc/planetary-mesh/${ROLE}.env")"
TLS_DIR="$(rooted "/etc/planetary-mesh/tls/${ROLE}")"
STATE_DIR="$(rooted "/var/lib/planetary-mesh/${ROLE}")"
WORK_DIR="$(rooted "/var/lib/planetary-mesh/agent/work")"
MARKER="$(rooted "/var/lib/planetary-mesh/.managed-${ROLE}")"
PMCTL_DEST="$(rooted "/usr/local/bin/pmctl")"
WORKLOAD_DEST="$(rooted "/opt/planetary-mesh/workloads/text-stats")"
TEMPLATE_DIR="$(rooted "/usr/share/planetary-mesh/templates")"

if [[ "${STAGING}" -eq 0 ]]; then
  [[ "$(uname -s)" == "Linux" ]] || die 3 "Linux with systemd is required"
  [[ "$(id -u)" == "0" ]] || die 3 "root privileges are required; rerun with sudo"
  for command_name in systemctl userdel groupdel getent ps; do
    command -v "${command_name}" >/dev/null 2>&1 || die 3 "missing required command: ${command_name}"
  done
fi

HAS_ASSETS=0
for path in "${BINARY_DEST}" "${UNIT_DEST}"; do
  path_exists "${path}" && HAS_ASSETS=1
done
if [[ "${ROLE}" == "coordinator" ]]; then
  path_exists "${PMCTL_DEST}" && HAS_ASSETS=1
  path_exists "${TEMPLATE_DIR}" && HAS_ASSETS=1
else
  path_exists "${WORKLOAD_DEST}" && HAS_ASSETS=1
fi

if [[ "${HAS_ASSETS}" -eq 1 ]] && ! validate_marker "${MARKER}" "${ROLE}"; then
  die 4 "managed marker is missing or invalid: ${MARKER}; refusing to remove unowned assets"
fi
if [[ "${PURGE}" -eq 1 ]] && ! validate_marker "${MARKER}" "${ROLE}"; then
  die 4 "managed marker is missing or invalid: ${MARKER}; refusing to purge unowned state"
fi

for managed_file in "${BINARY_DEST}" "${UNIT_DEST}"; do
  if path_exists "${managed_file}" && [[ ! -f "${managed_file}" || -L "${managed_file}" ]]; then
    die 4 "managed asset is not a regular file; refusing removal: ${managed_file}"
  fi
done
if [[ "${ROLE}" == "coordinator" ]]; then
  if path_exists "${PMCTL_DEST}" && [[ ! -f "${PMCTL_DEST}" || -L "${PMCTL_DEST}" ]]; then
    die 4 "managed asset is not a regular file; refusing removal: ${PMCTL_DEST}"
  fi
  if path_exists "${TEMPLATE_DIR}" && [[ ! -d "${TEMPLATE_DIR}" || -L "${TEMPLATE_DIR}" ]]; then
    die 4 "managed template path is not a regular directory; refusing removal: ${TEMPLATE_DIR}"
  fi
else
  if path_exists "${WORKLOAD_DEST}" && [[ ! -f "${WORKLOAD_DEST}" || -L "${WORKLOAD_DEST}" ]]; then
    die 4 "managed asset is not a regular file; refusing removal: ${WORKLOAD_DEST}"
  fi
fi

directory_empty() {
  local directory="$1"
  local item
  [[ -d "${directory}" ]] || return 0
  for item in "${directory}"/* "${directory}"/.[!.]* "${directory}"/..?*; do
    if path_exists "${item}"; then
      return 1
    fi
  done
  return 0
}

if [[ "${PURGE}" -eq 1 ]]; then
  if path_exists "${CONFIG_DEST}" && [[ ! -f "${CONFIG_DEST}" || -L "${CONFIG_DEST}" ]]; then
    die 4 "managed config is not a regular file; refusing purge: ${CONFIG_DEST}"
  fi
  for purge_path in "${TLS_DIR}" "${STATE_DIR}"; do
    if path_exists "${purge_path}" && [[ ! -d "${purge_path}" || -L "${purge_path}" ]]; then
      die 4 "managed purge path is not a regular directory; refusing purge: ${purge_path}"
    fi
  done
  if [[ "${ROLE}" == "agent" ]]; then
    if path_exists "${WORK_DIR}" && [[ ! -d "${WORK_DIR}" || -L "${WORK_DIR}" ]]; then
      die 4 "agent work path is not a regular directory; refusing purge: ${WORK_DIR}"
    fi
    directory_empty "${WORK_DIR}" || die 4 "agent work directory is not empty; refusing purge: ${WORK_DIR}"
    if [[ -d "${STATE_DIR}" ]]; then
      for state_item in "${STATE_DIR}"/* "${STATE_DIR}"/.[!.]* "${STATE_DIR}"/..?*; do
        if path_exists "${state_item}" && [[ "${state_item}" != "${WORK_DIR}" ]]; then
          die 4 "agent state directory contains unmanaged data; refusing purge: ${STATE_DIR}"
        fi
      done
    fi
  else
    directory_empty "${STATE_DIR}" || die 4 "coordinator state directory is not empty; refusing purge: ${STATE_DIR}"
  fi
  directory_empty "${TLS_DIR}" || die 4 "TLS directory is not empty; refusing purge: ${TLS_DIR}"

  if [[ "${STAGING}" -eq 0 ]]; then
    passwd_entry="$(getent passwd "${SERVICE_USER}" || true)"
    group_entry="$(getent group "${SERVICE_GROUP}" || true)"
    [[ -n "${passwd_entry}" && -n "${group_entry}" ]] || die 4 "managed service identity is incomplete: ${SERVICE_USER}"
    IFS=: read -r _ _ _ account_gid _ account_home account_shell <<<"${passwd_entry}"
    IFS=: read -r _ _ group_gid _ <<<"${group_entry}"
    [[ "${account_gid}" == "${group_gid}" && "${account_home}" == "/var/lib/planetary-mesh/${ROLE}" ]] || die 4 "managed service identity does not match expected group or home: ${SERVICE_USER}"
    [[ "${account_shell}" == "/usr/sbin/nologin" || "${account_shell}" == "/sbin/nologin" ]] || die 4 "managed service identity does not use a no-login shell: ${SERVICE_USER}"
    process_ids="$(ps -u "${SERVICE_USER}" -o pid= 2>/dev/null || true)"
    [[ -z "${process_ids//[[:space:]]/}" ]] || die 4 "service identity still owns running processes; refusing purge: ${SERVICE_USER}"
  fi
fi

if [[ "${STAGING}" -eq 0 ]]; then
  if systemctl is-active --quiet "${UNIT}"; then
    systemctl stop "${UNIT}" || die 6 "systemctl stop failed; no service files were removed; inspect journalctl -u ${UNIT}"
  fi
  if systemctl is-enabled --quiet "${UNIT}"; then
    systemctl disable "${UNIT}" || die 6 "systemctl disable failed; no service files were removed; inspect journalctl -u ${UNIT}"
  fi
fi

CLEANUP_FAILED=0
rm -f "${UNIT_DEST}" "${BINARY_DEST}" || CLEANUP_FAILED=1
if [[ "${ROLE}" == "coordinator" ]]; then
  rm -f "${PMCTL_DEST}" "${TEMPLATE_DIR}/text-stats.pmtemplate.json" "${TEMPLATE_DIR}/README.md" || CLEANUP_FAILED=1
  rmdir "${TEMPLATE_DIR}" >/dev/null 2>&1 || true
  rmdir "$(rooted /usr/share/planetary-mesh)" >/dev/null 2>&1 || true
else
  rm -f "${WORKLOAD_DEST}" || CLEANUP_FAILED=1
  rmdir "$(rooted /opt/planetary-mesh/workloads)" >/dev/null 2>&1 || true
fi

if [[ "${STAGING}" -eq 0 ]]; then
  systemctl daemon-reload || die 8 "cleanup incomplete; systemctl daemon-reload failed after asset removal"
fi

if [[ "${PURGE}" -eq 1 ]]; then
  rm -f "${CONFIG_DEST}" || CLEANUP_FAILED=1
  if [[ "${ROLE}" == "agent" ]] && path_exists "${WORK_DIR}"; then
    rmdir "${WORK_DIR}" >/dev/null 2>&1 || CLEANUP_FAILED=1
  fi
  if path_exists "${STATE_DIR}"; then
    rmdir "${STATE_DIR}" >/dev/null 2>&1 || CLEANUP_FAILED=1
  fi
  if path_exists "${TLS_DIR}"; then
    rmdir "${TLS_DIR}" >/dev/null 2>&1 || CLEANUP_FAILED=1
  fi
  rm -f "${MARKER}" || CLEANUP_FAILED=1
  if [[ "${STAGING}" -eq 0 ]]; then
    userdel "${SERVICE_USER}" || CLEANUP_FAILED=1
    groupdel "${SERVICE_GROUP}" || CLEANUP_FAILED=1
  fi
  rmdir "$(rooted /etc/planetary-mesh/tls)" >/dev/null 2>&1 || true
  rmdir "$(rooted /etc/planetary-mesh)" >/dev/null 2>&1 || true
  rmdir "$(rooted /var/lib/planetary-mesh)" >/dev/null 2>&1 || true
fi

rmdir "$(rooted /opt/planetary-mesh/bin)" >/dev/null 2>&1 || true
rmdir "$(rooted /opt/planetary-mesh)" >/dev/null 2>&1 || true

if [[ "${CLEANUP_FAILED}" -ne 0 ]]; then
  die 8 "cleanup incomplete; manual cleanup required for role ${ROLE}"
fi
if [[ "${PURGE}" -eq 1 ]]; then
  printf 'Uninstalled %s and purged empty managed configuration and identity state\n' "${UNIT}"
else
  printf 'Uninstalled %s; preserved config, TLS files, working data, and service identity\n' "${UNIT}"
fi
