# Managed TUI fixture waits for command readiness

The PR51 AMD64 job [40761236991 / 123073413304](https://github.com/ivo-lopes/ivoai/actions/runs/40761236991/job/123073413304)
failed `TestLiveManagedOpenCodeResizeKeyboard` after its first resize to 100,
then missed `Permissions:` at the next width of 60. That adverse result remains
valid; the original test did not retain its raw transcript. Its actual CI
scheduling is unknown.

At source `96cf9c7a44c84c8ad5bf854b394f5ffb3741d93a`, the complete bridge, TUI
assets, test, managed patches and build/test scripts are unchanged from main
`a1ccf4ca` and the published v0.14.0 source `6d99e320`. An isolated diagnostic
used the checksum-verified published `.3` AMD64 archive and synthetic local
Runner/clipboard fixtures, without authentication, personal configuration or
provider inference. It does not establish the identity of the failed CI ELF.

A traced ordinary run passed. Pausing the owned TUI across raw Escape and the
next slash also passed, while exposing that an old panel redraw can satisfy
the test's later label checks. Neither result explains away the CI failure.
Pausing only before the second slash until Enter had queued reproduced the
same missing-panel failure: the autocomplete menu appeared after Enter, without
opening the panel.

An independent 500 ms timed pause then reproduced **old FAIL / new PASS** with
identical fixture bytes and fault conditions. The change waits for the command's
unique `Executors, quotas` description in the fresh output suffix before Enter.
The existing 12-second state deadline, 90-second fixture context, two-minute test
deadline, panel assertions and all four keyboard/clipboard scenarios remain.
This fixes the demonstrated automation ordering defect; it does not attest
that the original CI failure had that exact scheduler history or change the
product keyboard protocol.

All four actual pinned keyboard/clipboard scenarios passed, as did the affected
Go race suite, vet, formatting and diff checks. The [receipt](evidence/managed-tui-menu-readiness/validation.json)
preserves ordinary/adversarial outcomes, source identities and bounded raw-trace
hashes. [Reproduction sources](evidence/managed-tui-menu-readiness/README.md) are
diagnostic `.go.txt` files, not an added Go package. Raw transcripts remain
private. No `.3` pin, runtime source, binary, release, tag or asset changed.
The latest combined PR51 CI remains a separate gate.
