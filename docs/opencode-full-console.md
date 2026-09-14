# OpenCode Full Console

`ivoai opencode` is a frontend for the same IVOAI orchestration core used by
`ivoai codex`. It is not a scheduler, provider registry or session database.
`ivoai auto` remains a deprecated alias. Direct sessions remain explicit.

## Operational view

Use `/ivoai` to open the scrollable console; Escape returns to the composer.
The view refreshes approximately once per second, without overlapping requests.
It shows prompt readiness, plan/DAG dependencies, task and worker states,
effective provider/model/reasoning, quota availability, isolated/integrated
worktrees, exact task MCP tool metadata, selected Skills/Ponytail, purpose-scoped
knowledge health and owned memory-hook health. Unknown or stale telemetry is
not rendered as healthy or available.

The core supplies one bounded snapshot plus its existing typed event ring.
Event sequence numbers survive session restart and make gaps in the retained
128-event window visible. This is a bounded recent-event view, not an audit log
or transcript replay service. The console does not persist another copy.
Only metadata is projected: no prompt, provider transcript, worker result,
private reasoning, checkpoint body, credential, environment or raw stderr.
Worktree integration is reported only after the integration operation succeeds,
not merely because a worker produced a commit.

Plan and material routing/MCP decisions still use native confirmation dialogs.
Full tool permission mode does not approve plans or expand MCP grants.
Cancellation leaves existing safe ownership rules in force.

## Session and model controls

`/ivoai-sessions` lists logical sessions in the current project and can resume
eligible mapped OpenCode conversations. Selection is confirmed, refuses a
running turn, preserves IVOAI/native identity and never replays the last prompt.
The native OpenCode conversation remains the history owner. Entries requiring
a provider handoff, missing frontend mapping or explicit mode transition direct
the user to Session Control in `ivoai`; they do not silently create a session.
The main IVOAI TUI continues to expose inspect/resume/recover/handoff/stop and
explicit Codex native adoption.

`/ivoai-models` shows the catalog observed for this session, its source and
supported reasoning options. Use OpenCode's native model/reasoning selector to
make an explicit primary choice. Actual turn admission revalidates eligibility
and quota; the session catalog is not a promise that a provider remains online.
Unavailable explicit models do not silently fall back. Worker routing remains
independent and uses the core's minimum-sufficient economic router.

## Automation profiles

Use the IVOAI command palette or `ivoai` → Orchestration Policies. Profiles edit
the existing `orchestration.auto` policy and take effect on the next session;
running workers are unchanged. CLI equivalent:

```sh
ivoai config set orchestration.auto.automation_profile economic
```

| Profile | Existing policy changes |
| --- | --- |
| economic | Two-worker cap; no parallel writes; progressive escalation |
| balanced | Automatic worker cap; isolated parallel writes; standard routing weights |
| quality | Four-worker cap; isolated parallel writes; more risk/verification weight in routing |
| custom | Preserve settings; edit individual policies in the same menu |

All presets keep the 10% low-quota threshold, quota telemetry enabled and plan
approval required. Explicit provider/model/effort and knowledge-source choices
are preserved. No model ID is invented or embedded in a preset. Quality changes
routing emphasis, not acceptance criteria or a separate verification engine.
Individual policy edits mark the selection custom; no second configuration
stack or destructive migration is used.
The custom menu includes verification routing weight (0–100); this changes
routing emphasis only, never disables required validation or acceptance.

## Hooks and safety

The command palette offers validate and repair for owned memory hooks. Actions
require confirmation. Repair preserves foreign hooks and never executes a
lifecycle event as a test. Failure/degradation produces an error rather than a
false success. Detailed bounded status remains available through
`ivoai memory hooks status` and `ivoai doctor`.

Control endpoints share the authenticated loopback bridge. They accept only
fixed actions, not shell commands, arbitrary configuration keys or credentials.
Session changes serialize against turn execution. See
[conversation continuity](conversation-continuity.md) and
[team development](team-development.md) for portability and local-state limits.
