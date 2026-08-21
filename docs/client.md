# Client

## Interactive menu

Run `ivoai` without arguments for the full menu. In a TTY use Up/Down (or `j`/`k`),
Enter, Esc, and `q`. The menu restores cooked terminal mode before prompts and before
launching Codex or Claude. Destructive operations require an exact confirmation
phrase.

When stdin/stdout are not terminals, ivoai prints a numbered fallback. Set
`NO_COLOR=1` to disable ANSI color or `IVOAI_ASCII=1` to avoid Unicode lettering.
Subcommands remain the stable interface for automation. Progress is written to
stderr so stdout and `doctor --json` stay machine-readable.

The renderer reads both width and height on every frame and reacts to `SIGWINCH`.
Wide terminals show the complete block/shadow lettering, readiness badges, and
descriptions. Medium terminals use a reduced banner and compact descriptions. Small
terminals use a one-line wordmark, a height-bounded viewport, and a position
indicator. Badges wrap and labels are truncated by displayed Unicode cell width, so
the interface never relies on a fixed 80-column terminal.

Interactive human-facing screens use the same semantic palette: cyan for active
work, violet for headings, green for success, yellow for degraded results, and red
for failures. The main menu, every submenu, and every human-facing command use the
same adaptive ivoai lettering and print the running binary version directly below
it. Lettering, version decoration, cursor animation, and color are deliberately
absent from machine-readable and redirected output.

## Installer presentation

`install.sh` uses the same responsive ivoai banner as the CLI, reports the exact
installed version, detected platform, architecture, installation target, and phases.
Known-size transfers use a byte/percentage
bar; checksum, extraction, source build, and registration use a spinner with elapsed
time. If a step fails, the installer stops the animation, prints the related log in
a readable error block, and leaves no partial temporary download.

On success it reports the installed path and the next command. A normal user is
directed to `ivoai setup`; a root server installation is directed to
`ivoai setup --mode server`. Animation automatically becomes periodic plain text
when stderr is not a compatible terminal.

## Files and ownership

ivoai follows the XDG Base Directory Specification:

| Purpose | Default |
| --- | --- |
| Configuration | `$XDG_CONFIG_HOME/ivoai` or `~/.config/ivoai` |
| Data and managed assets | `$XDG_DATA_HOME/ivoai` or `~/.local/share/ivoai` |
| State and ownership manifest | `$XDG_STATE_HOME/ivoai` or `~/.local/state/ivoai` |
| Cache | `$XDG_CACHE_HOME/ivoai` or `~/.cache/ivoai` |

Directories containing private state use mode `0700`; secret files use `0600`.
The main TOML file contains status and preferences, not bearer tokens.

`ivoai setup` is idempotent. It records whether each executable was already present
or installed by ivoai. `ivoai uninstall` removes only managed files and binaries; it
does not remove third-party logins or pre-existing tools.

## Components

Versions and installation sources are centralized in `manifest/components.yaml`.
Setup checks the platform, downloads pinned artifacts, verifies the reviewed integrity
data, installs managed wrappers, and reports independent failures. Headroom uses
architecture-specific hash-locked constraints; Ruflo uses its complete npm lockfile.
Updates are explicit through `ivoai update`. A successful update retains the previous
binary; `ivoai update --rollback` restores it atomically and runs Doctor again.

The healthy disconnected state is:

```text
ivoai          ready
Codex          installed / not connected
Claude Code    installed / not connected
Headroom       ready
ai-memory      installed / not connected
Ruflo          ready / provider execution disabled
Server         not-connected

Overall: READY — external connections pending
```

## Agent launch

`ivoai codex` and `ivoai claude` preserve the terminal, working directory, signals,
and agent exit code. When enabled and healthy, Headroom's supported wrapper is used.
If Headroom is unavailable, unhealthy, or incompatible during preflight, ivoai
starts the official agent directly. Once a selected wrapper process starts, its exit
status is propagated instead of being hidden. Memory and context hooks are best
effort and cannot block launch.

## Session control plane

The interactive menu contains **Session Control**, with direct and orchestrated
choices for both official clients, session listing, monitoring and safe stop. The
same operations are available to automation:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode orchestrated
ivoai session list --json
ivoai session show --json <session-id>
ivoai session stop <session-id>
ivoai monitor --watch
```

Direct sessions add metadata and monitoring but do not initialize Ruflo.
Orchestrated sessions require a verified safe Ruflo profile, initialize and verify a
real swarm, register the primary, and inject the local `ivoai-orchestrator` MCP. The
MCP delegates bounded tasks to official Codex/Claude non-interactive modes. The
default is two concurrent workers and the hard maximum is three.

Session JSON is private XDG state and contains no prompt, response or credential.
Model output is labelled `runtime_verified`, `argument`, `configured`, or `unknown`;
the last value is intentionally used rather than guessing. See
[Session control and orchestration](orchestration.md).

## Project identity

ivoai is host-first. Outside a project, memory uses a stable normalized host identity
instead of deriving a project from every working directory. `ivoai project init`
creates an explicit local marker inside a Git repository and overrides the host
identity for that tree.
