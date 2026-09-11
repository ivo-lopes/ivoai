#!/usr/bin/env bash
# Build the narrowly patched managed frontend without changing an installed copy.
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly repo_root
readonly upstream_revision=cb7d8b2f5e44876ef98b661dc10590c915af3a9f
readonly source_sha=95cf680c4263ef71d54329df867d6165406e715f6c4368cdc697b61bd328ca0e
readonly managed_version=1.18.25-ivoai.1
readonly bun_version=1.3.14
readonly output_dir="${1:?usage: build-managed-opencode.sh ABSOLUTE_OUTPUT_DIR}"
[[ "$output_dir" = /* ]] || { echo 'output directory must be absolute' >&2; exit 1; }
[[ "$(uname -s)" = Linux ]] || { echo 'managed frontend build requires Linux' >&2; exit 1; }
case "$(uname -m)" in
  x86_64)
    arch=amd64; bun_arch=x64
    bun_sha=951ee2aee855f08595aeec6225226a298d3fea83a3dcd6465c09cbccdf7e848f
    ;;
  aarch64)
    arch=arm64; bun_arch=aarch64
    bun_sha=a27ffb63a8310375836e0d6f668ae17fa8d8d18b88c37c821c65331973a19a3b
    ;;
  *) echo 'unsupported native architecture' >&2; exit 1 ;;
esac
readonly arch bun_arch bun_sha
build_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-opencode-build.XXXXXXXX")"
readonly build_root
trap 'echo "Managed frontend build workspace: $build_root" >&2' EXIT
mkdir -p "$output_dir"
cd "$build_root"
curl --fail --location --silent --show-error \
  "https://github.com/oven-sh/bun/releases/download/bun-v${bun_version}/bun-linux-${bun_arch}.zip" -o bun.zip
printf '%s  %s\n' "$bun_sha" bun.zip | sha256sum -c -
unzip -q bun.zip
curl --fail --location --silent --show-error \
  "https://codeload.github.com/anomalyco/opencode/tar.gz/${upstream_revision}" -o source.tar.gz
printf '%s  %s\n' "$source_sha" source.tar.gz | sha256sum -c -
tar -xzf source.tar.gz
readonly source_root="$build_root/opencode-$upstream_revision"
readonly clipboard_patch="$repo_root/patches/opencode/1.18.25-clipboard-errors.patch"
patch_sha="$(sha256sum "$clipboard_patch" | cut -d ' ' -f1)"
revision="$(printf '%s\n%s\n' "$source_sha" "$patch_sha" | sha256sum | cut -d ' ' -f1)"
readonly patch_sha revision
cd "$source_root"
patch --dry-run --fuzz=0 -p1 < "$clipboard_patch"
patch --fuzz=0 -p1 < "$clipboard_patch"
export PATH="$build_root/bun-linux-$bun_arch:$PATH"
export XDG_CONFIG_HOME="$build_root/config"
export XDG_DATA_HOME="$build_root/data"
export XDG_STATE_HOME="$build_root/state"
export XDG_CACHE_HOME="$build_root/cache"
# Do not execute dependency install hooks. The official build script is invoked
# explicitly after locked dependency resolution; its checks remain intact.
bun install --frozen-lockfile --ignore-scripts
cd packages/opencode
OPENCODE_VERSION="$managed_version" OPENCODE_CHANNEL=latest \
  bun run script/build.ts --single --skip-install
if [[ "$arch" = amd64 ]]; then binary_dir=dist/opencode-linux-x64/bin; else binary_dir=dist/opencode-linux-arm64/bin; fi
[[ "$("$binary_dir/opencode" --version)" = "$managed_version" ]]
archive="ivoai-opencode_linux_${arch}.tar.gz"
install -m 0644 "$source_root/LICENSE" "$binary_dir/LICENSE"
tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner \
  -C "$binary_dir" -czf "$output_dir/$archive" opencode LICENSE
asset_sha="$(sha256sum "$output_dir/$archive" | cut -d ' ' -f1)"
jq -n --arg version "$managed_version" --arg platform "linux/$arch" \
  --arg revision "$revision" --arg sha256 "$asset_sha" --arg archive "$archive" \
  --arg upstream_revision "$upstream_revision" --arg source_sha256 "$source_sha" \
  --arg patch_sha256 "$patch_sha" --arg bun_version "$bun_version" --arg bun_sha256 "$bun_sha" \
  '{version:$version,platform:$platform,revision:$revision,sha256:$sha256,archive:$archive,
    upstream_revision:$upstream_revision,source_sha256:$source_sha256,patch_sha256:$patch_sha256,
    bun_version:$bun_version,bun_sha256:$bun_sha256}' > "$output_dir/managed-opencode-$arch.json"
echo "Managed frontend archive: $output_dir/$archive"
