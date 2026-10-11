#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# image-size is now patched; keep the production audit free of advisory ignores.
corepack pnpm --dir "$repo_root/website" audit --prod --audit-level high
