#!/usr/bin/env bash
# Official Claude 2.1.296, synthetic loopback provider, private network/HOME.
# No installation, provider login, personal configuration, or real inference.
set -euo pipefail
umask 077
archive=''
proof_output=''
while (( $# )); do
  case "$1" in
    --archive) [[ $# -ge 2 ]] || exit 2; archive=$2; shift 2 ;;
    --output) [[ $# -ge 2 ]] || exit 2; proof_output=$2; shift 2 ;;
    *) echo 'usage: test-claude-worker-tools.sh [--archive verified-tar.gz] [--output private-dir]' >&2; exit 2 ;;
  esac
done
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || {
  echo 'This protocol proof pins official Claude 2.1.296 for Linux amd64 only' >&2
  exit 1
}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-claude-worker-tools.XXXXXXXX")
readonly repo_root test_root
trap 'echo "Claude worker proof workspace: $test_root" >&2' EXIT
if [[ -z $archive ]]; then
  archive="$test_root/claude-linux-x64.tar.gz"
  curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
    --connect-timeout 20 --max-time 180 \
    'https://github.com/anthropics/claude-code/releases/download/v2.1.296/claude-linux-x64.tar.gz' \
    -o "$archive"
fi
printf '%s  %s\n' '45fe2b33f5cbdd114309618970bcd90b8b163e34252d66cf100d7e068d9b831d' "$archive" | sha256sum -c -
[[ $(tar -tzf "$archive") == claude ]] || { echo 'unexpected official archive layout' >&2; exit 1; }
tar -xzf "$archive" -C "$test_root"
if [[ -z $proof_output ]]; then
  proof_output="$test_root/proof"
fi
mkdir -p -- "$proof_output"
proof_output=$(cd -P -- "$proof_output" && pwd -P)
if [[ $(stat -c '%u:%a' -- "$proof_output") != "$(id -u):700" ]]; then
  echo 'Claude worker proof output must be owned by the current user with mode 0700' >&2
  exit 1
fi
cd -- "$repo_root"
IVOAI_CLAUDE_WORKER_OFFICIAL_BINARY="$test_root/claude" \
  IVOAI_CLAUDE_WORKER_PROOF_DIR="$proof_output" \
  go test ./internal/workers -run '^TestClaudeWorkerOfficialDelegationBoundary$' -count=1 -v -timeout 4m
python3 - "$proof_output/summary.json" <<'PY'
import json
import sys
with open(sys.argv[1]) as stream:
    summary = json.load(stream)
assert summary['pass'] and len(summary['cases']) == 5
assert all(case['pass'] for case in summary['cases'])
print('Official Claude worker boundary: PASS (5 mandatory cases, no SKIP)')
PY
