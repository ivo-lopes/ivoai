# Read-only production inventory

IVOAI currently has two production installations, but this development
environment contains no documented hostnames or access path for both of them.
No production host was contacted while creating this compatibility foundation.

## Supported collection

Run this independently on each production host with the same account that
normally operates IVOAI:

```sh
ivoai doctor --inventory --json > ivoai-inventory.json
```

For a root-managed server installation:

```sh
sudo ivoai doctor --inventory --json > ivoai-inventory.json
```

The command is read-only and reports format/version, OS/architecture, client or
server mode, executable and XDG roots, config/state/ownership schemas, server
protocol, component version/path/ownership, connection booleans, a bounded
inventory health result, service state, backend type, install provenance, and
rollback availability. It deliberately does not run the full Doctor because
provider capability/quota probes may refresh local caches. It never reads
`secrets.json`, provider authentication
databases, prompts, responses, cookies, access tokens, refresh tokens, or raw
environment variables.

Before attaching an inventory to an issue, replace host-specific usernames and
paths if they identify a person or organization. Do not attach provider files or
server `.env` files. The JSON must be collected once from each production and
labelled `PROD-1` and `PROD-2`; hostnames and IPs need not enter the repository.

## Required evidence before a canary

Compare both inventories for:

- IVOAI version and release provenance;
- client/server role, OS, architecture and executable path;
- config/state/ownership schema versions;
- managed versus external component ownership and versions;
- Headroom/Ruflo legacy state;
- memory/context backend metadata and service health;
- inventory result, separately collected status/Doctor evidence, and rollback availability;
- installation method and any divergence from the canonical v0.5.0 fixtures.

The sanitized canonical baseline is under `tests/fixtures/v0.5.0`. It was derived
from the actual tag, not from either production, and must not be treated as proof
of the live hosts.
