# Native provider isolation: actual main composition

Local merge `3a380bfc00a0445b154d0f0a6d378fdd1945fbd9` incorporates actual main
`1dcf90400f67eb960a2446ccf3c987ecf977a9d8`, tree
`c26448992236054aba5672fc84b72dcc435423c9`, into the reviewed PR45 source
`99800ee5`. The earlier PR51 snapshot already supplied the code of its later main
merge; a separate mechanical merge of that parent was unnecessary.

The [new composition receipt](evidence/native-provider-independence/main55-composition-v1/validation.json)
verifies all 40 incoming PR55 paths against main. The previous 567 Go files,
native-picker regression, authored docs, embedded site and prior PR45 evidence
remain byte-identical. Only two benchmark fixture Go files were added; no
production runtime behavior or presentation contract changed.

Full Go passed 59 packages, with three no-test packages separate. Full vet, CLI,
73 Python tests and the benchmark fixture race package passed. Its one opt-in
skip is recorded and is not a successful live benchmark. Mandatory real
legacy/current protected-artifact rollback passed its one required named test
with zero skips. Documentation markers, site and build scripts match the prior
reviewed source; no Node build or previous 13-package race was repeated.

Earlier epochs and failures remain intact. These checks do not execute actual
concurrent projects, prove economic gains or complete the global benchmark
matrix. The Claude presentation/Core and nil-quota compatibility decisions in
the preceding integration report remain unchanged. This work activates no
client/provider, changes no personal preferences/authentication/history and
mutates no published v0.14.0 artifact. The final PR45 source still requires root
review and its fresh 16 hosted checks before merge.
