# Claude proof script: ShellCheck correction

CI [40738249519](https://github.com/ivo-lopes/ivoai/actions/runs/40738249519),
head `43ed3844256acbdaaab1d0ab5235934a43f649ef`, failed in ShellCheck:
`SC2174` on `mkdir -p -m 700`. Earlier quality steps, including full unit/race,
vet, native Codex and managed OpenCode integration, passed. The failure and
remaining unexecuted gates remain recorded in
`evidence/claude-worker-tools/shellcheck-correction/failed-ci-43ed.json`.
This failed run is not a passing final-source certification.

The script now sets `umask 077` before creating its workspace and output
directories. `mkdir -p` creates private ancestors as well as the final directory.
The canonical final output must be owned by the current user with mode 0700.
An existing unsuitable directory is rejected before the client runs; the script
does not change its mode or contents. Output filesystems must enforce those
ownership and permission requirements.

Local ShellCheck 0.10.0 passed across `install.sh` and every `scripts/*.sh`.
The unmodified prior script independently reproduced SC2174. The Debian package
was downloaded through apt, matched the existing apt metadata SHA256, and was
unpacked in a private temporary directory without system installation or a new
runtime dependency. Provenance and checksums are in the new validation receipt.

The corrected wrapper executed the pinned official Claude 2.1.296 proof: all
five mandatory protocol cases passed, zero skipped. New nested output directories
were 0700. A separate existing 0755 output was rejected and remained empty with
its original mode. These are synthetic loopback protocol checks, with no
authentication, real model inference, paid API call or personal configuration.

No production Go, embedded website, managed OpenCode source or pin changed.
Earlier receipts remain byte-identical and describe their original epochs.
The revised head still requires its own complete CI before merge and the tagged
Release gates before publication. No tag or release was created by this fix.
