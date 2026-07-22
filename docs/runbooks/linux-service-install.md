# Linux Managed Service Installation

This runbook installs the coordinator and agent as independent systemd services
from an extracted Planetary Mesh Linux release archive. It is a pre-release
Linux/systemd workflow for trusted hosts. It is not a signed installer,
package-manager distribution, GitHub Release, or production certification.

The installer copies release assets into fixed host paths, creates stable
unprivileged service identities, enables and starts the selected service by
default, and verifies its health endpoint. Coordinator and agent may be
installed on different machines or together on one machine.

## Prerequisites

- Linux with systemd 240 or newer (`Type=exec` requires systemd 240)
- a matching extracted `linux/amd64` or `linux/arm64` release archive
- root privileges for a real installation
- `systemctl`, `useradd`, `groupadd`, `getent`, and `install`
- `curl` unless installing with `--no-start`
- an absolute path to a coordinator or agent env-style config file

Linux archives contain:

```text
install/
  install-linux.sh
  uninstall-linux.sh
  systemd/
    planetary-mesh-coordinator.service
    planetary-mesh-agent.service
```

macOS and Windows archives remain manual-start environments and do not contain
these service assets.

## Prepare configuration

Start from the tracked examples under `config/`, store the working file outside
the extracted archive, and pass its absolute path. The installer validates the
blank/comment/`KEY=value` structure without sourcing or printing values, then
copies it into `/etc/planetary-mesh/`. The daemon remains authoritative for
semantic validation.

Managed configuration paths and modes are:

- `/etc/planetary-mesh/coordinator.env`, `root:planetary-mesh-coordinator`,
  `0640`
- `/etc/planetary-mesh/agent.env`, `root:planetary-mesh-agent`, `0640`

The source file is never modified or removed. Existing managed configuration is
never overwritten; use `--reuse-config` only when deliberately reinstalling a
role whose managed configuration was preserved by uninstall.

## Install the coordinator

From the extracted Linux archive:

```bash
sudo ./install/install-linux.sh coordinator \
  --config /absolute/path/coordinator.env
```

This installs:

- `/opt/planetary-mesh/bin/coordinator`
- `/usr/local/bin/pmctl`
- `/usr/share/planetary-mesh/templates/`
- `/etc/systemd/system/planetary-mesh-coordinator.service`
- the `planetary-mesh-coordinator` system user and group
- `/var/lib/planetary-mesh/coordinator/`

The default health check is `http://127.0.0.1:8080/healthz`. If the configured
listen address or TLS mode differs, pass the matching `--health-url` and, for
mTLS, all three health credential flags described below.

## Install the agent

```bash
sudo ./install/install-linux.sh agent \
  --config /absolute/path/agent.env
```

This installs:

- `/opt/planetary-mesh/bin/agent`
- `/opt/planetary-mesh/workloads/text-stats`
- `/etc/systemd/system/planetary-mesh-agent.service`
- the `planetary-mesh-agent` system user and group
- `/var/lib/planetary-mesh/agent/work/`

The default health check is `http://127.0.0.1:8081/healthz`.

Installing `text-stats` does not trust or enable it. To use it, explicitly map
its managed absolute path in `AGENT_COMMAND_ALLOWLIST`, for example:

```text
AGENT_COMMAND_ALLOWLIST=text-stats=/opt/planetary-mesh/workloads/text-stats
```

Other external executables must be absolute, executable by the agent account,
and preferably root-owned and not writable by that account. Inputs must be
readable and traversable by the service account. Write outputs only to an
explicitly writable location such as
`/var/lib/planetary-mesh/agent/work/`.

## TLS and Postgres access

The installer creates empty role-specific TLS directories:

- `/etc/planetary-mesh/tls/coordinator/`
- `/etc/planetary-mesh/tls/agent/`

It does not generate, copy, rotate, log, or delete certificates. Place files
manually, make each directory in the path traversable by the relevant service
account, and make private keys readable by the role group without making them
world-readable. All configured certificate and key paths must be absolute.

For an HTTPS or mTLS startup check, provide an HTTPS URL and the complete
all-or-none credential set:

```bash
sudo ./install/install-linux.sh agent \
  --config /absolute/path/agent.env \
  --health-url https://127.0.0.1:8081/healthz \
  --health-ca-file /absolute/path/ca.crt \
  --health-cert-file /absolute/path/health-client.crt \
  --health-key-file /absolute/path/health-client.key
```

These health credentials are used only by `curl`; they are not copied,
persisted, or printed.

For a Postgres-backed coordinator, set `COORDINATOR_DATABASE_URL` in the source
config before installation. Network and database access must work for the
service account. Optional Postgres behavior, nodes/jobs-only storage, startup
reconciliation, and schema readiness version `2` are unchanged.

## Install without starting

Use `--no-start` to copy assets and reload systemd without enabling, starting,
or health-checking the service:

```bash
sudo ./install/install-linux.sh coordinator \
  --config /absolute/path/coordinator.env \
  --no-start
```

This performs structural config validation only. A semantic config error will
appear when the operator later starts the service.

`--root /absolute/staging/root` is a non-root test/staging seam. It is accepted
only with `--no-start`, prefixes filesystem destinations, and never creates
accounts or mutates systemd. It is not a host installer mode.

## Operate the services

There is no reload operation. Restart after changing configuration.

```bash
sudo systemctl start planetary-mesh-coordinator.service
sudo systemctl stop planetary-mesh-coordinator.service
sudo systemctl restart planetary-mesh-coordinator.service
sudo systemctl status planetary-mesh-coordinator.service

sudo systemctl start planetary-mesh-agent.service
sudo systemctl stop planetary-mesh-agent.service
sudo systemctl restart planetary-mesh-agent.service
sudo systemctl status planetary-mesh-agent.service
```

Logs are JSON on stdout/stderr and are retained by journald according to the
host's policy:

```bash
sudo journalctl -u planetary-mesh-coordinator.service -n 100 --no-pager
sudo journalctl -u planetary-mesh-agent.service -f
```

The unit uses `Restart=on-failure`, a five-second restart delay, a 30-second
start timeout, and a 15-second stop timeout. The latter exceeds the daemons'
existing ten-second graceful shutdown window.

## Shutdown and active-job consequences

Stopping or restarting a service does not add cancellation or transparent job
continuation:

- Stopping an agent sends `SIGTERM`. A command that finishes during graceful
  shutdown may return and report a result; a longer command can be terminated
  with the service cgroup.
- Existing coordinator transport timeout, retry, and reassignment semantics
  remain authoritative. Side-effecting work may already have run.
- Agent restart loses the bounded in-memory terminal-result cache and does not
  resume an interrupted command.
- In-memory coordinator restart loses all coordinator state.
- Postgres-backed coordinator restart retains the existing bounded
  reconciliation grace behavior for persisted `RUNNING` jobs.

Inspect active jobs and acknowledge those consequences before stop, restart,
uninstall, upgrade, or rollback.

## Reinstall with preserved configuration

Automatic in-place upgrade is intentionally unsupported. A normal install over
an existing role refuses with exit code `4`.

After a default uninstall has preserved the managed config and role identity,
install from the new extracted archive with:

```bash
sudo ./install/install-linux.sh coordinator --reuse-config
sudo ./install/install-linux.sh agent --reuse-config
```

For a manual upgrade or rollback:

1. Inspect active jobs and retain the old extracted archive.
2. Back up configuration outside the managed tree.
3. Run default uninstall for the role.
4. Extract the desired archive and install with `--reuse-config`.
5. Verify service health, logs, node visibility, and a safe validation job.

If the new install fails health verification, the invocation removes only the
assets it created. It does not automatically restore the previous binary.

## Uninstall and purge

Default uninstall stops and disables the service, then removes the unit and
role release assets. It preserves config, TLS files, working data, marker,
accounts/groups, and journal history:

```bash
sudo ./install/uninstall-linux.sh coordinator
sudo ./install/uninstall-linux.sh agent
```

Repeated default uninstall is idempotent. If stopping the service fails, the
uninstaller refuses to delete live service files.

Purge is deliberately conservative:

```bash
sudo ./install/uninstall-linux.sh coordinator --purge
sudo ./install/uninstall-linux.sh agent --purge
```

It succeeds only when the managed marker and account metadata match, no role
process remains, and the role's TLS/work/state directories are empty. It never
recursively removes unknown data, arbitrary allowlisted executables, input or
output paths, original source config, or journal history.

## Failure codes

| Code | Meaning |
|---|---|
| `2` | usage or argument error |
| `3` | missing prerequisite, input, or privilege |
| `4` | conflict or safety refusal |
| `5` | malformed env-style configuration |
| `6` | filesystem or systemd transaction failure |
| `7` | startup health verification failure |
| `8` | incomplete rollback or cleanup requiring manual action |

Errors identify paths or line numbers where useful, but never print config
values, database URLs, certificate content, private keys, or curl credentials.
For startup failures, inspect the named unit with `systemctl status` and
`journalctl` before retrying.

## Validation evidence

The repository validates archive layout, script syntax, temporary-root role
installation, preservation/reuse/purge behavior, rollback injection, secret
redaction, and existing installed-binary behavior with:

```bash
GOCACHE=/private/tmp/planetary-mesh-gocache-linux-service \
  ./examples/linux_service_install_smoke.sh
GOCACHE=/private/tmp/planetary-mesh-gocache-release \
  ./examples/release_smoke.sh
```

The service smoke runs without touching real `/etc`, `/opt`, `/usr`, users, or
PID 1. Static unit verification runs when `systemd-analyze` is available.

Real install/start/status/log/restart/stop/uninstall validation requires a
disposable Linux systemd host or VM. It was **Not run** for Milestone 27 because
no suitable environment was available; do not treat temporary-root or static
validation as physical lifecycle evidence.

These units reduce daemon privileges operationally. They do not sandbox
allowlisted workloads or make arbitrary or untrusted execution safe. Planetary
Mesh remains trusted-host, allowlisted, direct execution without a shell.
