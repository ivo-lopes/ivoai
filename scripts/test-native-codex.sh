#!/usr/bin/env bash
# Opt-in official native TUI/App Server smoke; no provider login or real model calls.
set -euo pipefail
test_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-native-codex-test.XXXXXXXX")"
readonly test_root
trap 'echo "Native Codex test workspace: $test_root" >&2' EXIT
case "$(uname -m)" in
  x86_64) arch=amd64; target=x86_64-unknown-linux-musl ;;
  aarch64) arch=arm64; target=aarch64-unknown-linux-musl ;;
  *) echo 'native Codex smoke requires Linux amd64 or arm64' >&2; exit 1 ;;
esac
readonly arch target
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for binary in codex codex-code-mode-host; do
  asset="$binary-$target.tar.gz"
  read -r url digest < <(python3 - "$repo_root/manifest/native-codex-runtime.json" "$asset" <<'PY_PIN'
import json,sys
rows=[x for x in json.load(open(sys.argv[1])) if x['name']==sys.argv[2]]
assert len(rows)==1
print(rows[0]['url'],rows[0]['sha256'])
PY_PIN
)
  curl --fail --location --proto '=https' --silent --show-error "$url" -o "$test_root/$asset"
  printf '%s  %s\n' "$digest" "$test_root/$asset" | sha256sum --check --strict -
  tar -xzf "$test_root/$asset" -C "$test_root"
  mv -- "$test_root/$binary-$target" "$test_root/$binary"
done
startup=(python3 "$repo_root/scripts/verify-native-codex-pair.py" --arch "$arch" --directory "$test_root")
if [[ "$arch" == amd64 ]]; then
  startup+=(--emulator "$(command -v qemu-x86_64)" --cpu Nehalem,-avx,-avx2)
fi
"${startup[@]}" --output "$test_root/pair-startup.json"
IVOAI_LIVE_CODEX_PATH="$test_root/codex" IVOAI_LIVE_CODEX_TUI=1 \
  go test -race ./internal/codexfrontend -count=1 -timeout 2m
