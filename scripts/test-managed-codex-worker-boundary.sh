#!/usr/bin/env bash
# Opt-in protocol proof; downloads reviewed client assets into a private scratch
# directory. No installation, provider credentials, model inference or TUI claim.
set -euo pipefail

[[ "$(uname -sm)" == 'Linux x86_64' ]] || {
  echo 'This pinned proof requires Linux amd64; other runtimes are NOT_RUN.' >&2
  exit 1
}
proof_root="$(mktemp -d "${TMPDIR:-/var/tmp}/ivoai-worker-boundary.XXXXXXXX")"
readonly proof_root
trap 'echo "Retained worker-boundary proof workspace: $proof_root" >&2' EXIT
mkdir -m 700 "$proof_root/bin"

download() {
  local asset="$1" digest="$2"
  curl --fail --location --silent --show-error \
    "https://github.com/openai/codex/releases/download/rust-v0.162.1/${asset}.tar.gz" \
    -o "$proof_root/${asset}.tar.gz"
  printf '%s  %s\n' "$digest" "$proof_root/${asset}.tar.gz" | sha256sum -c -
  # Accept the single bounded regular executable; never apply archive ownership,
  # paths, permissions, links or a third-party installer.
  python3 - "$proof_root/${asset}.tar.gz" "$proof_root/bin/$asset" "$asset" <<'PY'
import os, pathlib, sys, tarfile
archive, output, expected = sys.argv[1:]
with tarfile.open(archive, "r:gz") as tar:
    members = tar.getmembers()
    if len(members) != 1 or members[0].name != expected or not members[0].isfile() or not 0 < members[0].size <= 400_000_000:
        raise SystemExit("unexpected reviewed archive layout")
    with tar.extractfile(members[0]) as source, open(output, "xb") as target:
        while block := source.read(1 << 20):
            target.write(block)
os.chmod(output, 0o700)
PY
}

download codex-x86_64-unknown-linux-musl 86f268d81b898f3e144c5ecff2ad2fda2c5802e042d6195b72538fe0fa125057
download codex-code-mode-host-x86_64-unknown-linux-musl 458239ea45c1fc508d89695f180ab352f2834709a8a46929a4466f4f04399ada
ln -s codex-code-mode-host-x86_64-unknown-linux-musl "$proof_root/bin/codex-code-mode-host"
IVOAI_WORKER_BOUNDARY_CODEX_PATH="$proof_root/bin/codex-x86_64-unknown-linux-musl" \
  go test -race -v ./internal/workers -run '^TestPinnedCodexManagedNativeWorkerBoundary$' -count=1 -timeout 3m \
  >"$proof_root/test.log" 2>&1
cat "$proof_root/test.log"
