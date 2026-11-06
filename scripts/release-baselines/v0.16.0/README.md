# Immutable v0.16.0 release verification

The verifier and adversarial tests are byte-preserved from source
`685c5740ec046dcbcf7eeec77c6fd88885975d8d`. They validate that release's original
12-file bundle, including OpenCode, without executing downloaded artifacts.
`test_release_artifacts_legacy_v016.py` checks their hashes and runs the original
suite separately from the current native bundle's tests. They are not current
runtime installation or frontend certification commands.

`setup-assets.json` freezes the original `ClientSetupCatalog(false)` platform
assets for isolated v0.16→v0.17 upgrade qualification. It was derived from that
source's fixture helper (ARM selects the same constant catalog with its ARM asset
key), without rebuilding a published Core. Its source file hash and own checksum
are bound in the qualification driver. Actual baseline executables remain the
original published, digest-verified v0.16.0 files. Previous profiles and user-owned
OpenCode data must remain unchanged during candidate setup, rollback and reapply.
