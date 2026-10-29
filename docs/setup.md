# Setup

## Client setup

`ivoai setup` provisions managed components transactionally and preserves external
executor installations and their authentication.

The local v0.15.0 candidate omits OpenCode from ordinary client setup and updater
candidate setup. To explicitly provision the retained compatibility component,
use `ivoai setup --mode client --legacy-opencode`; server setup refuses this flag.
Ordinary setup preserves existing OpenCode ownership, binaries and history; it
does not uninstall, repair or adopt that component. See [releases](releases.md)
for candidate status and native migration limits.

## Managed native configuration scope

For managed Codex, start IVOAI from the required workspace using shell `cd`:

```sh
cd /path/to/project
ivoai codex
```

Do not forward native `-C`/`--cd` or `--ignore-user-config`: they are rejected with
`CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED`. Existing sessions keep their recorded
workspace; start a new session when selecting another initial directory. The host
shares one captured directory/environment/config scope between MCP inventory and
execution, preserving Core grants, sandbox and approvals. No personal config or
credential file needs editing. This snapshot does not freeze changing settings or
cloud policy, and it does not certify general client parity. See [releases](releases.md).

## Debian 12 and LXC server setup

`sudo ivoai setup --mode server` performs a preflight before writing server state:
OS, architecture, container/LXC status, privileges, systemd, Docker CLI and daemon,
Engine version, and Compose v2.

On supported Debian 12, IVOAI can install a missing Docker Engine from Docker's
official APT repository and upgrade an old Engine already owned by that repository.
Unknown/non-official installations remain operator-owned. It never uses a remote
shell installer. If Docker cannot start inside LXC, enable nesting and the required
container features on the Proxmox host, then rerun setup. Host configuration cannot
be changed safely from inside the guest.

An interrupted setup is repaired idempotently by rerunning the same command. Before
completion, server diagnostics report `SERVER_SETUP=INCOMPLETE` and the actual
prerequisite instead of treating missing backend `.env` files as the root cause.
