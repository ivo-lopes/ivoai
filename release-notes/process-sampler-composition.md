# Sampler calibration composition with the reviewed benchmark source

Merge `e521dda99309f7f217b21b5e7352eca2a0c36e98` combines diagnostic source
`b941a6ebe17e44ecad889e20cf932afa59523cd2` with PR46 source
`54eb54edb9a08e732c45ed7a996b538741294a87`, including main's G0 implementation.
The calibration, diagnostic tests, ProcessSampler and common driver remain byte
identical to b941. No AB/BA cohort was rerun or historical row rescored.

Root repeated the combined Python suite with explicit offline fixtures:
69 tests passed in 12.427 s. Root also repeated the diagnostic four-test suite
(1.392 s), checked the retained diagnostic artifacts and independently verified
the original 32 balanced rows and their source scope. The first independent
fixture failure remains **INCONCLUSIVE**. Later successful diagnostics do not
resolve it, and this contribution remains a draft requiring review/CI.

The original four-pair measurements retain their original source8f identity.
Process counts and RSS stay partial; missed churn identities cannot be inferred
away from zero sampler error flags. There is no correction factor or LLM saving.
The 126 comparison tally elsewhere refers only to six backend-first cohorts;
separate original PR30 studies remain preserved outside that count.

This merge/report changes no workload, deadline, oracle, runtime sampler,
default, dependency, client configuration, provider quota or released artifact.
Context synchronization remains BLOCKED under IVOAI-175.
