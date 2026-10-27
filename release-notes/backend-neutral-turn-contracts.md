# Provider-neutral executor turn contracts

IVOAI-192 incremental change, based on `9874dbdf`.

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
