#!/usr/bin/env bash
# Retained command name; current releases do not build or run managed OpenCode.
set -euo pipefail
printf '%s\n' 'OPENCODE_MANAGED_DEPRECATED: managed OpenCode was removed; use scripts/test-native-codex.sh for current native certification. Historical v0.16 assets use scripts/release-baselines/v0.16.0/verify-release-artifacts.py; no user data was changed.' >&2
exit 1
