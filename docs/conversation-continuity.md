# Conversation continuity

## Find and reopen a conversation

The IVOAI session ID identifies the logical conversation. It is distinct from the
frontend conversation ID, provider thread/session ID, worker ID and turn ID.
Restarting a frontend does not create another primary for an already active
IVOAI session.

```sh
ivoai session list --json
ivoai session show --json <id>
ivoai session resume <id>
ivoai session resume <id> --frontend opencode
ivoai session recover <id>
ivoai session handoff <id> --to claude --confirm
```

`show --json` retains the existing one-element-array format. Human-readable lists
include mode, frontend, primary, state, update time and resumability. In `ivoai`,
open **Session Control → Conversations — Inspect / Resume**. The same domain
provides inspect, native resume, interrupted-turn recovery, frontend selection,
explicit provider handoff and safe stop. No native ID needs to be copied manually.

## Native Codex `/resume`

Inside `ivoai codex`, use the official `/resume` picker. It can list, search,
preview, cancel and select managed conversations in the current project. Selecting
a conversation switches the logical IVOAI binding; it does not launch a second
orchestrator. Unmanaged Codex threads are not silently imported.

The pinned native picker opens an additional authenticated App Server connection.
IVOAI supports bounded discovery connections with per-client request-ID remapping,
responses routed to their origin, and primary-only turn notifications. Closing a
picker does not close the main connection. Discovery clients cannot start turns,
execute tools or answer the primary's approval questions.

On Codex 0.154.0, passing sandbox/approval overrides to the remote TUI also
prevented selecting a conversation. Those enforcement settings now stay on the
IVOAI-owned App Server and sanitized protocol requests, not on the TUI's resume
flags. Removing the client-side duplicates does not weaken the Prompt Gate or
server sandbox. Both 0.153.4 and 0.154.0 are covered by native protocol tests.

Codex owns its native history in a private, persistent, project-scoped managed
home. IVOAI does not copy personal Codex configuration or account authentication
into that home. The official executor owns its own provider history and login;
IVOAI stores opaque mappings and an account-identity fingerprint, not credentials.
Native previews are transient protocol traffic, not continuity-journal entries.
The bridge never falls back to an unverified account mapping.

New turns use current model/effort selection and re-evaluate knowledge routing.
Previously discussed information remains part of the provider-native conversation;
resuming is not a way to erase that history. Start a separate IVOAI conversation
when an organizational context must remain separate. Historical sources do not
automatically receive new queries or MCP grants.

## Resume, recovery and handoff are different

- **Resume** reopens a native conversation and waits for new input. It does not
  repeat the last completed turn. Codex and Claude direct sessions use their
  official native resume arguments; managed OpenCode attaches to its native
  session. Each new orchestrated prompt still passes the Prompt Quality Gate.
- **Frontend switch** changes presentation, not the provider or logical identity.
  IVOAI retains both frontend mappings and reuses a verified provider mapping.
  A new presentation thread may be necessary; it is not a transcript migration.
  Switching to the Codex frontend requires a Codex primary. Use a confirmed
  provider handoff first if the primary is Claude.
- **Recover** reconciles the host-owned approved DAG checkpoint, repository
  identity/HEAD, task states, owned worktrees and process start markers. Completed
  tasks remain completed. Only remaining tasks are proposed again, using current
  runtime models, quota, Skills and newly issued MCP access. A new plan approval
  is required even when normal execution is configured as immediate.
- **Handoff** explicitly transfers a bounded brief into a new destination
  provider conversation. `--confirm` authorizes that transfer; it is separate from
  execution-plan approval. Lineage records the source session/provider,
  destination provider and confirmation time. Direct stays direct; orchestrated
  stays orchestrated. No transcript, tool grant or provider credential is copied.

Recovery refuses `AMBIGUOUS_PREVIOUS_EXECUTION` when a writer has no verified
collected commit or a previously attempted external tool operation has uncertain
completion. Do not retry a deploy, push, release, update or delete blindly.
Preserve the worktree, inspect the external result, and explicitly reconcile the
remaining work. Changed repository identity/HEAD and still-running owned workers
also stop recovery. Collected work is integrated through the existing isolated
worktree integration path; conflicts are not auto-resolved.

## Persistence and privacy

The existing private session store remains the registry. Additive frontend
mappings and lineage accompany a bounded checkpoint under
`sessions/checkpoints/<id>.json` (maximum 32 KiB). A handoff brief is at most 8 KiB.
Allowed content is a concise objective, constraints, acceptance, decisions,
completed/outstanding work, blockers and next step. The recovery portion is the
host's bounded approved task graph, not expanded worker prompts or Skill bodies.

No complete user/worker/provider transcript, chain of thought, authentication,
environment dump or raw tool response belongs in that journal. Secret-shaped
fields and terminal controls fail validation. Files are private and atomically
written; reads and session locks reject symlinks. Ephemeral gateway grants are
not restored. Kernel-held session leases prevent concurrent resume, and recorded
PID/start pairs prevent signaling unrelated reused PIDs.

The v0.10.2 session schema stays readable. Legacy runtime checkpoints are read
and promoted without resetting configuration; existing profiles, auth ownership,
MCP registry, Skills/Ponytail and policies are unchanged. Rollback binaries ignore
the additive private checkpoint directory. They cannot provide new continuity
operations, but reapplying v0.10.3 restores access to retained state.

## Limits and troubleshooting

Direct Codex/OpenCode mapping discovery is bounded and records a newly created
native session only when it is unambiguous. If several conversations were created
concurrently or no native ID was observed, IVOAI reports an unavailable mapping
instead of choosing the latest arbitrarily. Claude live use requires its existing
official authentication; absence of it does not remove Claude support.

A v0.10.2 Codex façade used an ephemeral home; already-deleted native histories
cannot be reconstructed from metadata. Such sessions require a retained safe
checkpoint/handoff, not a fabricated native resume. Likewise, recovery is not
available for a plan that was never approved or lacks a valid host checkpoint.

The former error `Failed to start TUI session picker: failed to connect to remote
app server` was caused by the façade rejecting the picker's second connection
with HTTP 409. Do not disable `/resume` or switch to direct mode as a fix. Update
IVOAI and restart its managed frontend. If a session is reported active, close or
stop its owned frontend before resuming it; do not delete lock or session files.

OpenCode Full Console, OpenViking and advanced parallel orchestration are not part
of this release.
