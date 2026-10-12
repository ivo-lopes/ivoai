#!/usr/bin/env bash
# Exercise the exact reviewed distribution through IVOAI's real sandbox and MCP.
set -euo pipefail
archive=${1:?usage: test-managed-graft.sh ARCHIVE METADATA}
metadata=${2:?usage: test-managed-graft.sh ARCHIVE METADATA}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
case $(uname -m) in
  x86_64) arch=amd64; node_arch=x64; node_sha=15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b ;;
  aarch64) arch=arm64; node_arch=arm64; node_sha=170454fb022f5e9b5f6cc76c25fa32ef5100895be44eb04e92f9780591491bbf ;;
  *) echo 'Native Linux amd64/arm64 required' >&2; exit 1 ;;
esac
[[ $(jq -er '.platform' "$metadata") == "linux/$arch" ]]
[[ $(jq -er '.node_version' "$metadata") == 26.11.1 ]]
[[ $(jq -er '.node_sha256' "$metadata") == "$node_sha" ]]
expected_sha=$(jq -er '.sha256' "$metadata")
[[ $expected_sha =~ ^[a-f0-9]{64}$ ]]
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status
work=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-graft-contracts.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/payload" "$work/node"
if [[ -n ${GRAFT_NODE_ARCHIVE:-} ]]; then
  cp -- "$GRAFT_NODE_ARCHIVE" "$work/node.tgz"
else
  env -i PATH=/usr/bin:/bin curl --disable --fail --location --proto '=https' --tlsv1.2 \
    "https://nodejs.org/dist/v26.11.1/node-v26.11.1-linux-$node_arch.tar.gz" --output "$work/node.tgz"
fi
printf '%s  %s\n' "$node_sha" "$work/node.tgz" | sha256sum --check --status
tar --extract --gzip --file "$work/node.tgz" --directory "$work/node" --strip-components=1 --no-same-owner
tar --extract --gzip --file "$archive" --directory "$work/payload" --no-same-owner
export IVOAI_GRAFT_TEST_NODE="$work/node/bin/node" IVOAI_GRAFT_TEST_DIST="$work/payload"
cd -- "$repo_root"
"$IVOAI_GRAFT_TEST_NODE" --test internal/codeintel/adapter.test.mjs
go test -count=1 -timeout=5m ./internal/codeintel -run '^TestReviewed' -v
printf '%s\n' 'Managed Graft native sandbox, operations, MCP and worktree contracts: PASS'
