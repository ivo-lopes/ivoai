# Managed OpenCode patches

Upstream baseline: `anomalyco/opencode` v1.19.0,
`733b2c627e5aec39cb33cc22373bdce33053ef2c` (MIT).

The clipboard writer catches native backend failures and resolves its promise.
The selection, message and dialog callers consequently display success even
when no native clipboard backend succeeded. v1.18.30 contains the same writer.
Forwarding the minimal desktop environment fixes transport access, but cannot
fix this error contract. The distributed plugin host does not expose the
clipboard service; importing its source package breaks plugin loading.

`1.19.0-clipboard-errors.patch` is a narrow, source-level patch: native failures
reject with a bounded message; write-only subprocesses do not retain stdout pipes
through background clipboard owners; stdin errors and process timeouts are handled.
OSC52 remains a best-effort fallback, never proof of desktop delivery.
It does not change selection, logical text extraction, mouse handling, provider
configuration, or orchestration. It deliberately does not certify OSC52-only
delivery as successful desktop copying. No upstream tag or binary is modified.

`1.19.0-plugin-sdk-version.patch` keeps the plugin SDK dependency at the public
upstream version `1.19.0`. The private executable version is not a published npm
SDK version. Both configuration and TUI dependency preparation use this pin;
npm lifecycle scripts remain disabled. The build verifies both call sites with
`scripts/test-opencode-plugin-version.py`.

`1.19.0-managed-local-plugins.patch` adds an explicit private-runtime contract.
`IVOAI_MANAGED_LOCAL_PLUGINS=1` skips configuration/TUI dependency preparation
and refuses npm reification before registry resolution or installation. The
private host assets directory admits only the exact regular, private, owned
`server-plugin.mjs` and `tui-plugin.tsx` entrypoints; package plugins, remote
URLs, symlinks and foreign files fail closed. Interactive package installation
is also unavailable in this mode. Without the flag, upstream behavior and the
public SDK pin above remain unchanged. No fake SDK or writable-cache shortcut is
used. This is not protection against a malicious process with the same OS user.

The server entrypoint has no imports. The TUI imports the host's bundled
`solid-js` and `@opentui/solid` runtime aliases plus three sibling IVOAI modules
with no external imports. These are exercised with the real HTTP/attach client,
private HOME/XDG and a private network namespace. A separate sentry registry
asserts zero requests for managed positive/negative cases and direct npm gates;
an unflagged control preserves upstream dependency waiting. A successful
synthetic client contract is not authenticated inference or economic evidence.

Build identity: `1.19.0-ivoai.3`. The official workflow builds these patches on the
pinned source with the pinned Bun compiler, without dependency installation
hooks. Separate archives, build metadata and immutable checksums preserve its
provenance. The IVOAI binary embeds the archive checksum and source/patch identity;
it never replaces the original immutable upstream object. Development builds
without that build metadata retain the upstream catalog. No artifact is certified
merely because this patch exists.

The amd64 archive packages upstream's `x64-baseline` target so AVX/AVX2 is not
required (including Xeon E5 v2 hosts). ARM64 keeps its native target. The immutable
object revision hashes source, patch and archive SHA-256 values, each followed by
a newline. This prevents rebuilt binaries from colliding with the older
source/patch-only object. CI and release builds run the distributed amd64 binary
under QEMU's Nehalem CPU model before publication, as well as native TUI checks.
`scripts/test-opencode-baseline.sh ARCHIVE METADATA` requires `qemu-x86_64` and
verifies the archive checksum and executable version with no AVX/AVX2 available.

Focused real-TUI test (with the explicitly selected patched binary):

```sh
IVOAI_LIVE_OPENCODE_PATH=/path/to/patched/opencode \
IVOAI_LIVE_OPENCODE_VERSION=1.19.0-ivoai.3 \
IVOAI_LIVE_OPENCODE_TUI=1 \
IVOAI_LIVE_OPENCODE_CLIPBOARD=failure \
go test ./internal/opencodebridge -run '^TestLiveManagedOpenCodeResizeKeyboard$' -count=1
```

Repeat with `IVOAI_LIVE_OPENCODE_CLIPBOARD=success`. Set
`IVOAI_LIVE_OPENCODE_COPY=mouse` for native selection, or `multiline` for logical
text, a code block and visual wrapping. `scripts/test-patched-opencode.sh` runs
both copy paths and success/failure fixtures against the built archive. These
default tests use isolated fake native backends, not the operator's clipboard.

An explicit `IVOAI_LIVE_OPENCODE_CLIPBOARD=desktop` smoke additionally reads back
the real X11 clipboard through a separate process. It preserves text in memory,
skips rich formats, restores the prior value only while the fixture still owns
the clipboard, and verifies restoration. No clipboard contents are logged.

Managed `.2`, upstream and unknown builds are explicitly refused by the new
host inside `StartManaged`, before its backend lease or `serve` process starts; `.2` cannot silently ignore this flag. Caller-level capability/version probes or
logical-session metadata may already exist before this backend-specific gate.
Published IVOAI builds embed the reviewed `.3` archive metadata, so ordinary
managed setup/update promotes a new immutable component and retains the old
object for rollback. Configurations, authentication and histories are preserved.
A source-only Core build without embedded managed metadata retains the original
upstream catalog and cannot obtain the new contract through setup alone: use an
official release or build the reviewed artifact and embed its verified metadata
through the existing release procedure. An old Core with its old `.2` object
remains a rollback option, with the old runtime dependency behavior.

The managed opt-in also refuses a provider SDK that would require an npm install;
it does not invent support for every upstream native provider. Existing bundled
SDKs and built-in authentication adapters retain their upstream contracts. The
live fixtures use local synthetic providers and do not certify subscription
credentials, paid APIs or cross-provider inference parity.

The asset root and every ancestor must resolve to the exact lexical canonical
path; symlink ancestors are rejected as well as symlink entrypoints. Existing
`EnsurePrivateDir` checks do not establish this stronger property. Use canonical
private runtime/cache paths targeting the same retained directories; no automatic
relocation, history migration or symlink rewrite is performed. Noncanonical
installations require operator correction and are not compatibility-certified by
the isolated tests above.

`Npm.add` may reuse a retained SDK cache without reaching reification. The
managed npm guard prevents installation; it is not a universal prohibition or
attestation of every dynamically loaded provider SDK. Plugin entrypoints remain
restricted independently before package resolution. No SDK cache is deleted, and
cached-provider behavior/isolation is not generally certified by these fixtures.
