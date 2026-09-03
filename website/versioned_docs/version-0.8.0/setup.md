# Setup

## Client setup

`ivoai setup` provisions managed components transactionally and preserves external
executor installations and their authentication.

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
