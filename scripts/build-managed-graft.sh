#!/usr/bin/env bash
# Build only pinned Graft library artifacts; never start its CLI, MCP or init.
set -euo pipefail
umask 022
export PYTHONDONTWRITEBYTECODE=1
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
helper="$repo_root/patches/graft"
revision=fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad
source_sha=ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1
lock_sha=c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5
node_version=26.11.1
case ${GRAFT_GCC_MAJOR:-} in
  "") cc=/usr/bin/gcc; cxx=/usr/bin/g++ ;;
  12) cc=/usr/bin/gcc-12; cxx=/usr/bin/g++-12 ;;
  *) echo "Unsupported reviewed compiler selection" >&2; exit 1 ;;
esac
[[ -x $cc && -x $cxx ]] || { echo "Selected compiler is missing" >&2; exit 1; }
[[ $(uname -s) == Linux ]] || { echo 'Linux native build required' >&2; exit 1; }
case $(uname -m) in
  x86_64) arch=amd64; node_arch=x64; node_sha=15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b; cpu_flags='-march=x86-64 -mtune=generic -mno-avx -mno-avx2' ;;
  aarch64) arch=arm64; node_arch=arm64; node_sha=170454fb022f5e9b5f6cc76c25fa32ef5100895be44eb04e92f9780591491bbf; cpu_flags='-march=armv8-a' ;;
  *) echo 'Only native Linux amd64/arm64 builds are supported' >&2; exit 1 ;;
esac
output=${1:-"$repo_root/dist/graft"}
mkdir -p -- "$output"
output=$(cd -- "$output" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-graft-build.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/build" "$work/node" "$work/cache" "$work/payload"
fetch() {
  local supplied=$1 url=$2 digest=$3 destination=$4
  if [[ -n $supplied ]]; then cp -- "$supplied" "$destination"; else env -i PATH=/usr/bin:/bin curl --disable --fail --location --proto '=https' --tlsv1.2 "$url" --output "$destination"; fi
  printf '%s  %s\n' "$digest" "$destination" | sha256sum --check --status || { echo 'Pinned archive checksum mismatch' >&2; exit 1; }
}
fetch "${GRAFT_SOURCE_ARCHIVE:-}" "https://codeload.github.com/trailhq/Graft/tar.gz/$revision" "$source_sha" "$work/source.tgz"
for command in bwrap python3 curl tar sha256sum make patch readelf; do command -v "$command" >/dev/null; done
fetch "${GRAFT_NODE_ARCHIVE:-}" "https://nodejs.org/dist/v$node_version/node-v$node_version-linux-$node_arch.tar.gz" "$node_sha" "$work/node.tgz"
python3 "$helper/build.py" extract "$work/source.tgz" "$work/build"
tar --extract --gzip --file "$work/node.tgz" --directory "$work/node" --strip-components=1 --no-same-owner
printf '%s  %s\n' "$lock_sha" "$work/build/package-lock.json" | sha256sum --check --status
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/dependency-lock.patch"
printf '%s  %s\n' 5fda79923e595c061e52a8f0154b7465fce7f5ac6441e5ed1ccff14cd46ae663 "$work/build/package-lock.json" | sha256sum --check --status
cp "$work/build/package-lock.json" "$work/build/build-package-lock.json"
python3 "$helper/build.py" lock "$work/build/package-lock.json" "$work/build-dependencies.json"
if [[ -n ${GRAFT_NPM_CACHE:-} ]]; then
  # Only npm content-addressed packages, never user configuration or credentials.
  cp -a -- "$GRAFT_NPM_CACHE/_cacache" "$work/cache/"
fi
sandbox=(bwrap --die-with-parent --new-session --unshare-all --clearenv
  --ro-bind /usr /usr --ro-bind /bin /bin --ro-bind /lib /lib --ro-bind-try /lib64 /lib64
  --ro-bind /etc/alternatives /etc/alternatives
  --bind "$work/build" /build --ro-bind "$work/node" /node
  --bind "$work/cache" /cache --ro-bind "$helper" /review
  --proc /proc --dev /dev --tmpfs /tmp --dir /home --dir /home/build
  --setenv HOME /home/build --setenv XDG_CONFIG_HOME /home/build/config
  --setenv XDG_CACHE_HOME /home/build/cache --setenv XDG_DATA_HOME /home/build/data
  --setenv PATH /node/bin:/usr/bin:/bin --setenv LANG C.UTF-8 --setenv TZ UTC
  --setenv SOURCE_DATE_EPOCH 0 --setenv PYTHONHASHSEED 0
  --setenv npm_config_userconfig /home/build/npm-user-config --setenv npm_config_globalconfig /home/build/npm-global-config
  --setenv npm_config_cache /cache --setenv npm_config_update_notifier false
  --setenv CC "$cc" --setenv CXX "$cxx"
  --setenv CFLAGS "$cpu_flags -ffile-prefix-map=/build=. -fdebug-prefix-map=/build=."
  --setenv CXXFLAGS "$cpu_flags -ffile-prefix-map=/build=. -fdebug-prefix-map=/build=."
  --chdir /build)
npm_args=(ci --ignore-scripts --no-audit --no-fund)
network_args=()
if [[ ${GRAFT_OFFLINE:-0} == 1 ]]; then
  npm_args+=(--offline)
else
  # Dependency fetch phase only: no lifecycle scripts, credentials or user homes.
  network_args+=(--share-net --ro-bind /etc/resolv.conf /etc/resolv.conf --ro-bind /etc/ssl /etc/ssl)
fi
"${sandbox[@]}" "${network_args[@]}" /node/bin/node /node/lib/node_modules/npm/bin/npm-cli.js "${npm_args[@]}"
"${sandbox[@]}" /node/bin/node /review/check-dependencies.mjs /build
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/tree-sitter-node26.patch"
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/bounded-parse-lifecycle.patch"
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/viewer-escaping.patch"
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/go-package-calls.patch"
"${sandbox[@]}" /usr/bin/python3 /review/test_viewer_escaping.py /build /node/bin/node
"${sandbox[@]}" /node/bin/node /review/check-parse-lifecycle.mjs /build
# These are the only reviewed package build actions. No npm run/prepare/install.
"${sandbox[@]}" /node/bin/node node_modules/typescript/bin/tsc -p tsconfig.json
"${sandbox[@]}" /node/bin/node node_modules/typescript/bin/tsc -p viewer/tsconfig.json
for package in tree-sitter tree-sitter-typescript tree-sitter-javascript tree-sitter-python tree-sitter-go tree-sitter-r tree-sitter-java tree-sitter-kotlin tree-sitter-swift tree-sitter-php; do
  "${sandbox[@]}" --chdir "/build/node_modules/$package" /node/bin/node /build/node_modules/node-gyp/bin/node-gyp.js rebuild --nodedir=/node --jobs=2 --napi_build_version=8
done
"${sandbox[@]}" /node/bin/node /review/check-go-package-calls.mjs /build
"${sandbox[@]}" /node/bin/node scripts/build-viewer.mjs
"${sandbox[@]}" /node/bin/node /node/lib/node_modules/npm/bin/npm-cli.js prune --omit=dev --ignore-scripts --offline --no-audit --no-fund
"${sandbox[@]}" /usr/bin/python3 /review/prepare_grammars.py /build
"${sandbox[@]}" /usr/bin/python3 /review/prune_cli.py /build
"${sandbox[@]}" /node/bin/node /review/check-yaml-library.mjs /build
python3 "$helper/build.py" lock "$work/build/package-lock.json" "$work/dependencies.json"
mv "$work/build/build-package-lock.json" "$work/payload/"
mv "$work/build/dist" "$work/build/node_modules" "$work/build/package.json" "$work/build/package-lock.json" "$work/build/LICENSE" "$work/payload/"
install -m 0755 "$helper/graft-runtime" "$work/payload/graft-runtime"
cp "$work/dependencies.json" "$work/payload/dependencies.json"
cp "$work/build-dependencies.json" "$work/payload/build-dependencies.json"
python3 "$helper/provenance.py" "$work/payload" "$repo_root" "$arch" "$node_sha" "$cpu_flags" "$cxx"
# Smoke the exact cleaned payload, not the source tree or its development deps.
python3 - "$helper" "$work/payload" <<'PY'
import sys
sys.path.insert(0, sys.argv[1])
from build import clean_native_payload
clean_native_payload(sys.argv[2])
PY
"${sandbox[@]}" --ro-bind "$work/payload" /build /node/bin/node /review/check-runtime.mjs
python3 "$helper/check-abi.py" "$work/payload"
archive="$output/ivoai-graft_linux_${arch}.tar.gz"
python3 "$helper/build.py" package "$work/payload" "$archive"
python3 "$helper/metadata.py" "$work/payload" "$archive"
(cd -- "$output" && sha256sum "$(basename -- "$archive")" > "$(basename -- "$archive").sha256")
printf 'Built %s\n' "$archive"
