#!/usr/bin/env bash
set -euo pipefail

# This smoke deliberately uses the exact managed OpenCode artifact shipped by
# IVOAI. It exercises the real HTTP provider path and the real attach TUI; it
# never uses or inspects provider credentials.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(mktemp -d)"
trap 'rm -rf -- "$root"' EXIT
command -v script >/dev/null 2>&1 || {
  echo "util-linux script is required for the managed OpenCode PTY smoke" >&2
  exit 1
}
if [[ $# -eq 0 ]]; then
  "$repo_root/scripts/build-managed-opencode.sh" "$root/dist"
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64) arch=arm64 ;;
    *) echo 'unsupported managed frontend architecture' >&2; exit 1 ;;
  esac
  archive="$root/dist/ivoai-opencode_linux_${arch}.tar.gz"
  metadata="$root/dist/managed-opencode-${arch}.json"
else
  archive="${1:?usage: test-managed-opencode.sh [ARCHIVE METADATA]}"
  metadata="${2:?usage: test-managed-opencode.sh [ARCHIVE METADATA]}"
fi
version="$(jq -er '.version' "$metadata")"
digest="$(jq -er '.sha256' "$metadata")"
printf '%s  %s\n' "$digest" "$archive" | sha256sum --check --status
tar -xzf "$archive" -C "$root"
binary="$root/opencode"
test -x "$binary"
test "$($binary --version)" = "$version"

IVOAI_LIVE_OPENCODE_PATH="$binary" \
IVOAI_LIVE_OPENCODE_VERSION="$version" \
IVOAI_LIVE_OPENCODE_TUI=1 \
  go test ./internal/opencodebridge ./internal/agents \
    -run 'TestLive(ManagedOpenCode(RoutesPromptThroughIVOAI|AttachRendersIVOAIPlugin|Permissions|API|ResizeKeyboard)|OpenCodeExecutor(HTTPContract|NativeLifecycle)|NativeOpenCodeAUTOExecutor)$' \
    -count=1 -v

"$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/test-managed-opencode-offline.sh" "$archive" "$metadata"
