# Provider-neutral executor turn contracts

IVOAI-192 incremental change, rebased on `2ba265cb` from PR #31.

Shared executor input, results, failures and traces lived in `opencodebridge`,
forcing backend consumers to use a frontend namespace. Their definitions now
belong to `internal/core/turn.go`, which imports only the Go standard library.
The change moves the contract boundary without changing execution behavior.

## Contracts and compatibility

Seven shared names move to Core: `ExecutorRequest`, `ExecutorResult`,
`ExecutorRunner`, `ExecutorFailure`, `FailureClass`, `ExecutionTrace` and
`ToolTrace`. The six types remain available as source-compatible aliases in
`opencodebridge`; its `FailureClass` function delegates to the Core implementation.

Fields, field types, JSON tags and omission behavior are preserved. Wrapped
failures retain pointer identity through `errors.As` using either package name,
the existing error strings and the `executor_failure` classification fallback.
No persistence schema or configuration migration is introduced.

Production consumers now use the Core names in five files:

- `internal/app/app.go`
- `internal/app/auto.go`
- `internal/app/auto_turn.go`
- `internal/agents/opencode_native.go`
- `internal/workers/adapter.go`

The worker adapter no longer imports `opencodebridge`. The Codex frontend still
uses its Bridge and model Catalog contracts; those frontend and transport
dependencies remain. This increment does not remove OpenCode, replace the
scheduler, add a primary-only path, change failover or alter tool grants,
approvals, sandboxing, authentication or execution policy.

## Executed validation

Local validation used Go 1.27.3 on Linux amd64:

- Full suite: `GOMAXPROCS=2 go test -p=2 -timeout=20m ./...` passed.
- Static checks: `GOMAXPROCS=2 go vet -p=2 ./...` passed.
- Race tests passed for the selected contract, CLI parsing, native failure,
  worker cancellation, sandbox admission and final DAG acceptance paths in
  `core`, `agents`, `workers`, `app`, `codexfrontend` and `opencodebridge`.
- New compatibility tests passed for cross-package runner implementations,
  wrapped error identity/classification and fixed legacy JSON documents covering
  all fields plus optional-field omission.
- `go list` confirmed Core imports only `context`, `errors`, `fmt`, `sort`,
  `strings` and `time`. The moved contract file itself uses only the first three.
- Formatting, diff checks and a production-consumer reference check passed.

The race command was:

```sh
GOMAXPROCS=2 go test -race -p=2 -timeout=10m \
  ./internal/core ./internal/agents ./internal/workers ./internal/app \
  ./internal/codexfrontend ./internal/opencodebridge \
  -run 'TestTurn|TestCLIRunner|TestNativeFailoverNeverReplaysToolEffects|TestOpenCodeWorker|TestAutoFinalResponse|TestNativeAdmission|TestNativeIntake|TestCapability|TestMatrix|TestComponent' \
  -count=1
```

Opt-in live client tests were not enabled for this refactor. These results do
not establish additional client parity or whole-session token/latency savings.
No release or production deployment is part of this increment. Rollback is a
code revert; no stored-data conversion is required.

## Follow-up: turn-local completion and resource lifetime

Automatic turn preparation now returns a `preparedAutoTurn` value containing its
executor runner, release callback and completion validator. `autoTurnRunner.Run`
keeps this value local to one execution. The existing production DAG validator
is created for that prepared turn instead of stored on the shared runner.

Release runs after preparation, execution, validation or final-emitter failure
when a callback has been acquired. The production preparation paths continue
closing their own partial resources before returning an empty failed descriptor.
Final output remains buffered until validation passes, with the existing 1 MiB
bound and missing-final failure class. Existing injected passthrough fixtures keep
their prior streaming behavior.

This prepares a safer boundary for later adaptive decisions. It does not enable
execution without a DAG, change the default completion policy, or alter executor
arguments, model selection, scheduler, grants, approvals or sandbox settings.

Additional local validation passed:

- `go test ./internal/app -count=1 -timeout=20m`: full App package suite.
- Focused lifecycle tests cover preparation errors with an acquired lease,
  executor/validation/emitter errors, missing output, the exact 1 MiB boundary and
  over-limit rejection. Concurrent turns retain different validators and release
  callbacks; a rejected turn cannot inherit another turn's acceptance or emit.
- Production scoped-runner tests retain `DAG_INCOMPLETE` and
  `ACCEPTANCE_INCOMPLETE` failures, and permit synthesis only after completed
  task metadata.
- App race tests passed for the new preparation/lifecycle tests, existing final
  acceptance tests and primary grant isolation tests.

After rebasing onto the audited dependency/profile updates in PR #31, the full
normal Go suite, focused contract/lifecycle/native-admission race tests and full
vet passed again. No opt-in live client tests were enabled for this revalidation.
The release verifier also runs the reviewed Zod runtime/type fixtures already
required by the parent PR's CI, keeping those dependency gates aligned.
