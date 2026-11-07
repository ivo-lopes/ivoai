# Read-only sampler failure audit

Verdict: **INCONCLUSIVE**. No defensible fix for the original failure is established.

The original8f failure occurred at `row.correct`, before coverage/RSS assertions. The retained record has no case, phase, exit/signal, per-attempt timing, parse error category, stderr, interpreter identity or contemporaneous resource/load state. Its4.830s total includes four tests and possibly both real cases; it cannot identify a timeout or its location.

The current d7 source preserves the worker/oracle/protocol functions and driver wait4 logic from8f. READY holds known children before release, worker outputs are bounded, and common wait4 owns one coordinator reap. Ordinary sampler read/cap losses affect partial observation, not fixture correctness by themselves; time spent observing could still consume the overall deadline. No direct protocol deadlock/race was found. This is not proof that none exists.

One predeclared eight-control pipe/reap matrix plus a supplemental strictly barrier-forced fragmentation control produced the expected outcomes. Expected EOF, oversized input, empty-pipe timeout and deadline kill were retained as negative controls. None reran memory/churn/CPU/wait calibration cases. The supplemental control was necessary because two writes alone do not guarantee scheduling between reads; both records remain retained.

Current Python3.13.5 matches the binary hash in the original32-row cohort. The original independent failure did not record its interpreter, so no historical equivalence is inferred. All four supplied independent files match their retained copies; d7 source remains clean.

Keep the draft limitation. Do not raise deadlines or loosen oracles based on successful repeats. A future first natural failure needs one bounded row plus stage/exit and typed timeout/EOF/errno classification; this is a diagnostic proposal, not an identified fix. No source edit, new calibration cohort, real client/provider or economic claim resulted from this audit.
