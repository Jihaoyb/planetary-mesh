#!/usr/bin/env bash
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<'USAGE'
Usage:
  install-linux.sh coordinator|agent --config /absolute/path [options]
  install-linux.sh coordinator|agent --reuse-config [options]

Options:
  --no-start                 Install without enabling, starting, or checking health.
  --health-url URL           Override the role's local health URL.
  --health-ca-file PATH      CA file for an HTTPS health check.
  --health-cert-file PATH    Client certificate for an HTTPS health check.
  --health-key-file PATH     Client key for an HTTPS health check.
  --root PATH                Non-/ staging root; requires --no-start.
USAGE
}

die() {
  local code="$1"
  shift
  printf 'ERROR[%s] %s\n' "${code}" "$*" >&2
  exit "${code}"
}

path_exists() {
  [[ -e "$1" || -L "$1" ]]
}

is_absolute() {
  [[ "$1" == /* ]]
}

trim_value() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

validate_config() {
  local path="$1"
  local line line_no=0 value trimmed

  while IFS= read -r line || [[ -n "${line}" ]]; do
    line_no=$((line_no + 1))
    line="${line%$'\r'}"
    if [[ "${line}" =~ ^[[:space:]]*$ || "${line}" =~ ^[[:space:]]*# ]]; then
      continue
    fi
    if [[ ! "${line}" =~ ^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*[[:space:]]*= ]]; then
      die 5 "invalid env-style config at ${path}:${line_no}; expected KEY=value"
    fi

    value="${line#*=}"
    trimmed="$(trim_value "${value}")"
    if [[ "${trimmed}" == \"* && "${trimmed}" != *\" ]]; then
      die 5 "invalid env-style config at ${path}:${line_no}; unterminated double-quoted value"
    fi
    if [[ "${trimmed}" == \'* && "${trimmed}" != *\' ]]; then
      die 5 "invalid env-style config at ${path}:${line_no}; unterminated single-quoted value"
    fi
  done <"${path}"
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

CONFIG_SOURCE=""
REUSE_CONFIG=0
NO_START=0
DEST_ROOT="/"
HEALTH_URL=""
HEALTH_CA_FILE=""
HEALTH_CERT_FILE=""
HEALTH_KEY_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      [[ $# -ge 2 ]] || die 2 "--config requires a path"
      CONFIG_SOURCE="$2"
      shift 2
      ;;
    --reuse-config)
      REUSE_CONFIG=1
      shift
      ;;
    --no-start)
      NO_START=1
      shift
      ;;
    --health-url)
      [[ $# -ge 2 ]] || die 2 "--health-url requires a URL"
      HEALTH_URL="$2"
      shift 2
      ;;
    --health-ca-file)
      [[ $# -ge 2 ]] || die 2 "--health-ca-file requires a path"
      HEALTH_CA_FILE="$2"
      shift 2
      ;;
    --health-cert-file)
      [[ $# -ge 2 ]] || die 2 "--health-cert-file requires a path"
      HEALTH_CERT_FILE="$2"
      shift 2
      ;;
    --health-key-file)
      [[ $# -ge 2 ]] || die 2 "--health-key-file requires a path"
      HEALTH_KEY_FILE="$2"
      shift 2
      ;;
    --root)
      [[ $# -ge 2 ]] || die 2 "--root requires a path"
      DEST_ROOT="$2"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *) die 2 "unknown option: $1" ;;
  esac
done

if [[ -n "${CONFIG_SOURCE}" && "${REUSE_CONFIG}" -eq 1 ]]; then
  die 2 "--config and --reuse-config are mutually exclusive"
fi
if [[ -z "${CONFIG_SOURCE}" && "${REUSE_CONFIG}" -eq 0 ]]; then
  die 2 "exactly one of --config or --reuse-config is required"
fi
if [[ -n "${CONFIG_SOURCE}" ]] && ! is_absolute "${CONFIG_SOURCE}"; then
  die 2 "--config must be an absolute path"
fi

STAGING=0
if [[ "${DEST_ROOT}" != "/" ]]; then
  is_absolute "${DEST_ROOT}" || die 2 "--root must be an absolute path"
  [[ "${NO_START}" -eq 1 ]] || die 2 "--root requires --no-start"
  [[ -d "${DEST_ROOT}" && ! -L "${DEST_ROOT}" ]] || die 3 "staging root is not a regular directory: ${DEST_ROOT}"
  DEST_ROOT="$(cd "${DEST_ROOT}" && pwd -P)"
  [[ "${DEST_ROOT}" != "/" ]] || die 2 "--root must not resolve to /"
  STAGING=1
elif [[ "${NO_START}" -eq 1 && -n "${HEALTH_URL}${HEALTH_CA_FILE}${HEALTH_CERT_FILE}${HEALTH_KEY_FILE}" ]]; then
  die 2 "health-check options cannot be used with --no-start"
fi

TEST_FAIL_AFTER="${PLANETARY_MESH_INSTALL_FAIL_AFTER:-}"
if [[ -n "${TEST_FAIL_AFTER}" ]]; then
  [[ "${STAGING}" -eq 1 ]] || die 2 "PLANETARY_MESH_INSTALL_FAIL_AFTER is allowed only with a non-/ --root"
  case "${TEST_FAIL_AFTER}" in
    account | config | binary | unit) ;;
    *) die 2 "unsupported PLANETARY_MESH_INSTALL_FAIL_AFTER stage" ;;
  esac
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
BINARY_SOURCE="${RELEASE_ROOT}/bin/${ROLE}"
UNIT_SOURCE="${SCRIPT_DIR}/systemd/${UNIT}"
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

if [[ -z "${HEALTH_URL}" ]]; then
  if [[ "${ROLE}" == "coordinator" ]]; then
    HEALTH_URL="http://127.0.0.1:8080/healthz"
  else
    HEALTH_URL="http://127.0.0.1:8081/healthz"
  fi
fi

for command_name in install mv rm rmdir; do
  command -v "${command_name}" >/dev/null 2>&1 || die 3 "missing required command: ${command_name}"
done
if [[ "${STAGING}" -eq 0 ]]; then
  [[ "$(uname -s)" == "Linux" ]] || die 3 "Linux with systemd is required"
  [[ "$(id -u)" == "0" ]] || die 3 "root privileges are required; rerun with sudo"
  for command_name in systemctl useradd userdel groupadd groupdel getent; do
    command -v "${command_name}" >/dev/null 2>&1 || die 3 "missing required command: ${command_name}"
  done
  if [[ "${NO_START}" -eq 0 ]]; then
    command -v curl >/dev/null 2>&1 || die 3 "missing required command: curl"
  fi
fi

for source_path in "${BINARY_SOURCE}" "${UNIT_SOURCE}"; do
  [[ -f "${source_path}" && ! -L "${source_path}" ]] || die 3 "missing release binary: ${source_path}; run this installer from an extracted Linux release archive"
done
if [[ "${ROLE}" == "coordinator" ]]; then
  for source_path in "${RELEASE_ROOT}/bin/pmctl" "${RELEASE_ROOT}/templates/text-stats.pmtemplate.json" "${RELEASE_ROOT}/templates/README.md"; do
    [[ -f "${source_path}" && ! -L "${source_path}" ]] || die 3 "missing release asset: ${source_path}; run this installer from an extracted Linux release archive"
  done
else
  [[ -f "${RELEASE_ROOT}/workloads/text-stats" && ! -L "${RELEASE_ROOT}/workloads/text-stats" ]] || die 3 "missing release asset: ${RELEASE_ROOT}/workloads/text-stats; run this installer from an extracted Linux release archive"
fi

if [[ "${REUSE_CONFIG}" -eq 1 ]]; then
  [[ -f "${MARKER}" && ! -L "${MARKER}" ]] || die 4 "managed marker is missing or unsafe: ${MARKER}; refusing to reuse unowned configuration"
  [[ -f "${CONFIG_DEST}" && ! -L "${CONFIG_DEST}" ]] || die 3 "managed config is missing: ${CONFIG_DEST}"
  CONFIG_SOURCE="${CONFIG_DEST}"
else
  [[ -f "${CONFIG_SOURCE}" && ! -L "${CONFIG_SOURCE}" && -r "${CONFIG_SOURCE}" ]] || die 3 "config must be a readable regular file: ${CONFIG_SOURCE}"
  if path_exists "${CONFIG_DEST}"; then
    die 4 "managed config already exists: ${CONFIG_DEST}; use --reuse-config to preserve it"
  fi
fi
validate_config "${CONFIG_SOURCE}"

for existing_path in "${BINARY_DEST}" "${UNIT_DEST}"; do
  if path_exists "${existing_path}"; then
    die 4 "${ROLE} is already installed; automatic upgrade is not supported; uninstall it before reinstalling"
  fi
done
if [[ "${ROLE}" == "coordinator" ]]; then
  if path_exists "${PMCTL_DEST}" || path_exists "${TEMPLATE_DIR}"; then
    die 4 "coordinator is already installed; automatic upgrade is not supported; uninstall it before reinstalling"
  fi
elif path_exists "${WORKLOAD_DEST}"; then
  die 4 "agent is already installed; automatic upgrade is not supported; uninstall it before reinstalling"
fi

TLS_OPTION_COUNT=0
for tls_path in "${HEALTH_CA_FILE}" "${HEALTH_CERT_FILE}" "${HEALTH_KEY_FILE}"; do
  [[ -n "${tls_path}" ]] && TLS_OPTION_COUNT=$((TLS_OPTION_COUNT + 1))
done
if [[ "${TLS_OPTION_COUNT}" -ne 0 && "${TLS_OPTION_COUNT}" -ne 3 ]]; then
  die 2 "--health-ca-file, --health-cert-file, and --health-key-file must be provided together"
fi
if [[ "${TLS_OPTION_COUNT}" -eq 3 ]]; then
  [[ "${HEALTH_URL}" == https://* ]] || die 2 "health TLS files require an https:// health URL"
  for tls_path in "${HEALTH_CA_FILE}" "${HEALTH_CERT_FILE}" "${HEALTH_KEY_FILE}"; do
    is_absolute "${tls_path}" || die 2 "health TLS file paths must be absolute"
    [[ -f "${tls_path}" && ! -L "${tls_path}" && -r "${tls_path}" ]] || die 3 "health TLS file must be a readable regular file: ${tls_path}"
  done
elif [[ "${HEALTH_URL}" == https://* && "${NO_START}" -eq 0 ]]; then
  die 2 "https:// health checks require --health-ca-file, --health-cert-file, and --health-key-file"
fi

NOLOGIN_SHELL=""
ACCOUNT_EXISTS=0
if [[ "${STAGING}" -eq 0 ]]; then
  if [[ -x /usr/sbin/nologin ]]; then
    NOLOGIN_SHELL=/usr/sbin/nologin
  elif [[ -x /sbin/nologin ]]; then
    NOLOGIN_SHELL=/sbin/nologin
  else
    die 3 "missing no-login shell: expected /usr/sbin/nologin or /sbin/nologin"
  fi

  if [[ -f "${MARKER}" && ! -L "${MARKER}" ]]; then
    passwd_entry="$(getent passwd "${SERVICE_USER}" || true)"
    group_entry="$(getent group "${SERVICE_GROUP}" || true)"
    [[ -n "${passwd_entry}" && -n "${group_entry}" ]] || die 4 "managed service identity is incomplete: ${SERVICE_USER}"
    IFS=: read -r _ _ _ account_gid _ account_home account_shell <<<"${passwd_entry}"
    IFS=: read -r _ _ group_gid _ <<<"${group_entry}"
    [[ "${account_gid}" == "${group_gid}" && "${account_home}" == "/var/lib/planetary-mesh/${ROLE}" ]] || die 4 "managed service identity does not match expected group or home: ${SERVICE_USER}"
    [[ "${account_shell}" == "/usr/sbin/nologin" || "${account_shell}" == "/sbin/nologin" ]] || die 4 "managed service identity does not use a no-login shell: ${SERVICE_USER}"
    ACCOUNT_EXISTS=1
  else
    if getent passwd "${SERVICE_USER}" >/dev/null 2>&1 || getent group "${SERVICE_GROUP}" >/dev/null 2>&1; then
      die 4 "service identity already exists without a managed marker: ${SERVICE_USER}"
    fi
  fi
fi

CREATED_DIRS=()
CREATED_GROUP=0
CREATED_USER=0
CREATED_MARKER=0
CREATED_CONFIG=0
CREATED_BINARY=0
CREATED_UNIT=0
CREATED_PMCTL=0
CREATED_WORKLOAD=0
CREATED_TEMPLATE_JSON=0
CREATED_TEMPLATE_README=0
SYSTEMD_TOUCHED=0

ensure_dir() {
  local path="$1"
  local mode="$2"
  local owner="$3"
  local group="$4"
  if path_exists "${path}"; then
    [[ -d "${path}" && ! -L "${path}" ]] || return 1
    return 0
  fi
  if [[ "${STAGING}" -eq 1 ]]; then
    install -d -m "${mode}" "${path}" || return 1
  else
    install -d -m "${mode}" -o "${owner}" -g "${group}" "${path}" || return 1
  fi
  CREATED_DIRS[${#CREATED_DIRS[@]}]="${path}"
}

install_file_atomic() {
  local source="$1"
  local destination="$2"
  local mode="$3"
  local owner="$4"
  local group="$5"
  local temporary="${destination}.tmp.$$"

  rm -f "${temporary}" >/dev/null 2>&1 || return 1
  if [[ "${STAGING}" -eq 1 ]]; then
    install -m "${mode}" "${source}" "${temporary}" || return 1
  else
    install -m "${mode}" -o "${owner}" -g "${group}" "${source}" "${temporary}" || return 1
  fi
  mv -n "${temporary}" "${destination}" || return 1
  if path_exists "${temporary}"; then
    rm -f "${temporary}" >/dev/null 2>&1 || true
    return 1
  fi
}

rollback() {
  local failed=0 index

  if [[ "${STAGING}" -eq 0 && "${SYSTEMD_TOUCHED}" -eq 1 ]]; then
    systemctl disable --now "${UNIT}" >/dev/null 2>&1 || true
  fi
  [[ "${CREATED_TEMPLATE_README}" -eq 0 ]] || rm -f "${TEMPLATE_DIR}/README.md" || failed=1
  [[ "${CREATED_TEMPLATE_JSON}" -eq 0 ]] || rm -f "${TEMPLATE_DIR}/text-stats.pmtemplate.json" || failed=1
  [[ "${CREATED_WORKLOAD}" -eq 0 ]] || rm -f "${WORKLOAD_DEST}" || failed=1
  [[ "${CREATED_PMCTL}" -eq 0 ]] || rm -f "${PMCTL_DEST}" || failed=1
  [[ "${CREATED_UNIT}" -eq 0 ]] || rm -f "${UNIT_DEST}" || failed=1
  [[ "${CREATED_BINARY}" -eq 0 ]] || rm -f "${BINARY_DEST}" || failed=1
  [[ "${CREATED_CONFIG}" -eq 0 ]] || rm -f "${CONFIG_DEST}" || failed=1
  [[ "${CREATED_MARKER}" -eq 0 ]] || rm -f "${MARKER}" || failed=1

  for ((index = ${#CREATED_DIRS[@]} - 1; index >= 0; index--)); do
    rmdir "${CREATED_DIRS[index]}" >/dev/null 2>&1 || true
  done
  if [[ "${STAGING}" -eq 0 ]]; then
    [[ "${CREATED_USER}" -eq 0 ]] || userdel "${SERVICE_USER}" >/dev/null 2>&1 || failed=1
    [[ "${CREATED_GROUP}" -eq 0 ]] || groupdel "${SERVICE_GROUP}" >/dev/null 2>&1 || failed=1
    [[ "${SYSTEMD_TOUCHED}" -eq 0 ]] || systemctl daemon-reload >/dev/null 2>&1 || failed=1
  fi
  return "${failed}"
}

transaction_error() {
  local code="$1"
  shift
  local message="$*"
  if rollback; then
    die "${code}" "${message}"
  fi
  die 8 "rollback incomplete; manual cleanup required for: ${BINARY_DEST} ${UNIT_DEST} ${CONFIG_DEST} ${MARKER}"
}

maybe_fail_after() {
  local stage="$1"
  if [[ "${TEST_FAIL_AFTER}" == "${stage}" ]]; then
    transaction_error 6 "injected install failure after ${stage}; rollback completed"
  fi
}

if [[ "${STAGING}" -eq 0 && "${ACCOUNT_EXISTS}" -eq 0 ]]; then
  if ! groupadd --system "${SERVICE_GROUP}"; then
    transaction_error 6 "create service group failed; rollback completed"
  fi
  CREATED_GROUP=1
  if ! useradd --system --gid "${SERVICE_GROUP}" --home-dir "/var/lib/planetary-mesh/${ROLE}" --no-create-home --shell "${NOLOGIN_SHELL}" "${SERVICE_USER}"; then
    transaction_error 6 "create service user failed; rollback completed"
  fi
  CREATED_USER=1
fi

ensure_dir "$(rooted /opt/planetary-mesh)" 0755 root root || transaction_error 6 "create installation directory failed; rollback completed"
ensure_dir "$(rooted /opt/planetary-mesh/bin)" 0755 root root || transaction_error 6 "create binary directory failed; rollback completed"
ensure_dir "$(rooted /etc/planetary-mesh)" 0755 root root || transaction_error 6 "create config directory failed; rollback completed"
ensure_dir "$(rooted /etc/planetary-mesh/tls)" 0755 root root || transaction_error 6 "create TLS directory failed; rollback completed"
ensure_dir "${TLS_DIR}" 0750 root "${SERVICE_GROUP}" || transaction_error 6 "create role TLS directory failed; rollback completed"
ensure_dir "$(rooted /var/lib/planetary-mesh)" 0755 root root || transaction_error 6 "create state directory failed; rollback completed"
ensure_dir "${STATE_DIR}" 0750 "${SERVICE_USER}" "${SERVICE_GROUP}" || transaction_error 6 "create role state directory failed; rollback completed"
ensure_dir "$(rooted /etc/systemd/system)" 0755 root root || transaction_error 6 "create systemd unit directory failed; rollback completed"
if [[ "${ROLE}" == "agent" ]]; then
  ensure_dir "${WORK_DIR}" 0750 "${SERVICE_USER}" "${SERVICE_GROUP}" || transaction_error 6 "create agent work directory failed; rollback completed"
fi
maybe_fail_after account

if [[ "${REUSE_CONFIG}" -eq 0 ]]; then
  install_file_atomic "${CONFIG_SOURCE}" "${CONFIG_DEST}" 0640 root "${SERVICE_GROUP}" || transaction_error 6 "install managed config failed; rollback completed"
  CREATED_CONFIG=1
fi
maybe_fail_after config

install_file_atomic "${BINARY_SOURCE}" "${BINARY_DEST}" 0755 root root || transaction_error 6 "install role binary failed; rollback completed"
CREATED_BINARY=1
maybe_fail_after binary

install_file_atomic "${UNIT_SOURCE}" "${UNIT_DEST}" 0644 root root || transaction_error 6 "install systemd unit failed; rollback completed"
CREATED_UNIT=1
maybe_fail_after unit

if [[ "${ROLE}" == "coordinator" ]]; then
  ensure_dir "$(rooted /usr/local/bin)" 0755 root root || transaction_error 6 "create pmctl directory failed; rollback completed"
  ensure_dir "$(rooted /usr/share/planetary-mesh)" 0755 root root || transaction_error 6 "create shared-data directory failed; rollback completed"
  ensure_dir "${TEMPLATE_DIR}" 0755 root root || transaction_error 6 "create template directory failed; rollback completed"
  install_file_atomic "${RELEASE_ROOT}/bin/pmctl" "${PMCTL_DEST}" 0755 root root || transaction_error 6 "install pmctl failed; rollback completed"
  CREATED_PMCTL=1
  install_file_atomic "${RELEASE_ROOT}/templates/text-stats.pmtemplate.json" "${TEMPLATE_DIR}/text-stats.pmtemplate.json" 0644 root root || transaction_error 6 "install template failed; rollback completed"
  CREATED_TEMPLATE_JSON=1
  install_file_atomic "${RELEASE_ROOT}/templates/README.md" "${TEMPLATE_DIR}/README.md" 0644 root root || transaction_error 6 "install template documentation failed; rollback completed"
  CREATED_TEMPLATE_README=1
else
  ensure_dir "$(rooted /opt/planetary-mesh/workloads)" 0755 root root || transaction_error 6 "create workload directory failed; rollback completed"
  install_file_atomic "${RELEASE_ROOT}/workloads/text-stats" "${WORKLOAD_DEST}" 0755 root root || transaction_error 6 "install text-stats failed; rollback completed"
  CREATED_WORKLOAD=1
fi

if ! path_exists "${MARKER}"; then
  marker_source="${MARKER}.source.$$"
  if ! printf 'role=%s\n' "${ROLE}" >"${marker_source}"; then
    transaction_error 6 "create managed marker failed; rollback completed"
  fi
  if ! install_file_atomic "${marker_source}" "${MARKER}" 0600 root root; then
    rm -f "${marker_source}" >/dev/null 2>&1 || true
    transaction_error 6 "install managed marker failed; rollback completed"
  fi
  rm -f "${marker_source}" >/dev/null 2>&1 || true
  CREATED_MARKER=1
fi

if [[ "${STAGING}" -eq 1 ]]; then
  printf 'Installed %s under staging root %s without enabling or starting it\n' "${UNIT}" "${DEST_ROOT}"
  exit 0
fi

SYSTEMD_TOUCHED=1
if ! systemctl daemon-reload; then
  transaction_error 6 "systemctl daemon-reload failed; rollback completed; inspect journalctl -u ${UNIT}"
fi
if [[ "${NO_START}" -eq 1 ]]; then
  printf 'Installed %s without enabling or starting it\n' "${UNIT}"
  exit 0
fi
if ! systemctl enable --now "${UNIT}"; then
  transaction_error 6 "systemctl enable --now failed; rollback completed; inspect journalctl -u ${UNIT}"
fi

CURL_ARGS=(-fsS --max-time 2)
if [[ "${TLS_OPTION_COUNT}" -eq 3 ]]; then
  CURL_ARGS+=(--cacert "${HEALTH_CA_FILE}" --cert "${HEALTH_CERT_FILE}" --key "${HEALTH_KEY_FILE}")
fi
HEALTHY=0
for _ in {1..40}; do
  if systemctl is-active --quiet "${UNIT}" && curl "${CURL_ARGS[@]}" "${HEALTH_URL}" >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  sleep 0.5
done
if [[ "${HEALTHY}" -ne 1 ]]; then
  transaction_error 7 "health check failed: ${HEALTH_URL}; rollback completed; inspect journalctl -u ${UNIT}"
fi

printf 'Installed %s and verified %s\n' "${UNIT}" "${HEALTH_URL}"
