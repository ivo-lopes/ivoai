#!/bin/sh
# Run from this release checkout on the affected host. No personal config/auth
# is copied or modified; the opt-in PTY test uses a private home and fake provider.
set -eu
if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' 'FIRST_RUN_PROBE=UNAVAILABLE_GO_TOOLCHAIN'
  exit 1
fi
if ! command -v codex >/dev/null 2>&1; then
  printf '%s\n' 'FIRST_RUN_PROBE=UNAVAILABLE_CODEX'
  exit 1
fi
probe_codex=$(command -v codex)
printf 'PROBE_UID=%s\n' "$(id -u)"
"$probe_codex" --version
IVOAI_LIVE_CODEX_PATH="$probe_codex" IVOAI_LIVE_CODEX_TUI=1 \
  go test ./internal/codexfrontend -run '^TestLiveNativeTUI$' -count=1 -v
