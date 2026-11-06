# Managed OpenCode removal and native migration

The current Core no longer installs, repairs, discovers, probes or executes
OpenCode. `ivoai auto` now aliases the managed Codex frontend and the same Core
admission/orchestration path. `ivoai opencode`, `--direct`, the setup flag
`--legacy-opencode`, OpenCode planners/session starts and frontend switches return
`OPENCODE_MANAGED_DEPRECATED`. The refusal precedes filesystem/setup/lease/provider
effects. A retained legacy session/checkpoint receives
`OPENCODE_MIGRATION_REQUIRED` instead of silently replaying or rerouting its plan.

Default setup and updater catalogs exclude OpenCode. Even an injected legacy
installer spec or old managed-build linker metadata cannot reinstall it. Doctor
exposes existing metadata without binary checks/probes and advertises no start
capability. The administrative catalog retains old action IDs as migration
information, with no launch command. The terminal menu shows the removed entries
disabled. Config/state/quota/session wire types remain readable; the shared
`opencodebridge` package name does not introduce another scheduler.

Existing user binaries, configuration, authentication, histories, supply-chain
objects and old profiles are preserved. No cleanup/uninstall of the retired
component is part of this migration. Published v0.16.0 source/assets remain
immutable. Rollback must use the original verified previous Core/profile and
its original asset contract; do not rebuild a historical tag or substitute the
new bundle. New request-journal and protected-evidence namespaces retain their
existing rollback isolation. Retained legacy history can be inspected; new native
execution requires a new admitted session and reviewed context.

AUTO execution tests now inject the native Codex frontend and submit through the
same Bridge/Core contract, while retaining assertions for authorization,
approved quota failover, shared knowledge, working-tree preservation and no
duplicate executor launch. Obsolete tests that expected OpenCode execution now
assert explicit denial and preserved data. Historical archive/metadata parsing
contracts remain tested independently; their success is not counted as support
for executing the retired frontend. Native protocol/TUI/runtime gates replace
legacy frontend publication jobs, with each gate reported under its actual scope.

Offline reapplication of the optional Headroom profile accepts the explicit
process-local setting `UV_OFFLINE=1`. Only that literal opt-in crosses the clean
UV installer environment; index overrides, credentials, Python paths and other
parent settings remain excluded. The reviewed wheel hash, dependency lock,
Python version, cutoff, `--force` and `--no-build` are unchanged. Required Python
and wheel caches must already exist: a missing cache is an installation failure
and never publishes a launcher. This does not turn bootstrap downloads into an
offline installer or skip a previously installed compression provider.

The isolated v0.16.0 → candidate v0.17.0 → v0.16.0 → candidate v0.17.0
qualification initially failed on the last Headroom installation (UV exit 2),
after the first upgrade and rollback succeeded. That failed receipt is retained.
A separate real-UV experiment reproduced a network fetch for the cached pinned
wheel with networking denied; the same command succeeded in explicit offline
mode. This establishes why the network-isolated qualification must select
offline mode, without claiming the original unrecorded UV stderr proves the
same cause. The production installer also passed the real official-UV empty
offline-cache negative, publishing no launcher. Final integrated upgrade
qualification remains required before stable publication.
