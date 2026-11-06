# Exact Memory rollback correction — 6 November 2026

An independent review found that the initial dependency rollback called public
Core Restore directly on the current Memory tree. Restore intentionally overlays
files. A forward runtime's new files or SQLite sidecars could therefore survive
and contaminate the restored old database. The earlier positive migration proof
did not assert the absence of forward-only files; its original artifacts remain
unchanged.

Dependency rollback now restores the authenticated backup into a fresh private
Layout on the Memory filesystem before touching active data. Core archive/path/
entry validation runs there. Config, Context and Corpus retain the existing
Restore overlay behavior. Memory is replaced by renaming the validated fresh
tree, while the complete current tree is preserved in a private sibling directory
`post-upgrade-memory-*/memory`. It is never overlaid or deleted. Failure to activate
the replacement attempts to reestablish the original active name; failure leaves
the preserved quarantine and services stopped. This recovery does not rerun the
requested mutation or automatically replay an upgrade.

The kernel mutation lock, stopped-service requirement, private receipt, archive
hash, Qdrant snapshot hash and fixed operational-asset validation still precede
restoration. Public `server.Restore` and its executable-configuration whitelist
are unchanged. Qdrant rollback continues to preserve its forward index separately.

## Evidence

[Real official Docker result](evidence/server-dependency-upgrade-20261106/exact-memory-rollback/real-docker.json)
and [execution log](evidence/server-dependency-upgrade-20261106/exact-memory-rollback/real-docker.log)
passed in **122.10 seconds**: ai-memory **2.6.3 → 3.4.2 → 2.6.3** and Qdrant
**1.19.2 → 1.20.0 → 1.19.2**. After the new runtimes stopped, the fixture created
new WAL/SHM-named sentinel files and a nested extra file in the real Memory tree.
These are deliberate filesystem sentinels, not a claim of SQLite-generated WAL
content. Before restarting old runtimes, it compared every restored directory/
file name and file SHA-256 with a separate fresh Core restoration of the backup,
and verified every sentinel's bytes in quarantine. Real authenticated exact
memory/vector reads and unauthenticated refusal passed again after rollback.
The numeric vector fixture is a storage-contract test, not embedding accuracy.

[Focused race results](evidence/server-dependency-upgrade-20261106/exact-memory-rollback/focused-race.log)
cover server and servercmd. Tests also verify actual `memory.db-wal`,
`memory.db-shm` and nested extras disappear from the active restored tree, current
bytes survive in quarantine, a failed activation preserves the original active
tree, and a malformed archive with a corresponding receipt hash changes no active
tree. `go vet` passed for both packages. The
[check and hash index](evidence/server-dependency-upgrade-20261106/exact-memory-rollback/checks.json)
records commands and source/proof hashes.

The Docker fixture uses the explicit disposable socket and mapped UID/GID 1000.
It does not access production, certify production systemd, or claim enforced
cgroup limits: that private daemon reports cgroup `none`. The probes check known
read/status/query/catalog and vector contracts; they are **not a universal tool
inventory, full provider parity, or comprehensive compatibility certification**.
No new native ARM, no-AVX or economic result is claimed by this correction.
