# Managed terminal UX audit

Validated for the OpenCode-first AUTO experience on 2026-09-04. The guiding rule is
one authoritative state model: session, executor, quota and knowledge data originate
in IVOAI and are rendered by the managed frontend. JSON and non-TTY output never
receive branding or ANSI decoration.

## Information hierarchy

The normal path is `ivoai auto`: preflight, knowledge selection, private backend,
then the IVOAI-themed OpenCode TUI. The home footer shows only actionable summary
state; the sidebar shows knowledge sources; `/ivoai` opens the complete bounded
panel. Symbols are always paired with words, so status never depends on color alone.

| UI field | Authoritative source | Missing evidence |
| --- | --- | --- |
| version | binary build metadata | `N/A` |
| frontend | session metadata | `N/A` |
| primary executor | quota scheduler/session | `Unknown` |
| worker | task ledger/runtime | `Not available` |
| authentication | official executor auth probe | `authentication required` |
| quota | quota manager | `N/A` |
| Memory / Context | session knowledge bootstrap | `degraded` |
| servers | ServerPool snapshot | `0 configured` |
| knowledge scope | Knowledge Router selection | `automatic` or `restricted` |
| compression | CompressionProvider policy | `Unknown` |
| skills | Skill Registry | `Not available` |

## Menu and route inventory

| Surface | Decision | Rationale in managed mode |
| --- | --- | --- |
| IVOAI launcher: Automatic | KEEP / PRIMARY | Opens the OpenCode frontend under IVOAI control. |
| IVOAI launcher: Codex | KEEP | Explicit official Codex TUI escape hatch. |
| IVOAI launcher: Claude | KEEP | Explicit official Claude Code TUI escape hatch. |
| IVOAI launcher: OpenCode | RENAME | Opens the managed IVOAI OpenCode frontend. |
| `ivoai opencode` | KEEP | Same managed control plane as AUTO. |
| standalone OpenCode provider | MOVE | Explicit `session start --executor opencode --mode direct`. |
| status / doctor | KEEP | Operational and machine-readable diagnostics. |
| monitor | KEEP | Existing read-only runtime view; not duplicated in the TUI panel. |
| OpenCode `/ivoai` | ADD | Full session, executor, quota and source status. |
| OpenCode home footer/sidebar | ADD | Persistent, compact scope and health. |
| OpenCode `/connect` | HIDE BY CONFIG | Only the IVOAI provider is enabled; executor auth stays official. |
| OpenCode model/provider picker | GROUP BY CONFIG | Exposes only `ivoai/auto` in managed mode. |
| OpenCode share | DISABLE IN MANAGED MODE | Prevents accidental conversation publication. |
| OpenCode auto-update | DISABLE IN MANAGED MODE | Supply-chain pin and rollback stay authoritative. |
| OpenCode update | HIDE IN MANAGED MODE | IVOAI updates the pinned component. |
| session picker | KEEP | OpenCode sessions map to bounded IVOAI executor session IDs. |
| theme picker | KEEP | IVOAI theme is installed and selected; accessibility remains available. |
| direct `opencode` menus | KEEP UPSTREAM | IVOAI does not modify use outside managed mode. |

## Responsive terminal policy

IVOAI uses OpenCode's supported renderer and plugin slots rather than parsing ANSI or
automating keystrokes. Long aliases and purposes are sanitized and capped; the panel
shows at most eight source rows plus a remainder count. OpenCode owns Unicode width,
resize, focus, cursor and terminal cleanup. `TERM=dumb`, `NO_COLOR`, pipes and JSON
continue through the existing non-TUI IVOAI surfaces.

## Stable public action inventory

Every action below is covered by the menu-tree regression test. The decisions are
`KEEP` unless the table above explicitly says `RENAME`, `MOVE`, `HIDE`, `DISABLE`, or
`ADD`; nested section entries are grouped in the launcher instead of duplicated at
the top level.

```text
auto
status doctor doctor.inventory version
setup update.dry-run update rollback uninstall
connect.list connect.chatgpt disconnect.chatgpt connect.claude disconnect.claude connect.server disconnect.server
mcp.list mcp.add mcp.remove
launch.codex launch.claude launch.opencode
memory.status memory.configure
session.direct.codex session.direct.claude session.direct.opencode session.orchestrated.codex session.orchestrated.claude
session.list session.monitor session.stop
project.status project.init
config.show config.headroom config.memory config.ruflo config.auto config.auto-planner config.auto-failover config.auto-checkpoint
config.auto-strategy config.auto-parallel config.auto-bootstrap config.auto-escalation config.session-mode config.primary config.reviewer config.workers
server.setup server.status server.doctor server.start server.stop server.restart server.logs
server.enrollment.create server.enrollment.list server.enrollment.revoke
server.web-access.create server.web-access.list server.web-access.revoke
server.connector.list server.connector.add server.connector.remove server.context.status server.memory.status
server.gateway.configure server.backup server.restore
remote.status remote.doctor remote.connector.list
```

## Visual system

The TUI theme uses neutral ink/slate surfaces, a restrained blue primary and amber
accent. Semantic success, warning and error colors have textual markers. Light and
dark palettes are authored independently with high-contrast text, borders and diff
colors. IVOAI lettering uses the official `home_logo` slot; the upstream OpenCode
binary, license, notices and provenance are not patched or hidden.
