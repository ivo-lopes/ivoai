#!/usr/bin/env bash
set -euo pipefail
readonly archive="${1:?usage: test-patched-opencode.sh ARCHIVE METADATA}"
readonly metadata="${2:?usage: test-patched-opencode.sh ARCHIVE METADATA}"
expected_sha="$(jq -er '.sha256' "$metadata")"
readonly expected_sha
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum -c -
test_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-patched-opencode-test.XXXXXXXX")"
readonly test_root
trap 'echo "Patched frontend test workspace: $test_root" >&2' EXIT
tar -xzf "$archive" -C "$test_root"
export IVOAI_LIVE_OPENCODE_PATH="$test_root/opencode"
IVOAI_LIVE_OPENCODE_VERSION="$(jq -er '.version' "$metadata")"
export IVOAI_LIVE_OPENCODE_VERSION IVOAI_LIVE_OPENCODE_TUI=1
[[ "$("$IVOAI_LIVE_OPENCODE_PATH" --version)" = "$IVOAI_LIVE_OPENCODE_VERSION" ]]
for scenario in success:mouse:csi-u success:multiline:csi-u failure:mouse:xterm failure:message:xterm; do
  IFS=: read -r result copy keyboard <<< "$scenario"
  IVOAI_LIVE_OPENCODE_CLIPBOARD="$result" IVOAI_LIVE_OPENCODE_COPY="$copy" IVOAI_LIVE_OPENCODE_KEYBOARD="$keyboard" \
    go test ./internal/opencodebridge -run '^TestLiveManagedOpenCodeResizeKeyboard$' -count=1 -timeout 2m
done
