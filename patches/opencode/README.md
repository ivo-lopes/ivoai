# Managed OpenCode clipboard patch

Upstream baseline: `anomalyco/opencode` v1.18.25,
`cb7d8b2f5e44876ef98b661dc10590c915af3a9f` (MIT).

The clipboard writer catches native backend failures and resolves its promise.
The selection, message and dialog callers consequently display success even
when no native clipboard backend succeeded. v1.18.30 contains the same writer.
Forwarding the minimal desktop environment fixes transport access, but cannot
fix this error contract. The distributed plugin host does not expose the
clipboard service; importing its source package breaks plugin loading.

`1.18.25-clipboard-errors.patch` is a narrow, source-level patch: native failures
reject with a bounded message; write-only subprocesses do not retain stdout pipes
through background clipboard owners; stdin errors and process timeouts are handled.
OSC52 remains a best-effort fallback, never proof of desktop delivery.
It does not change selection, logical text extraction, mouse handling, provider
configuration, or orchestration. It deliberately does not certify OSC52-only
delivery as successful desktop copying. No upstream tag or binary is modified.

Build identity: `1.18.25-ivoai.1`. The official workflow builds this patch on the
pinned source with the pinned Bun compiler, without dependency installation
hooks. Separate archives, build metadata and immutable checksums preserve its
provenance. The IVOAI binary embeds the archive checksum and source/patch identity;
it never replaces the original immutable upstream object. Development builds
without that build metadata retain the upstream catalog. No artifact is certified
merely because this patch exists.

Focused real-TUI test (with the explicitly selected patched binary):

```sh
IVOAI_LIVE_OPENCODE_PATH=/path/to/patched/opencode \
IVOAI_LIVE_OPENCODE_VERSION=1.18.25-ivoai.1 \
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
