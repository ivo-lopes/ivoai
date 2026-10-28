# Managed OpenCode local-plugin policy validation

This is a compatibility fix supporting IVOAI-191/192/195, not completion of the
OpenCode removal or any economic gate. The component build began on main `2bf9f943`; the final local source also includes
main `3be82befea6ed3875373afcb1bfaa450710310ac`, preserving its administrative
OAuth correction, guide and release gate. It does
not modify the historical `.2` archives, private configurations, credentials or
histories. No release or production installation is performed by this record.

## Reproduced problem

[CI40715183470](https://github.com/ivo-lopes/ivoai/actions/runs/40715183470),
PR44's documentation-only head `8e56741a`, failed real managed frontend readiness:
`/global/health` was available but `/config/providers` reported eight communication
failures before its unchanged 20-second deadline. A separate smoke of the
checksum-verified public v0.13.0 `.2` archive also failed API and executor HTTP
readiness; other real prompt/TUI/permission/resize/native-MCP cases passed. Those
failures are preserved; later passes do not rescore them.

The exact runner network delay remains UNKNOWN because that failed job did not
retain its private SDK-install diagnostics. An independent, executable causal
experiment found an upstream dependency: Config starts npm SDK installation in
each private configuration directory; a configured local plugin makes Plugin
wait for those dependencies; provider initialization waits for Plugin. Model
fetching/default-plugin flags do not disable this path.

With the published `.2` ELF `920c256f6c8c6950e1194c1b15d997bd61d8dfbe3144dac8c5d4e0d723f0aa60`
in an isolated network namespace, a local sentry registry held the SDK request.
Health returned 200, two provider requests each timed out at 2,002ms, and releasing
the sentry with 404 let the provider catalog respond in 859ms. No package was
installed. The no-plugin control returned its catalog in 4ms while that SDK
request remained held. A 404 is a diagnostic control, not a proposed workaround.
Read-only configuration was also tested, but rejected as the implementation
strategy because DAC permissions do not establish the contract for privileged
users. The final policy does not depend on chmod tricks.

## Delivered policy and migration

The distinct build `1.19.0-ivoai.3` keeps upstream commit
`733b2c627e5aec39cb33cc22373bdce33053ef2c`, Bun 1.3.14, existing clipboard/public-SDK
patches, MIT provenance and baseline amd64 packaging. A new explicit private flag
skips Config/TUI dependency preparation and refuses npm reification before package
resolution/install. Config, loader and TUI reject npm packages, remote URLs,
foreign files and symlinks before execution. Only the exact private owned regular
server/TUI entrypoints are admitted. Interactive plugin installation is refused.
The opt-in is not inherited from the user's environment; the host supplies it
and the owned asset root. Upstream use without the flag is unchanged.

The server module has no imports. The TUI uses the host-bundled `solid-js` and
`@opentui/solid` aliases plus three sibling IVOAI modules without external imports.
Real offline HTTP/attach tests validate these imports without a fake SDK, npm
package preseed, provider credentials or inference. Providers requiring additional
SDK installation are unavailable in this controlled mode; broad native-provider
parity is not claimed. The mechanism is not a sandbox against a malicious process
with the same OS identity changing already owned files. The asset root and all
ancestors must resolve to their lexical canonical path; old symlink-ancestor
configurations are refused. Use canonical private runtime/cache paths for the
same retained data; no automatic relocation or symlink rewrite occurs. Those
noncanonical layouts were not certified by these fixtures.

`StartManaged` refuses `.2`, upstream and unknown versions with
`MANAGED_OPENCODE_UPDATE_REQUIRED` before its backend lease/serve process. This
is not a claim that earlier caller-level version probes or logical-session
metadata never existed. Official releases embed verified `.3` metadata; managed
setup/update promotes a new immutable object while retaining historical objects
for rollback. A source-only Core retaining the upstream catalog cannot fabricate
`.3` by setup: use an official release or the existing reviewed artifact/metadata
build procedure. Rolling back Core and component together restores prior behavior,
including the old dependency wait. No personal state is deleted.

## Evidence and limits

The exact locally built archive and runtime-source hashes are bound in
`evidence/managed-opencode-offline-plugins/validation.json`. The immutable archive
was compiled before the builder's new source-contract probes were registered;
the same source/Bun was subsequently checked directly. No compiled TypeScript
changed after that artifact was built. CI rebuilds and executes the probes through
the final builder. Source probes assert the specific typed denial cause for both
npm install and dynamic add, zero registry requests, entrypoint/path checks and
unflagged compatibility. The final source probe owns and disposes its Effect
runtime and exits normally, without `process.exit(0)`. It runs outside a network
namespace and proves zero sentry hits plus the specific denial; actual network
containment is established separately by the binary gate. This is not a general
subprocess-lifecycle certification.

Real Go bridge/agent fixtures ran in a different network namespace with private
HOME/XDG, no mounted operator home, synthetic local providers and mandatory zero
skips. API create/list/get/prompt/async/status/abort/diff/files/agents/MCP/permissions
and SSE passed. Prompt routing/owned resume, three TUI widths, full/interactive
permissions, resize/Escape, executor lifecycle, actual fixture MCP projection,
approval and cancellation passed. Policy cases also passed at UID1000 and UID0:
five managed cases per identity made zero registry requests; each unflagged
control made exactly one held request and resumed after its fixture error.

Adverse construction results are retained separately: the first negative oracle
expected an internal denial marker in the HTTP body, whereas the server correctly
returned generic 500 with the marker only in its private log; the corrected oracle
requires both status and private marker. The next sandbox omitted `/bin`, so the
native fixture's `#!/bin/sh` wrapper could not execute; adding the standard
`/bin -> /usr/bin` alias fixed that fixture environment. The first source probe
completed assertions but retained Effect handles and was explicitly terminated
(exit143); an intermediate explicit-exit version was replaced by owned runtime
disposal with a normal exit. A stricter denial regex then failed because the typed error stores its
marker in `cause`; the final oracle checks both `_tag` and exact `cause`. None of
these construction failures were provider attempts or ignored successes.

CI retains the existing network smoke, clipboard contracts, race and compatibility
gates. Exact archive offline checks are added to AMD64/ARM managed builds and the
release build, using the existing CI-only AppArmor admission for bubblewrap;
global user-namespace policy is not disabled. Namespace/test/SKIP failures fail
the new gate. ARM runtime and non-AVX execution still require their actual CI
jobs; local Go cross-builds alone do not certify them. Session tokens, cost, quotas,
global economy, production deployment and authenticated parity are unmeasured.

Reproduce from a reviewed build:

```sh
scripts/build-managed-opencode.sh "$PWD/dist/managed-frontend"
scripts/test-managed-opencode.sh \
  "$PWD/dist/managed-frontend/ivoai-opencode_linux_amd64.tar.gz" \
  "$PWD/dist/managed-frontend/managed-opencode-amd64.json"
```

The integration script includes offline policy and HTTP/TUI checks. A supported
bubblewrap setup is required; no missing sandbox is counted as a pass. Private
validation workspaces are retained locally for inspection and must not be uploaded
wholesale because fixtures contain disposable authentication/state.

`Npm.add` may reuse a retained SDK cache without reaching reification. The
managed npm guard prevents installation; it is not a universal prohibition or
attestation of every dynamically loaded provider SDK. Plugin entrypoints remain
restricted independently before package resolution. No SDK cache is deleted, and
cached-provider behavior/isolation is not generally certified by these fixtures.

After combining main, the exact same component ELF passed the complete offline
suite again with mandatory PASS records for all eight named top-level tests and
zero skips. Combined policy data and bridge/agent logs are retained alongside the
prior epoch. Full Go, affected race tests, vet, cross-builds and own frozen
EN/PT documentation checks passed; their hashes are in the validation record.
The new patch file contains seven normal unified-diff blank context lines that
an initial staged whitespace check reported. Its bytes were retained for immutable
patch/component provenance; other source whitespace and the final merge diff
were clean. No whitespace or CI configuration was weakened to hide that format.

The documentation-only certification notes from
`df8cbb420975f2bfbaf0885022bcfb9a00725530` were subsequently integrated as
`4ee0004e296c70a198f9f6e9d30708a3eac58250`. Own frozen EN/PT site build/check,
focused embedded-portal tests and both Core cross-builds passed again. This adds
the candidate migration notes and separately scoped 126-attempt economic history;
it does not incorporate the runtime of PR45/46/47 or publish the candidate.
No upstream component source changed and no additional inference was run.
