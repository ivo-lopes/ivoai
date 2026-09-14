# Team development — current operating model

IVOAI supports individual clients using shared purpose-scoped Memory/Context.
It does **not** yet coordinate distributed human writers or lock work across
hosts. IVOAI-174 tracks that future work; it is not part of v0.10.4.

## One developer, one client

Install IVOAI separately for each developer. Enroll each client individually on
the Voicecorp server with only the scopes that developer needs. Keep each
developer's HOME/XDG directories, client identity, secret store and provider
login separate. Never share enrollment credentials, client tokens, Codex/Claude
auth, OpenCode auth or provider quota. Server knowledge can be shared by purpose;
provider-native transcripts and authentication remain personal.

Use individual Git clones. Assign the main task in Plane, create a feature
branch per task/developer, and integrate through a reviewed PR. Do not work on
the same task or overlapping paths without coordination. Plane is the task
ownership authority; a running IVOAI session is not a distributed task claim.

## Project identity across clones

Without a project marker, identity falls back to the local host. Independently
running `ivoai project init` in two clones derives different project IDs from
their absolute Git roots, even with the same origin. This is a known GAP.

For a shared project scope, a maintainer should initialize and review one
`.ivoai.toml` marker and distribute that same non-secret marker through Git.
Existing valid markers are preserved by `project init`. Check that teammates
have the same reviewed ID before relying on project-scoped knowledge. Do not
copy secret stores or native session databases to align project identity.
Changing an established project's marker does not migrate its historical data;
coordinate any such change rather than silently replacing it.

## Local state and parallel work

Session records, provider mappings, checkpoints, quota observations and leases
are local to each client's XDG state. `flock` prevents concurrent ownership on
the same filesystem; it is not a distributed lock. Worktrees isolate workers
inside an orchestration. They do not prevent two humans on different hosts from
editing the same paths or pushing conflicting branches.

Before integration, fetch/revalidate the target branch and resolve conflicts in
Git. Use Git + Plane + shared Memory/Context + a bounded handoff brief to transfer
work between humans. Do not exchange complete transcripts, private reasoning,
auth files or worker prompts. Provider handoff is explicit and does not make
provider-native session IDs interchangeable.

## Hook health and offboarding

Use `ivoai memory hooks status`, `validate` or `repair`, or the Memory Hooks
actions in the IVOAI TUI. Repair touches only proven IVOAI-owned wiring and
preserves personal hooks. Degraded optional hooks mean memory integration is
unavailable, not that the base agent must stop. Never paste hook commands or
raw diagnostic output that may contain credentials into a ticket.

On offboarding, the server operator must revoke the enrolled **client** through
an authorized client-revocation operation and verify denied access. Revoking an
unused enrollment code is not revoking a previously enrolled client. Where no
client-revocation UI is exposed, coordinate with the server administrator;
do not edit credential files or share another developer's identity as a
workaround. Provider access is separately owned and revoked by that provider.

## Certification and governance

`TestTeamCurrentTwoClientIsolation` enrolls two clients against one TLS gateway,
uses independent HOME/XDG and synthetic provider state, checks scoped shared
knowledge, tests individual revocation, and demonstrates local-only leases.
`TestTeamCurrentProjectIdentityCrossCloneGap` demonstrates the cross-clone GAP
and preservation of an explicitly shared project marker. These are controlled
fixtures, not a claim of live multi-developer distributed coordination.

Read-only GitHub audit (2026-09-14): repository rulesets absent; the classic main
protection endpoint explicitly returned `Branch not protected`; CODEOWNERS and
PR templates absent. CI and release workflows exist. Merge, squash and rebase
are enabled; automatic branch deletion is disabled. No governance settings were
changed. Teams should agree on required review/checks and protected release
tags with repository administrators; workflow files alone do not enforce them.

IVOAI-174 retains stable logical project identity, developer provenance,
shared active-work registry, distributed leases/TTL/heartbeat, Plane-first
claims, path-overlap detection, branch/PR mapping, presence, artifact provenance,
remote-branch revalidation, team handoff, RBAC and offboarding UX as future gaps.
