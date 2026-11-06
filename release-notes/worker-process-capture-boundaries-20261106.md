# Worker process ownership and raw capture boundaries

Workers now reuse the owned process-group guard already applied to native
executors. Cancellation signals are retired before reaping the leader, preventing
a late signal from targeting a recycled process-group ID. Normal completion still
returns the actual child outcome; cancellation does not become a successful turn.

Raw stdout/stderr buffers no longer inherit bytes.Buffer fast paths. Standard
Write, io.WriteString and io.Copy all respect the reviewed raw limit. The15MiB raw
budget is validated independently from the1MiB maximum model-summary tier; neither
changes the summary policy or grants new execution authority. Aggregate overflow
is explicitly marked truncated.

The full worker/ownedprocess race suite and vet passed. An ordinary local child
exercises the real os/exec pipe path; these tests contain no model inference or
credentials. Initial negative results exposed the raw/summary clamp interaction
and are preserved in the [validation](evidence/worker-process-capture-boundaries-20261106/validation.json).
Compiler cache isolation is not added or claimed by this increment.
