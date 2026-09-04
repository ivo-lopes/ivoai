# Releases

## v0.9.0

The `v0.9.0` stable release makes the pinned OpenCode TUI the managed frontend for
AUTO while IVOAI remains the session, executor, policy, quota, and knowledge control
plane. Codex and Claude Code continue to own their subscription authentication; no
provider credential is copied into OpenCode. It also adds resumable frontend ↔
executor session mappings, persistent multi-server scope/health visibility, an IVOAI
theme and panel built with supported OpenCode plugin APIs, and an accessibility
hardening pass for the self-hosted documentation portal. Complete notes live in
[`release-notes/v0.9.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.0/release-notes/v0.9.0.md).

## v0.8.0

The `v0.8.0` stable release adds all-enabled multi-server read federation, hardened
Debian 12/LXC bootstrap, the embedded production documentation portal, and explicit
remote Web MCP conformance. Its complete public notes live in
[`release-notes/v0.8.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.8.0/release-notes/v0.8.0.md).

Stable releases are immutable tags built by GitHub Actions. Each release contains
Linux amd64/arm64 binaries, the Memory/Context skill archive, and SHA-256 checksums.

The documentation portal exposes `latest` from the installed binary and preserves a
versioned snapshot for each documented release. Release notes state migrations,
fallback behavior, known limitations, and rollback instructions.
