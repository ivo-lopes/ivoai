#!/usr/bin/env bash
# Execute the library and every grammar under the actual managed Node, without AVX.
set -euo pipefail
archive=${1:?usage: test-graft-baseline.sh ARCHIVE METADATA}
metadata=${2:?usage: test-graft-baseline.sh ARCHIVE METADATA}
[[ $(jq -er '.platform' "$metadata") == linux/amd64 ]] || exit 0
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
expected_sha=$(jq -er '.sha256' "$metadata")
printf '%s  %s\n' "$expected_sha" "$archive" | sha256sum --check --status
node_sha=15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b
[[ $(jq -er '.node_version' "$metadata") == 26.11.1 ]]
[[ $(jq -er '.node_sha256' "$metadata") == "$node_sha" ]]
command -v qemu-x86_64 >/dev/null
work=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-graft-baseline.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/payload" "$work/node"
if [[ -n ${GRAFT_NODE_ARCHIVE:-} ]]; then
  cp -- "$GRAFT_NODE_ARCHIVE" "$work/node.tgz"
else
  env -i PATH=/usr/bin:/bin curl --disable --fail --location --proto '=https' --tlsv1.2 \
    https://nodejs.org/dist/v26.11.1/node-v26.11.1-linux-x64.tar.gz --output "$work/node.tgz"
fi
printf '%s  %s\n' "$node_sha" "$work/node.tgz" | sha256sum --check --status
tar --extract --gzip --file "$work/node.tgz" --directory "$work/node" --strip-components=1 --no-same-owner
tar --extract --gzip --file "$archive" --directory "$work/payload" --no-same-owner
ulimit -c 0
timeout 300 bwrap --die-with-parent --new-session --unshare-all --clearenv \
  --ro-bind /usr /usr --ro-bind /bin /bin --ro-bind /lib /lib --ro-bind-try /lib64 /lib64 \
  --ro-bind "$work/payload" /build --ro-bind "$work/node" /node \
  --ro-bind "$repo_root/patches/graft" /review --proc /proc --dev /dev --tmpfs /tmp \
  --dir /home --dir /home/test --setenv HOME /home/test --setenv PATH /node/bin:/usr/bin:/bin \
  --setenv LANG C.UTF-8 --setenv TZ UTC --chdir /build \
  /usr/bin/qemu-x86_64 -cpu Nehalem /node/bin/node /review/check-runtime.mjs
printf '%s\n' 'Managed Graft non-AVX CPU smoke: PASS'
