# Client

## Interactive menu

**Launch Orchestration** offers **Codex Orchestrated** and explicit
**Codex Direct** / **Claude Direct** sessions. OpenCode entries are disabled
migration notices, not launch options. The same Core owns managed admission,
permissions and scheduling. See [frontends](orchestrated-frontends.md).

**Connections → IVOAI Servers** manages independent profiles. Add a server,
then select its alias to test, enable/disable, edit, re-enroll or remove it.
See [Connections](connections.md) for the complete flow.


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
Updates are explicit through `ivoai update`. The updater verifies the candidate,
creates a private snapshot of IVOAI-owned files, applies ordered target-owned
migrations, promotes the binary, and runs Doctor. Failure restores both the binary
and compatible config/state/ownership. Use `ivoai update --dry-run` for a non-committing
compatibility plan; it still executes the checksum-verified candidate's bounded
preflight probes. Use `ivoai update --rollback` for the last transaction. See the
[production compatibility contract](production-compatibility.md).

The healthy disconnected state is:

```text
ivoai          ready
Codex          installed / not connected
Claude Code    installed / not connected
OpenCode       removed / retained metadata only
Headroom       ready
Compression    default caveman / effective caveman
Caveman        installed / managed
ai-memory      installed / not connected
Ruflo          ready / provider execution disabled
Server         not-connected

Overall: READY — external connections pending
```

If reviewed compressor metadata or integrity is unavailable, compression reports
`default caveman / effective direct / fallback=caveman_unavailable`. This optional
fallback is distinct from missing required clients or a degraded Core setup.

## Agent launch

`ivoai codex` opens the official Codex terminal through IVOAI's managed façade;
`ivoai auto` is its compatibility alias. `ivoai codex --direct` explicitly uses
the direct client without managed turn orchestration. The adaptive Primary is
experimental and off by default: `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` enables
only the reviewed runtime/platform path during controlled certification. It does
not authorize arbitrary versions or platforms.

`ivoai claude` uses the official direct TUI. Managed headless Claude requires
`IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1` and `ivoai claude run --prompt ...`.
Antigravity's experimental ACP host requires `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1`,
`ivoai antigravity run --prompt ...` and the reviewed `agy` 1.5.2 binary on Linux
amd64. These headless hosts do not control the native TUIs. Claude inference
certification remains pending an authorized user account.

Managed OpenCode launch and explicit legacy setup return migration errors.
Existing user data and previous profiles remain intact for explicit rollback.

Codex and Claude sessions can select an enrolled knowledge purpose without changing
global agent configuration:

```sh
ivoai codex --knowledge-source mindsite
ivoai claude --knowledge-source voicecorp
ivoai auto --knowledge-source mindsite
```

With no `--knowledge-source`, every enabled connected source participates in bounded
read federation. Repeat `--knowledge-source`, or pass a comma-separated value, to
restrict a session to exactly that subset. Automatic federation never broadcasts
Memory writes; an ambiguous destination fails explicitly. The agent receives
only private loopback MCP endpoints and a short-lived local capability. Upstream
credentials stay inside ivoai. See [Multi-server knowledge sources](multi-server.md).

Codex and Claude request Caveman by default. An explicit `direct` or legacy
`headroom` override is preserved. If the selected provider is unavailable, unhealthy,
or incompatible during preflight, ivoai starts the official agent directly. Once a selected wrapper process starts, its exit
status is propagated instead of being hidden. Memory and context hooks are best
effort and cannot block launch.

When authoritative `ivoai-memory` or `ivoai-context` is active for the selected
session sources, the launcher deliberately starts the official client directly
regardless of whether Caveman or Headroom was requested. A lossy provider may
shorten exact custom-tool results before the model sees them,
including the end of an exact memory page. The launch prints this bypass and observed
session metadata reports the provider-neutral bypass. Headroom's component and
explicit setting remain available, but its managed Codex/Claude wrapper is
`unavailable_by_policy`: the pinned wrapper can modify client MCP configuration
and no safe exact-recovery alternative is verified. IVOAI uses Direct once,
before probing or running that wrapper, and preserves the chosen setting.

`ivoai opencode`, including `--direct`, is refused before executor dispatch.
`ivoai setup --legacy-opencode` also refuses installation. To use a separately
installed upstream OpenCode, run it independently of IVOAI; the managed launcher
never adopts it, changes its provider or starts a fallback OpenCode process.

Every ivoai-managed primary receives the same shared-knowledge contract. Any task
that requires research, fact-finding, current information, or external verification
uses the fixed source order `ivoai-memory` → read-only `ivoai-context` → web/external
sources. Both ivoai stages are attempted before the first external lookup, including
for apparently general or time-sensitive questions. Empty, unavailable, insufficient,
or stale internal results permit web research; self-contained tasks do not trigger
artificial lookups. An explicit request to remember information is written through
`memory_write_page` and verified with `memory_read_page`. Context is a connector-fed
RAG index, not a conversational write store, so an agent must not claim it wrote a
chat fact to Context.

The same process-scoped policy reaches Codex and Claude workers through the
existing Core contracts. Codex is the managed native frontend; no frontend owns
a second scheduler or memory store. Retained OpenCode wire types and history do
not restore execution, and retrieved text never becomes an instruction source.

Codex treats MCP tools without read-only annotations conservatively. For remote
IvoAI servers that are actually registered and enabled, the launcher adds
process-local `approve` overrides only for `memory_query`, `memory_read_page`, and
the four read-only Context tools. This keeps headless reads functional while memory
writes, deletion and unrelated MCP tools continue to follow the user's normal
approval policy. No absent MCP server is synthesized by these overrides.

OpenAI publishes `codex-code-mode-host` as a separate, versioned release asset.
IvoAI installs its reviewed archive beside managed Codex and verifies its SHA-256.
Codex's stable tool router fails closed when the companion is missing, including for
MCP calls, so ivoai refuses a toolless managed launch and directs the user to
`ivoai setup`. Externally managed Codex installations remain responsible for their
own version-matched companion.

## Session control plane

The interactive menu contains **Session Control** for native session inspection,
supported resume/recovery, confirmed handoff and safe stop. Direct Codex and
Claude starts remain explicit; managed native orchestration uses Codex. The
experimental Claude headless host is a separate command, not a Claude TUI mode.
Automation uses the same session operations:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode direct
ivoai session list --json
ivoai session show --json <session-id>
ivoai session resume <session-id>
ivoai session recover <session-id>
ivoai session handoff <session-id> --to claude --confirm
ivoai session stop <session-id>
ivoai monitor --watch
```

Direct sessions add metadata and monitoring without a DAG. Managed Codex uses
the existing Core planning protocol, plan approval and worker scheduler. With
the experimental adaptive flag enabled, eligible read-only turns can finish
without a DAG. Ruflo's provider-free coordination profile is not another
scheduler. Resume, recovery and handoff remain limited to eligible identities
and supported client modes; an unsupported managed Claude planner/handoff is
refused instead of opening a headless or alternate TUI. See
[Conversation continuity](conversation-continuity.md).

Session JSON is private XDG state and contains no prompt, response or credential.
Model output is labelled `runtime_verified`, `argument`, `configured`, or `unknown`;
the last value is intentionally used rather than guessing. See
[Session control and orchestration](orchestration.md).

Multiple primaries may run at once, including two Codex sessions plus one Claude
session. Each has an independent random session ID, PID/start marker and private
runtime directory. Session metadata and quota updates are file-locked and atomic;
stopping or cleaning one session targets only its recorded processes and runtime.
Memory is shared only within the explicitly selected source/purpose. Concurrent
Voicecorp and Mindsite sessions have separate loopback routers and capabilities,
while transient Ruflo state and the session-local orchestrator bridge remain
isolated per session. Session metadata stores selected aliases, never credentials.

## Automatic conversation mode

`ivoai codex` and `ivoai auto` open the same official native Codex frontend.
Core retains session ownership, provider selection and single-writer authority.
The adaptive Primary remains off by default until controlled certification and
rollout: `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` enables reviewed sessions only.
Natural prompts do not require formal acceptance headings. Under that flag,
eligible simple read-only work can finish in the Primary; complex work escalates
to the existing plan validation, approvals and worker scheduler. `--planner opencode` is refused; legacy history is not converted into new execution.

The Core owns DAG scheduling, isolated worker grants, quota/capability checks and
bounded continuity state. Ruflo's safe coordination profile stays provider-free;
it does not replace the scheduler or own durable memory. No worker or economic
benefit is presumed for a simple request. The global economic gate remains
incomplete. Run `ivoai monitor --watch` in a second terminal for the primary,
model provenance, worker state, quota source/freshness/reset and service health.
Unavailable fields remain `N/A / not exposed`; before a Claude response its quota
may read `awaiting first response`, and old observations remain `stale`.

Knowledge selection and local capability boundaries carry into workers and
supported handoffs. Unsupported client transitions are refused; this is not a
promise of transparent Codex↔Claude TUI failover. A handoff never expands the
source set or copies upstream credentials.

Running `ivoai connect chatgpt` or `ivoai connect claude` invalidates the selected
provider's reconstructible quota cache before official authentication and probes
afterward. No provider credential is copied or inspected by this cache reset.

See [Automatic orchestration](auto-orchestration.md),
[Automatic scheduler](auto-scheduler.md) and [Quota routing](quota-routing.md).

`ivoai status` uses bounded live checks for every Server profile and the Ruflo safe
profile. It reports alias, purpose, protocol, bounded features, credential
configured/not configured, redundancy group and priority without printing a secret.
Stored configuration is never labelled healthy by itself. One unreachable source
does not remove another; local Codex and Claude remain available. Headroom
installation/compatibility is reported separately from an interactive launch
validation.

## Project identity

ivoai is host-first. Outside a project, memory uses a stable normalized host identity
instead of deriving a project from every working directory. `ivoai project init`
creates an explicit local marker inside a Git repository and overrides the host
identity for that tree.
