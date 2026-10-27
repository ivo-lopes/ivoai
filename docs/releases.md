# Releases

## v0.13.0 — prepared, publication gates pending

Incremental backend-first delivery: fail-closed benchmark evidence, an optional
Evidence Gateway disabled by default, informal-prompt intake, provider-neutral
executor-turn contracts and isolated dependency-profile updates for managed Ruflo.
OpenCode remains operational; primary execution still follows the existing DAG
completion policy. Claude authentication was not confirmed; Antigravity runtime
integration and Context ingestion remain unverified/blocked.

**Economic acceptance failed:** the final three-scenario comparison reduced input
by 5.43% and elapsed time by 0.12%, below the 10%/5% targets. All four datasets
retain 72 real attempts; the global 16-scenario gate remains INCOMPLETE. No general
savings, complete migration or new client parity is claimed. Exact-head CI,
distributed-artifact and release gates remain pending; no tag or publication is
claimed. See the committed [backend validation](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/backend-first-validation.md),
[Core contract validation](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/backend-neutral-turn-contracts.md) and
[dependency/rollback report](https://github.com/ivo-lopes/ivoai/blob/5949f20d238acc1c4108515fc462b88e979537c3/release-notes/dependency-remediation-validation.md). Prepared bilingual notes are in
`release-notes/v0.13.0.md` in the release-preparation branch.

The earlier PR #31/#32 pipelines passed their test/race/docs/audit/ShellCheck
checks but failed the Go 1.27.2 vulnerability gate for
[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809). The prepared candidate now
requires Go 1.27.3, including both verified Linux bootstrap archives. New CI for
`5949f20d` is pending; the earlier partial passes are not a green promotion result.

## v0.12.1

Compatible corrections to exact/API retrieval, sanitized operation telemetry and
OpenCode catalog readiness. Published after explicit maintainer instruction with
**economic acceptance still failed** and the full 16-scenario comparison incomplete.
No aggregate token or latency savings are claimed. See the
[release notes](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/v0.12.1.md)
for validation, compatibility and rollback limits.

## v0.12.0

Consolidates the P1/P2 session, hook-recovery, telemetry and automation-profile
changes with managed [native Graft code intelligence](native-graft.md). The
runtime selects bounded direct or structural inspection, preserves institutional
Memory/Context routing, and confines indexing to private snapshots without network
or project-script execution. Claude remains supported by contract tests; real
Claude validation under IVOAI-188 is still deferred. See the
[complete notes](https://github.com/ivo-lopes/ivoai/blob/v0.12.0/release-notes/v0.12.0.md)
for compatibility, measured evidence and recovery limits.

## v0.11.0

Updates ai-memory to 2.6.3, Ponytail to 5.1.0, managed executors and reviewed locks;
fixes known Go/npm vulnerabilities and adds the governed Claude Red catalog.
Real Claude validation remains pending account access. Back up ai-memory 1.x data
with writes stopped before migration; binary rollback does not reverse database
migrations. See versions, compatibility limits, security and recovery in the
[complete notes](https://github.com/ivo-lopes/ivoai/blob/v0.11.0/release-notes/v0.11.0.md).

## v0.9.2

The `v0.9.2` stable patch keeps the OpenCode-first architecture while restoring
conformant federated Memory/Context results for Codex. It also exposes runtime model
and reasoning choices through OpenCode's native picker, persists requested and
effective selection metadata, classifies bridge failures without accepting partial
output, drains federated initialization deterministically, unifies the documentation
canvas, and adds complete English and Brazilian Portuguese locales. Complete notes
live in [`release-notes/v0.9.2.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.2/release-notes/v0.9.2.md).

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
