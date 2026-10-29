# Compression fixture PID publication

PR57 CI run `40784670844`, unit-test job `123154759458`, failed at `runtime_test.go:590`: `invalid PID marker "": strconv.Atoi: parsing "": invalid syntax`. The failure is preserved in the [evidence receipt](evidence/fixture-compression-pid-publication/validation.json). The existing fixture redirected output to the final PID filename before `printf` wrote its content, while `waitForFile` checked existence only. Its wrapper-ready marker did not synchronize the separate agent's PID write.

The fixture now writes each PID to a private adjacent temporary filename and publishes it with rename after the write succeeds. The fake agent installs its signal trap before publishing its PID. Test cleanup always cancels and joins the runtime before restoring environment variables and removing fixture files, including failures during readiness/PID validation. Production `runtime.go`, permissions, compression policy and client behavior are unchanged. Existing PID parsing, process-absence assertions, one-second readiness and three-second cancellation deadlines remain intact.

One predeclared offline causal experiment delayed the agent's PID write by 150ms after the redirection opened its target. The old fixture failed with the exact CI error; the corrected fixture passed with the same intervention applied to its temporary target. Each arm ran once. The diagnostic used owned synthetic process groups and a subreaper; neither arm left a process in its owned group. The old failed test required diagnostic reaping of two adopted fixture children, whereas the corrected arm completed cleanup itself.

The full `internal/agents` race check passed: 37 test/subtest PASS events, five explicit opt-in SKIPs, and the affected test PASS. Package vet passed. No test was weakened or retried until green, no production file changed, and no official client, authentication, model or provider was invoked. Full-source hosted CI is still required for the eventual integrated head; this local evidence does not convert the historical failed CI run into success.

## Combined managed-scope validation

The fixture fix was cherry-picked onto managed configuration guard source
`55ec7ab98683bf0c8784b93baebc966f2910772b`. The combined source is `c7ad71bf`.
Only `internal/agents/runtime_test.go` changes among Go files relative to that
base; production runtime, modules, pins and workflows remain unchanged.

The [separate combined receipt](evidence/fixture-compression-pid-publication/combined-55ec/validation.json)
records 59 Go packages passing without cached results, three additional packages
without tests, all-package vet and the complete agents race suite. Those checks
passed with captured exit0. They do not replace the original CI failure or the
single OLD/NEW causal control above. Fresh final-head CI remains pending; no
official client, model, authentication, production release or deployment ran.
