# Offline ProcessSampler calibration — IVOAI-138 / IVOAI-198

**Reliability limitation:** a later independent run of the unchanged `8f513876`
fixture tests failed once; its original case/stage was not recorded. Follow-up
successes do not establish a fix. The [diagnostic follow-up](process-sampler-diagnostics.md)
preserves that failure and adds bounded phase/case messages. This increment
remains a draft; the original 32 observations below are not rescored.


This separate Python harness calibrates the existing optional sampler on fixed
local subprocess trees. It changes neither `ProcessSampler`, the economic driver,
its schemas/defaults nor any historical dataset. **No native client, provider,
credential, network access or LLM inference was used.** It establishes no LLM
benefit and supplies no correction factor to subtract from client measurements.

The [freeze](evidence/backend-first/process-sampler-calibration-v1/freeze.json)
was written before execution and checked after the cohort and tests. It binds the
adapter, unchanged sampler/common driver, tests, catalog helper, Python executable
and full plan. There are four pairs per case in AB/BA/AB/BA order (A OFF/B ON):
32 rows, all retained. Each owned process group has a three-second deadline;
the cohort has a 110-second budget, with unexecuted cases marked NOT_RUN on
budget/source failure. The fixed workers use private HOME/explicit environments,
no shell or user configuration, 128 MiB per-process address-space limits and
bounded CPU/descriptors. Successful execution took 14.565 seconds; all 32 fixture
oracles passed and all 16 ON observations remained explicitly partial.

Four workloads exercise different mechanisms:

- Wait: one coordinator sleeps 250 ms after the barrier.
- CPU: one coordinator computes two million modular squared terms, checked
  independently against the sum-of-squares formula.
- Touched memory: two children each allocate/touch 4 MiB, then remain held until
  the READY/release barrier and sleep 250 ms concurrently. All children are
  waited; touched-page checksums and counts must match.
- Churn: twelve sequential children each sleep 12 ms after release. The first
  child is held at READY. Successful startup/completion and child exit counts
  are checked, giving thirteen known process identities including coordinator.

The first sampler tick occurs at READY, with known children held alive. Subsequent
ticks use the unchanged driver's ten-millisecond wait4 polling and original
100 ms sampling interval/caps. Startup before READY is not observed. These
controls do not certify real-client startup, threaded trees or process escape.

[Every pair and metric](evidence/backend-first/process-sampler-calibration-v1/diagnostic.json)
is available alongside all individual rows. Signed paired medians in milliseconds:

| Fixture | External wall ON−OFF | Controller CPU ON−OFF | Sampler tick CPU, ON |
| --- | ---: | ---: | ---: |
| Wait | +2.399 | +1.242 | 1.258 |
| CPU | −1.706 | +0.765 | 0.825 |
| Touched-memory children | +4.564 | +2.960 | 3.081 |
| Churn | −10.321 | +5.093 | 5.243 |

Nine individual paired wall deltas were positive and seven negative. These are
small shared-host observations with no CPU reservation or measured starting
host load. Interpreter startup, scheduling, polling and protocol work affect the
external measurement. No favorable row was selected, condition tuned or result
excluded. CPU of the owned workload is recorded separately through wait4 and
coordinator/reaped-child phase reports. Controller process CPU covers the whole
attempt; sampler tick CPU/wall is included within it. External wall runs from
Popen through controller parsing/cleanup, not an exact kernel exit timestamp;
active wall starts at barrier release. Costs must not be added twice or
mechanically subtracted from LLM elapsed time.

**Churn missed four to five of thirteen known process identities in every ON
execution, despite zero read failures, partial-sample counts or cap flags.**
Known stable trees were observed at the expected counts. Absence of read/cap
flags therefore does not establish complete lifecycle coverage. The sampler's
`actual_process_total` and `exact_peak_rss` remain null; fixture-known counts are
separate evidence, not replacements for those unknown measurements.

The largest observed subtree RSS sum was 71,180,288 bytes. This includes shared
pages and interpreter overhead; it is neither unique physical RAM nor an exact
lifetime peak. Controller RAM and whole-host resources were not measured. Caps
were unchanged and not reached here; large process/thread stress requires a
separate declared experiment.

[Validation](evidence/backend-first/process-sampler-calibration-v1/validation.json)
records 65 passing Python tests, including explicit real offline memory/churn
fixtures. Earlier default unit execution had three passes and one explicit
NOT_RUN, not an extra successful live fixture. Tests also check balanced planning,
the independent CPU oracle, private bounded output/no overwrite, environment
allowlisting and failed-launch preservation with unknown resource values. All
source and artifact hashes were checked again. A Python executable hash does
not attest the entire stdlib, shared libraries or host kernel.

To reproduce on Linux with a reviewed Python 3 executable and a new absolute
output path beneath a private directory:

```sh
python3 -I scripts/benchmark_sampler_calibration.py --output /private/new-calibration
IVOAI_SAMPLER_CALIBRATION_FIXTURES=1 python3 -m unittest discover -s scripts -p 'test_benchmark_sampler_calibration.py'
```

The base is `a137d693`; PR46/PR51 remain separate and unmodified. There is no
version bump, runtime dependency, catalog promotion, provider quota use or
reanalysis of 126 historical turns. The economic gate remains INCOMPLETE and
CONTEXT_SYNC remains BLOCKED under IVOAI-175. Rollback removes this standalone
adapter/tests/report; no daemon, personal configuration or persistent migration
exists.
