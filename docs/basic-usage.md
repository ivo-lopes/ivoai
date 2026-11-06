# Basic usage

```bash
ivoai codex
ivoai claude
ivoai auto
ivoai status
ivoai doctor
```

`ivoai codex` starts the official native Codex TUI with IVOAI Core admission.
`ivoai auto` is a compatibility alias for the same path. Natural prompts do not
require formal acceptance headings. Adaptive Primary execution without a DAG is
an experimental opt-in, OFF by default; the existing planning/scheduler path and
all authorization checks remain available.

`ivoai codex --direct` and `ivoai claude` use the official direct clients.
Managed headless clients have separate explicit feature flags and do not imply
native TUI interception. `ivoai opencode`, including `--direct`, now returns a
migration error and preserves all existing user data.

See [Orchestrated frontends](orchestrated-frontends.md) for prompts and decisions.
