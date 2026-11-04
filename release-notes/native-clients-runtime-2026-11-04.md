# Native client consolidation — 2026-11-04

## Claude Code 2.1.318

The managed installer pins the official v2.1.318 release for Linux amd64 and
ARM64, with the independently downloaded archive SHA-256 for each platform.
The amd64 executable reports `2.1.318 (Claude Code)` and its digest is retained
in the protocol proof. ARM64 archive integrity was checked; ARM64 execution was
not tested on the amd64 host.

The existing worker factory was exercised against that official amd64 client
inside a private bubblewrap network/HOME namespace and a synthetic loopback
Anthropic endpoint. All five mandatory cases passed. Positive controls prove
that native delegation really executes when exposed; the managed factory
advertises only `Read`, `Glob`, and `Grep`, blocks both `Agent` and `Task`, and
performs zero delegated child requests. This is an offline protocol proof,
**not provider inference, subscription certification, or economic evidence**.
The previous-version evidence remains unchanged.

Run:

```sh
scripts/test-claude-worker-tools.sh --archive /path/to/claude-linux-x64.tar.gz \
  --output /owned/private/proof-directory
```

The user explicitly has no Claude account and requested that real Claude
inference remain pending. No authentication was requested, no paid API fallback
was introduced, and no personal Claude settings were modified. Managed Claude
TUI interception, adaptive frontend admission and real inference certification
remain pending under IVOAI-194/197. Existing direct launching and bounded Claude
workers remain available under their existing contracts.

Evidence: [offline summary](evidence/native-clients-2026-11-04/claude-offline/summary.json).
