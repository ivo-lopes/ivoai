The final fixture source is identified by SHA-256 in controlled-load-result.json.
Only internal/codexfrontend/facade_test.go changes executable test behavior; no
production runtime changes. Readiness requires two atomic FIFO messages from
the background writer and its parent, followed by PID/starttime verification.
Setup has an explicit 10-second budget for scheduling/filesystem contention;
all existing Close, writer termination and post-trigger assertions retain their
original deadlines. Cleanup uses a validated Linux pidfd, with no raw-PID signal
fallback. The test requires Linux pidfd support; unsupported setup fails.

Final source: six focused race repetitions PASS, then six further repetitions
PASS under one CPU hash worker and one bounded 1 MiB overwrite/fsync worker.
Both load processes were alive throughout; all twelve live/closed pairs ran,
with no skips. The host was shared, so this is a robustness check, not an isolated
performance or provider benchmark. No provider inference or credentials used.

The parent's initial full-race readiness failure remains preserved separately
in composed-final-full-race.log. An earlier ten-repetition FIFO-only run passed,
but predates the pidfd cleanup addition and is not counted as final-source
coverage. A preliminary load invocation began before the test binary compilation
finished and failed with FileNotFoundError; no test process ran in that attempt.
The completed final-source load run above starts only after compilation.

Reproduce the focused check:

```sh
GOMAXPROCS=2 GOWORK=off GOENV=off go test -race ./internal/codexfrontend -run '^TestNativeCloseStopsBackgroundDirectoryWriters$' -count=6 -timeout=90s -v
```
