#!/usr/bin/env bash
# Real last-stable CLI/update gate. No IVOAI_TEST_MODE, credentials or inference.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$repo_root/scripts/stable_release_upgrade.py" "$@"
