# Direct benchmark MCP scope: test-only preparation

The standalone economic benchmark has a new, **unwired** test-only inventory and
command builder. Its source-only tests require one frozen argument, working
directory and explicitly copied environment scope for inventory and execution.
Successful inventory is required before constructing the denied command. The
reader bounds stdout at256KiB, stderr at64KiB and names at128, rejects malformed,
ambiguous or unsafe names, and places exact per-server enabled=false overrides
before the fixed host restrictions. Inventory contents and raw diagnostics are
never emitted in measurement rows. No empty-table clear or managed ABI bypass is
introduced; managed --ignore-user-config rejection is unchanged.

This is preparation, not demonstrated official MCP isolation. The current live
invocation, prompts, observer lifecycle, metrics, production runtime and driver
are unchanged. A separately frozen private proposal tests whether the reviewed
CLI accepts --ignore-user-config for inventory and execution with the same scope.
A must show a system-only configured/catalog source, then B applies the exact
system-name denial and requires no startup/catalog. An incomplete A stops B.
No official CLI, inventory, MCP, provider, namespace, auth/status/model or cohort
execution was made for this change. Its explicit future budget is at most two
inventories and two exec calls, subject to root review and authorization.

The [receipt](evidence/direct-benchmark-mcp-scope-preparation/validation.json)
binds candidate/baseline helper race tests, candidate vet, preserved existing
measurement/catalog contracts, fresh App builds and matching discovery schema4.
Baseline3ff retains all241 original non-testGo/module files; candidate6cc retains
all286. The harness differs only by the added helper/tests and imports, with
HarnessSHA70ccfa8a3728282a7c168bbb567583516d8a2f999918187931e78ba9334fdd1d.
The baseline already has the same inline-table ProjectTrustArgs; the initial
private audit's caveat about that implementation was corrected without changing
its original document. Fresh private Go source-only preparation compares actual
helper argv/Dir/Env to the proposed plan;21 pure Python protocol cases pass.

The first bounded-writer test failed because embedding bytes.Buffer promoted its
WriteString method and bypassed the custom Write method. That adverse source/log
is preserved. A private named buffer fixes the defect; final helper tests pass
with race detection in both common projections. Earlier proof epochs and labs
remain unchanged. No full App race, hosted CI, native ARM or hardware noAVX
certification is claimed here.

Cloud/context-policy isolation remains unverified. Real cohorts remain blocked,
concurrent_distinct_projects remains UNSUPPORTED and the global economic gate
remains INCOMPLETE. A future common harness change requires both arms rebuilt,
discovery repeated and source/conditions frozen. Reverting this standalone
preparation needs no credential, configuration, data or runtime migration.
