#!/usr/bin/env bash
set -euo pipefail
if [[ "${IVOAI_LIVE_P0:-}" != 1 ]]; then
  echo 'Set IVOAI_LIVE_P0=1 to run real operator acceptance with authenticated official clients.' >&2
  exit 2
fi
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case_root="$(mktemp -d)"
trap 'rm -rf -- "$case_root"' EXIT
binary="${IVOAI_BINARY:-$case_root/ivoai}"
if [[ -z "${IVOAI_BINARY:-}" ]]; then
  go build -o "$binary" "$repo_root/cmd/ivoai"
fi
evidence="${IVOAI_LIVE_P0_EVIDENCE:-$case_root/evidence}"
executors=(codex claude)
if [[ -n "${IVOAI_LIVE_P0_EXECUTOR:-}" ]]; then
  executors=("$IVOAI_LIVE_P0_EXECUTOR")
fi
result=0
cd "$repo_root"
for executor in "${executors[@]}"; do
  python3 "$repo_root/scripts/p0-voicehub-live.py" "$binary" --executor "$executor" --evidence "$evidence" || result=1
done
exit "$result"
