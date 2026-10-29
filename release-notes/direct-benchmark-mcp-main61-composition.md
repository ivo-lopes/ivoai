# Direct benchmark preparation composed with reviewed main61

Source `58639394dca7b38d2b61a918cbe1e594ab126326` merges the disconnected direct MCP helper preparation `496523c6403464cb751542c168dbf7fb39d8c326` with actual main `ccc48a055ae3099a2027cfc8f31210c172baf247`. The merge had no conflicts and required no Go or Python edits. It includes the reviewed PR61 invocation-context implementation and PR62 deterministic flight/deadline fixture, rather than running the older A6 source as if it were current main.

All282 existing main non-test Go files and both root module manifests are byte-identical to main. The three additional non-test Go files remain benchmark-fixture helpers imported only by test sources. The reviewed direct helper and two catalog scripts remain byte-identical to e425. This composition does not connect the helper to live invocations, change a backend contract, add a dependency or create a second scheduler.

## Checks on this composition

A frozen3704-path source manifest was checked before and after each command. Commands ran once with an explicit environment, private HOME, offline module settings, GOMAXPROCS2 and no official-client opt-ins.

| Gate | Result |
|---|---|
| Full Go suite, `-count=1 -json ./...` |59 packages PASS,3 packages without tests;2111 passing test events including subtests,1190 top-level passes,71 explicit opt-in skips;123.828s walltime |
| Full benchmark Python suites |71 tests PASS;28.791s command walltime |
| `go vet ./...` |PASS;4.129s walltime |

Skips are not successful integrations. Shared-host walltimes are observations, not comparative performance results. The preceding A6 focused race/discovery checks remain bound to their own source and are not rescored as a new race run. No paired comparative binaries were rebuilt, and no real cohort was started.

All1098 unique release-note records from both parents remain byte-identical, including adverse historical CI, fixture and economic results. The [validation receipt](evidence/direct-benchmark-mcp-main61-composition/validation.json), SHA256 `fa701f552620da26d5662f1251191dca819dcf2ce4a26adc56d44a827df82fd1`, records the source freeze, commands and counts. The original full Go JSON log is retained as gzip with its uncompressed digest. This report and its evidence directory are the only finishing additions.

## Admission remains incomplete

The helper still declares `USER_CONFIG_INCLUDED_SHARED_SCOPE` and `direct_mcp_helper_connected=false`. These are discovery declarations, not proof of the scope used by existing live benchmarks or a provider-route admission decision. The separate route guard and its transport remain outside this source. A real experiment requires reviewed route admission, actual wiring, equivalent arm configuration, both-arm builds/discovery/freeze and bounded execution. No paid fallback, automatic retry or mutable replay is introduced.

No official client, help/status/auth/config/account RPC, real model/provider, installer, Node build, personal configuration change or new download ran in this epoch. There is no new token, cache, quota or economic comparison. Global economic acceptance remains INCOMPLETE; matrix readiness remains9 supported contracts/5 unsupported/2 blocked. This is a draft benchmark prerequisite, not a release or proof of savings. Hosted CI is a separate gate on the eventual PR head; old failed CI is preserved.
