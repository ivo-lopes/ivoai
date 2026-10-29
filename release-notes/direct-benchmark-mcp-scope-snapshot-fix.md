# Direct benchmark inventory scope snapshot correction

The unwired test-only helper now copies its environment and global argument
slices **before** building the inventory command. Its successful result retains
that exclusive snapshot, so later edits by the caller cannot change the scope of
the denied exec command. The earlier command-level copies did not protect the
result's aliases to the caller's scope.

The [receipt](evidence/direct-benchmark-mcp-scope-snapshot-fix/validation.json)
preserves a source-pinned OLD FAIL in both during-inventory and after-inventory
mutation cases. The same regression passes after the two snapshot copies. The
candidate and baseline helper race checks pass, as do candidate vet and existing
catalog/measurement contracts. Fresh baseline/candidate App builds and matching
discovery4 were completed; the build/preparation pipeline was already running when the separate CLI parser
finding arrived and was observed complete at the next poll. These are source/build
compatibility checks, not proof of an official inventory contract. No live
benchmark invocation, prompt, observer, driver, production API or runtime changed.

The proposed root-level --ignore-user-config inventory is now **known
incompatible** with the reviewed official CLI628e. A separate root-owned offline
help check observed exec-only support and an unexpected-argument exit2 for
root ignore+mcp help. Those were three metadata-only helps with zero inventories,
tasks, auth/status/model calls. This agent ran no official CLI. The original fc18
package remains intact and NOT_RUN; the new clone is retired without a new
qualifying freeze or any official execution.

A next design needs explicit review of a common configuration-scope change in
both arms, such as including user configuration with the same scoped inventory
and exec. No silent flag removal, guessed alias, inventory skip, personal setting
change or managed guard bypass is authorized. Cloud/context-policy isolation,
real cohort admission and economics remain unverified; concurrent_distinct_projects
remains UNSUPPORTED and the global economic gate INCOMPLETE.
