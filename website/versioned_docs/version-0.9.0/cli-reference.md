# CLI reference

<!-- GENERATED-CLI-HELP:START -->
```text
ivoai - personal AI client and server platform

Usage:
  ivoai                         interactive menu
  ivoai help | version | status | uninstall
  ivoai setup [--mode client|server]
  ivoai doctor [--json] [--inventory]
  ivoai update [--dry-run] | update --rollback [--force]
  ivoai connect [list|chatgpt|claude]
  ivoai connect server [--url URL] [--purpose PURPOSE] [--redundancy-group GROUP] [--priority N] [--enrollment-code CODE|--code-stdin]
  ivoai connect server add <alias> [--url URL] [--purpose PURPOSE] [--redundancy-group GROUP] [--priority N] [--enrollment-code CODE|--code-stdin]
  ivoai connect server list [--json] | show <alias> [--json] | test <alias> [--json]
  ivoai connect mcp [list] | add <name> <https-url> | remove <name>
  ivoai disconnect <chatgpt|claude|server [alias|--all]>
  ivoai codex [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai claude [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai opencode [--knowledge-source <alias|purpose>]
  ivoai auto [--planner codex|claude] [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai session start --executor <codex|claude|opencode> --mode <direct|orchestrated> [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai session list [--json] | show [--json] <id> | stop <id>
  ivoai monitor [--watch] [--session <id>] [--json]
  ivoai memory [status|configure]
  ivoai config [show|set <key> <value>]
  ivoai project [init|status]
  ivoai server setup | status | doctor | start | stop | restart
  ivoai server logs [service]
  ivoai server enrollment create [--ttl 10m] | list | revoke <id>
  ivoai server web-access create [--ttl 10m] [--scopes SCOPE,...] | list | revoke <id>
  ivoai server connector list | add --name NAME --type filesystem|git --path PATH | remove NAME
  ivoai server context status | memory status | docs status | docs configure --listen IP:PORT | docs serve
  ivoai server gateway serve | gateway configure --public-url HTTPS_ORIGIN [--listen HOST:PORT] [--trusted-proxy CIDR] [--tls-cert PATH --tls-key PATH]
  ivoai server backup [--output PATH] | restore --input PATH
  ivoai server remote status | doctor | connector list
```
<!-- GENERATED-CLI-HELP:END -->

The block above is generated from the executable's canonical help output and is
checked in CI by `scripts/generate-cli-reference.sh --check`.

## Session-source flag

Repeat `--knowledge-source` or use comma-separated aliases/purposes. With no flag,
all enabled connected server profiles are selected. With any flag, it becomes a
restrictive filter. Unknown, disabled, or disconnected explicit sources fail.

## Public defaults and effects

| Surface | Default / effect |
| --- | --- |
| `setup --mode` | `client`; an existing server installation is detected and retained when the flag is omitted |
| `auto --planner` | interactive/default planner selection; only `codex` and `claude` are valid overrides |
| `session start --executor` | `codex` |
| `session start --mode` | `direct` |
| `connect server add --purpose` | the validated alias |
| `connect server add --priority` | `100`; lower values win within one redundancy group |
| `doctor --inventory` | adds a sanitized compatibility inventory; combine with `--json` for automation |
| `update --dry-run` | stages and probes without committing |
| `update --rollback` | restores the last transactional checkpoint; `--force` is valid only with rollback |
| `monitor --watch` | refreshes until cancelled; `--session` restricts the displayed session |
| `server enrollment/web-access create --ttl` | `10m` |
| `server backup --output` | managed timestamped backup path |
| `server docs status` | probes the configured production listener, default `0.0.0.0:7780` |

`--enrollment-code` is accepted for compatibility but exposes a secret in process
arguments; prefer `--code-stdin` or the hidden interactive prompt. JSON flags write
machine-readable JSON without banners or ANSI. Validation and runtime errors are
written to stderr; sensitive values are redacted.

## Server documentation

`ivoai server docs status` reports the configured listener; `ivoai server docs serve`
is reserved for the managed systemd unit. The default is `0.0.0.0:7780`.

## Public environment variables

- `NO_COLOR=1`: disable ANSI output.
- `IVOAI_ASCII=1`: use ASCII terminal rendering.
- `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`: relocate user-owned state.
- `IVOAI_VERSION`: select a release in the bootstrap installer.
- `IVOAI_INSTALL_DIR`: select an installer target.

Internal session capability variables and test-only variables are intentionally not
public interfaces. Commands return zero on success, non-zero on validation/runtime
failure, and 130 on cancellation where the executor lifecycle supports it.
