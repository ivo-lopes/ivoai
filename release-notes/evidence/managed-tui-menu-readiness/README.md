# Reproduce the fixture ordering control

The archived `.go.txt` files preserve diagnostic Go overlays. They add bounded
private PTY tracing and, for fault controls, pause only the owned TUI child.
No instrumentation is part of the committed application or regular test.

Use a disposable checkout of source
`96cf9c7a44c84c8ad5bf854b394f5ffb3741d93a`, a reviewed Go1.27.3 toolchain with
the pinned module cache available, and an already verified Linux AMD64 `.3`
executable. The actual run used public archive SHA256
`9f38b0507bdf295b34c7faf58f91fd402c5a102b76e8867242503d2e4db46b61`;
metadata and extracted ELF identity are in validation.json. Do not run an
unknown executable or install/authenticate a provider to reproduce this fixture.

```sh
repo_root=/path/to/disposable/source96cf
evidence=/path/to/later/fix-checkout/release-notes/evidence/managed-tui-menu-readiness
reviewed_binary=/path/to/verified/opencode
umask 077
scratch=$(mktemp -d /var/tmp/ivoai-menu-readiness.XXXXXXXX)
test "$(git -C "$repo_root" rev-parse HEAD)" = 96cf9c7a44c84c8ad5bf854b394f5ffb3741d93a
test "$("$reviewed_binary" --version)" = 1.19.0-ivoai.3
```

For the decisive comparison, run both `timed-command-tui_interaction_test.go.txt`
and `timed-command-fixed-tui_interaction_test.go.txt` as the `probe` below. They
differ only by the same four readiness/comment lines as the committed test.
Both independently resume the child after 500 ms; neither resumes in response
to the test reaching Enter. The old control must fail with missing Permissions;
the fixed control must pass. Record actual exits separately instead of treating
the expected old failure as a successful integration.

```sh
probe=timed-command-tui_interaction_test.go.txt
mkdir -m 700 "$scratch/trace"
python3 - "$repo_root" "$evidence/probes/$probe" "$scratch/overlay.json" <<'PY'
import json, sys
from pathlib import Path
repo, probe, output = map(Path, sys.argv[1:])
output.write_text(json.dumps({'Replace': {
    str(repo / 'internal/opencodebridge/tui_interaction_test.go'): str(probe)
}}))
PY
cd "$repo_root"
IVOAI_LIVE_OPENCODE_PATH="$reviewed_binary" \
IVOAI_LIVE_OPENCODE_VERSION=1.19.0-ivoai.3 \
IVOAI_LIVE_OPENCODE_TUI=1 IVOAI_LIVE_OPENCODE_CLIPBOARD=success \
IVOAI_LIVE_OPENCODE_COPY=mouse IVOAI_LIVE_OPENCODE_KEYBOARD=csi-u \
IVOAI_RESIZE_DIAGNOSTIC_DIR="$scratch/trace" \
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
go test -overlay "$scratch/overlay.json" ./internal/opencodebridge \
  -run '^TestLiveManagedOpenCodeResizeKeyboard$' -count=1 -timeout 2m -v
```

Use a new private scratch/trace directory for the other probe. The other archived
sources retain the traced baseline, Escape-queue and Enter-triggered queue
experiments; their outcomes remain separate in the receipt. Raw terminal output
is capped at 4 MiB and stays private. Published logs contain only labelled
synthetic fixture events/results. Existing managed HOME/XDG and credential
filtering remain in effect; the desktop clipboard case is not selected.

These controls establish a test synchronization mechanism on the reviewed pin.
They do not reproduce CI scheduler history, certify general TUI behavior,
exercise ARM/no-AVX again, or measure an economic gain.
