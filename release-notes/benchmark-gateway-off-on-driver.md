# Same-binary Gateway OFF/ON comparison

The existing economic driver accepts `--gateway-off-on`. It requires baseline
and candidate to be the identical immutable executable bytes, overrides the
ambient Gateway flag separately for each invocation (A OFF, B ON), and retains
the existing counterbalanced AB/BA ordering and admission/oracle contracts.
Authentication locations, filtered environment and official-client launch policy
are unchanged. The concurrent composition is explicitly refused in this mode
until its separate condition/freezing contract supports per-arm treatment.

New manifests record the exact per-arm configuration. Rows distinguish configured
ON/OFF from Gateway resolutions actually observed in completed Session events.
ON with no resolution is NOT_EXERCISED, not proof of activation. Any observed
Gateway resolution in OFF invalidates that measured row while retaining tokens
and other numeric observations. Neither the flag nor smaller logs establish
whole-session economy. Historical observations and failed gates are unchanged.

All 85 benchmark Python tests passed in 27.918s, including synthetic process
checks for AB/BA flag isolation, unchanged authentication scope, rejection of
unequal binaries before any probe, unavailable concurrent treatment, and
false-activation accounting. These are driver mechanics, not real inference.
The required official-client smoke and three-repetition nine-case cohort will
be recorded independently with fresh source/artifact identities and all outcomes.
