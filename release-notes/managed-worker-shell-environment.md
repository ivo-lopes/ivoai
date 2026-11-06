# Managed worker shell environment

The controlled Codex worker path disables login-shell startup and personal shell
snapshots using the official allow_login_shell and features.shell_snapshot
settings. These settings exist in the reviewed0.162.1 and0.166.0 schemas. The
same task sandbox, MCP allowlist, approval policy and write grants still apply.
The host's projected toolchain PATH is retained instead of importing shell
profile changes. User-owned configuration is not edited.

Scoped worker contract tests passed locally. This is not yet evidence that the
previous Antigravity DAG's missing Go executable is resolved: that fixture must
be repeated with the new worker source and must execute its Go checks inside
the actual worker. It also does not certify arbitrary older client versions or
new writable caches, toolchain installation, filesystem mounts or permissions.

Sources: official pinned Codex configuration schemas at
https://github.com/openai/codex/blob/rust-v0.166.0/codex-rs/core/config.schema.json
and
https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/config.schema.json .
