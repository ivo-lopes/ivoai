# Dependency remediation during backend-first validation

The first CI run for PR #31, at `9874dbdf`, stopped in the documentation
production-dependency audit: 23 advisories (one critical, nine high, nine
moderate, four low). Twelve independent jobs passed; two downstream jobs were
skipped. This was not an acceptable promotion result. No merge or release was
performed, and no advisory was excluded to pass the gate.

## Documentation

Exact patch overrides update DOMPurify, fast-uri, follow-redirects, launch-editor,
body-parser, qs, serialize-javascript and undici. The frozen lockfile records
their integrity and the embedded EN/pt-BR site was rebuilt. The production audit
now reports zero advisories; before/after counts and candidate input hashes are
in `evidence/backend-first/dependency-remediation/documentation-audit.json`.
The production build, embedded-site consistency check and docsserver Go tests
passed locally.

An initial accessibility run overlapped a separate consistency rebuild, which
removed the served build temporarily: 149 tests passed and three failed while
pages were unavailable. Those three passed in an isolated rerun. The complete
serialized rerun then passed all 152 checks (5.5 minutes). Exact-head CI remains
a separate promotion gate; the failed run is retained as a failed run.

## Managed Ruflo runtime

The additional local runtime audit found 14 advisories, including eleven high
and three moderate. Pinned updates address TOML, body-parser, fast-uri, qs,
ip-address and the two active undici major ranges. Zod V3 ranges use the explicit
MIT fork documented in `patches/zod3-compat`; V4 ranges retain their four official
4.6.6 copies and integrity values. The candidate production audit reports zero
advisories, without exclusions. This does not by itself prove the fork correct:
its actual early-rejection behavior, V3 consumer compatibility, source identity
and reproducible payload are validated separately.

Ruflo remains 3.56.5, but the installer now identifies its dependency profile by
SHA-256 of the exact embedded package manifest and lockfile. An existing managed
installation with that top-level version still migrates when its dependency
profile differs. npm runs with lifecycle scripts disabled in a private staging
directory, using the managed Node and an isolated environment. The installer
probes the contained launcher before promotion and atomically switches the
wrapper only after successful validation. A nonblocking lock admits one builder;
failure preserves the previous wrapper. Named profiles are never repaired in
place. Existing user-owned installations remain under their prior adoption
policy; this change does not patch an unmanaged third-party installation.

The old flat runtime and earlier profiles remain available for rollback.
Restoring the old wrapper can also restore older vulnerable dependencies; it is
an emergency compatibility recovery, not a security fix. Retention consumes
disk space, and automatic profile garbage collection is intentionally absent.
Managed runtime, cache and wrapper paths must have no symlink ancestors; such
configurations now fail closed. Idempotent admission checks the exact manifests,
wrapper and contained regular launcher; it does not rehash the entire installed
node_modules tree or protect against compromise by the same operating-system
user. No new state schema or database is introduced.

The first real installer run failed closed because npm installed the reviewed
launcher with mode 0775. The installer now normalizes that contained regular
leaf to 0700 only within the new private stage, before its version probe. A
promoted profile with unsafe permissions still fails closed and is never
repaired in place. Regressions exercise both cases; no permission check was
removed to accept the original mode.

The corrected real installer run passed in 38.39 seconds, including the promoted
wrapper and idempotence. Before/after runtime audit counts, the unchanged V4
identities, candidate hashes and both real-installer outcomes are retained in
`evidence/backend-first/dependency-remediation/runtime-audit.json`. All 24 Zod
fixtures passed with Node 26.11.1, including the strict ESM/CJS type checks and
actual pure Security validator from the isolated locked prefix. Nine builder
security tests passed. The archive was rebuilt independently with identical
bytes. The full components race suite passed locally (7.815 seconds). These are
Linux amd64 results, not whole-capability or ARM64 runtime certification.

After the installer fix, the full normal Go suite and `go vet ./...` passed.
Linux amd64 (`GOAMD64=v1`) and ARM64 builds succeeded. Those cross-builds do not
substitute for execution of optional native dependencies on either architecture.

Deterministic installer tests cover same-version lock replacement, legacy
prefix migration, idempotence, preservation of old payloads, executable rollback,
failed npm/version/manifest checks, symlink escapes and concurrent admission.
The opt-in `TestLiveRufloProfileInstallation` uses a checksum-verified official
Node archive to install the real locked graph in a temporary root, promote it
and verify its launcher and idempotent admission. Run it with
`IVOAI_TEST_RUFLO_NODE_ARCHIVE=/absolute/path/node.tar.gz go test
./internal/components -run '^TestLiveRufloProfileInstallation$' -count=1 -v`.
Without that input the test is explicitly skipped and supplies no live evidence.
It does not perform inference, read personal provider credentials, certify
native optional libraries on every CPU, or establish client parity.

## Additional real Ruflo MCP probe

An additional real Ruflo MCP probe against the same locked graph passed
`initialize`, discovery of the ten expected coordination tools and
`swarm_status` (`no_swarm`, zero). It ran under `bwrap --unshare-all`, with no
network, a read-only runtime prefix and temporary HOME/XDG/cwd/cache. Wall time
was 211 ms and sampled process-tree peak RSS was 94560256 bytes across three
processes; payload CPU was not independently measured. No inference, task/swarm
mutation or personal configuration was used. The sanitized result is in
`evidence/backend-first/dependency-remediation/ruflo-mcp-smoke.json`.

The MCP server announces a hardcoded 3.0.0 while the locked package and CLI are
3.56.5; that report is not package identity attestation. Upstream `--tools`
filters discovery and is not proof that hidden tool names cannot execute.
IVOAI grants and containment remain authoritative. This narrow probe is not
certification of lifecycle mutations, every Ruflo capability or other platforms.

## Go toolchain promotion gate

CI run [40591751032](https://github.com/ivo-lopes/ivoai/actions/runs/40591751032)
on `2ba265cb` passed the twelve independent platform jobs, normal/race Go tests,
documentation/a11y, both npm audits, native protocol fixtures, vet, build and
ShellCheck. Promotion still failed: the workflow selected Go 1.27.2 from go.mod,
and govulncheck reported reachable
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) in crypto/x509. Two dependent
server jobs were skipped, not passed. No merge or release followed that result.

Go 1.27.3 includes the upstream correction. The module minimum, source-bootstrap
installer checksums, component manifest and third-party notice now pin 1.27.3.
Both Linux archive SHA-256 values were checked against the
[official release metadata](https://go.dev/dl/?mode=json). Local checks above
already used the reviewed Go 1.27.3 archive; this difference explains why the
local vulnerability scan passed while CI correctly rejected the older compiler.
The vulnerability gate remains enabled and a new exact-head CI run is required.

The isolated source installer smoke also passed with a PATH containing no Go:
it downloaded and checksum-verified 1.27.3, built and installed the real binary,
verified repeat-install idempotence and exercised the existing mock setup/server
contracts. The setup fixtures are not provider or production-server evidence;
personal installations and configuration were untouched.

## Native-client limitation

Claude Code 2.1.251 administrative authentication status returned exit 1,
`loggedIn=false`, `authMethod=none`. No inference or paid fallback was attempted.
The sanitized result is retained in
`evidence/backend-first/native-clients/claude-auth-status.json`. Runtime and
native TUI parity remain unverified for Claude in this environment.
