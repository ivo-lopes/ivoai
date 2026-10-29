# Direct benchmark MCP scope: current test-only preparation

The standalone helper now explicitly includes user configuration under
USER_CONFIG_INCLUDED_SHARED_SCOPE. Its common prefix is identical for inventory
and exec in baseline/candidate and contains no ignore-user-config option.
Discovery reports direct_mcp_helper_configuration_scope and
**direct_mcp_helper_connected=false**. These describe a declared, disconnected
helper; they do not relabel the still-legacy live benchmark invocation or admit a
real cohort. Prompt, oracles, client observer/lifecycle, metrics and production
runtime are unchanged. Managed ignore rejection remains intact.

The helper snapshots global arguments and environment before inventory, uses the
same actual cwd, and requires the successful bounded inventory result to build
its denied command. Stdout256KiB/stderr64KiB/names128 are hard-bounded; malformed,
ambiguous and unsafe names fail closed. Exact per-server enabled=false overrides
precede fixed host restrictions. No empty-table clear, guessed name provenance,
projection re-enable input or raw inventory/diagnostic measurement output exists.

The [current receipt](evidence/direct-benchmark-mcp-user-config-scope/validation.json)
records candidate/baseline source-only helper race checks, candidate vet,
71Python benchmark tests, fresh App builds and identical discovery4. Python
rejects missing/unknown scope enums or a substituted connected=true declaration
before synthetic client attempts. Baseline3ff retains241 original non-testGo/module
files and candidate6cc retains286. No new production API/dependency is projected.
Future comparisons require both arms rebuilt, discovery and conditions re-frozen.

The root-reviewed exact new offline pair was executed **once**, after the frozen
preparation controls. A inventoried both synthetic user/system entries and
observed both startup/init/list and actual fixture-bound CodeMode ALL_TOOLS
metadata. B repeated the same inventory and added only the two exact denies:
zero observed fixture startup/init/list and an empty MCP catalog. Each arm used
one inventory and one exec, with two accepted fakeHTTP requests and zero nested
MCP calls. A5.226s/B5.202s are fixture durations, not economic comparisons. Actual
inventory/exec, App and namespace exit0; no individual MCP/companion Wait claim.

The [execution receipt](evidence/direct-benchmark-mcp-user-config-scope/execution-validation.json)
binds all40input pins unchanged,171private artifacts and historical artifact
checks. Fresh cases were exact/owned/private, without auth files or symlinks.
The25pure protocol cases and Go argv/Dir/Env preparation control remain separately
labelled as preparation. The original static PREPARED receipt is not rewritten as
execution. Root authorization was specific and consumed; there was no retuning,
alias, retry or extra call. This proves exclusion for these two frozen synthetic
layers only. No real auth/status/model/provider/cohort call was made.

Historical epochs remain unchanged:

- ed79 preserves the initial promoted WriteString failure and named-buffer fix;
  its first root-ignore proposal fc18 remains NOT_RUN with all34pins intact.
- 476 preserves the inventory-result slice alias OLD FAIL during/after inventory,
  then the exclusive snapshot fix. Its source/build checks do not qualify the
  old official inventory syntax.
- Three independent root-owned help-only observations prove CLI628e lists ignore
  under exec, not root, and rejects root ignore+mcp help with exit2. No inventory
  or task was run by those helps. The known-incompatible plan was retired, never
  executed, aliased, retuned or relabelled as an isolation success.

The [provider-route proposal](direct-benchmark-provider-route-proposal.md) records
why CHATGPT authentication alone cannot admit a normal-config cohort and why some
apparently read-only official APIs/forced-login controls have side effects. That
guard is NOT_IMPLEMENTED. Real cohorts remain BLOCKED; cloud/context policies are
unverified, concurrent_distinct_projects remains UNSUPPORTED and the global
economic gate INCOMPLETE. No parity, savings or release certification is claimed.
Reverting this preparation needs no credential, configuration or data migration.
