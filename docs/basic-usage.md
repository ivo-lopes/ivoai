# Basic usage

```bash
ivoai codex
ivoai claude
ivoai opencode
ivoai status
ivoai doctor
```

`ivoai codex` starts the native Codex TUI with IVOAI-controlled admission. `ivoai opencode`
starts managed OpenCode. Both use the same prompt gate, native DAG, plan approval,
worker scheduler and capability policies. Codex is the preferred primary in the
Codex frontend, not necessarily every worker's executor.

Use `ivoai codex --direct` or `ivoai opencode --direct` for the official standalone
clients without orchestration. `ivoai claude` is unchanged. `ivoai auto` remains a
deprecated alias for `ivoai opencode`, with a warning on stderr only.

See [Orchestrated frontends](orchestrated-frontends.md) for prompts and decisions.
