# Sampler fixture diagnostics after independent failure

**The cause of the independent failure remains INCONCLUSIVE.** This follow-up
improves diagnostic coverage; it does not fix or explain the original failure,
change a deadline, tune a workload or rerun the AB/BA cohort. Publication remains
appropriate only as a draft with this limitation.

At unchanged source `8f513876`, an independent explicit four-test fixture run
failed in 4.830 s at `row.correct`, reporting `fixture_or_protocol_failed`.
The original test did not record which memory/churn case or phase failed. The
[preserved record](evidence/backend-first/process-sampler-diagnostics-v1/independent-source8f/initial-unittest-failure.log)
was reconstructed by the parent agent from its retained tool transcript; it is
not represented as raw captured stderr. No stage, case or interpreter identity
is retroactively inferred. All supplied files are copied byte for byte.

Two subsequent same-source diagnostic cases passed: touched memory 421.328 ms
(READY read 124.093 ms) and churn 802.269 ms (READY read 81.208 ms). Their rows
and read-stage observations remain separate from the failure and original
cohort. Green repeats do not remove the unresolved observation. The original
32 rows, freeze, logs and diagnostics remain unchanged and retain their original
source and scope.

The adapter now records only a bounded `failure_phase`: `launch`, `ready`,
`sample`, `release`, `wait4`, `done` or `oracle`. It is null on successful or
unexecuted rows. A nonzero wait4 outcome keeps the wait4 label. The label identifies
where the failure surfaced, not its cause. Real fixture checks use a separate
subtest for each case and include the complete bounded allowlisted row in failure
messages. No argv, environment, paths, PIDs or raw exception text are added.

[The new source freeze](evidence/backend-first/process-sampler-diagnostics-v1/source-epoch.json)
precedes diagnostic tests and was checked afterward. AST/byte comparisons confirm
the plan, workload functions, independent oracle, constants/deadlines, sampler
and common wait4 helper are unchanged. The three-second attempt deadline and
110-second cohort budget remain intact. This additional projection changes the
source identity, so it cannot be silently combined with the old overhead cohort.
No new AB/BA observations were collected.

New-source diagnostic tests passed four cases in 1.372 s, including explicit
local memory/churn fixtures. The original ProcessSampler tests passed three
cases in 1.079 s. An injected formatting-only negative control produced two
expected failed subtests, confirming both case and complete bounded row appear;
it launched no workload and is not confused with the independent real failure.
An injected launch exception also verifies phase labeling and exclusion of raw
exception text. These checks validate the diagnostic projection, not resolution
of the original issue.

[Validation and all outcomes](evidence/backend-first/process-sampler-diagnostics-v1/validation.json)
remain separate. No sampler/default/schema/version change, actual client/provider,
credential access, model quota use or economic claim is introduced. RSS remains
an approximate sampled sum and process coverage partial, including the original
churn misses. The economic gate remains INCOMPLETE. CONTEXT_SYNC remains BLOCKED
under IVOAI-175. PR46/PR51 are not changed by this follow-up.
