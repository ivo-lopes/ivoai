#!/usr/bin/env bash
# Reviewed optional Caveman rebuild. No upstream installer, hooks, credentials or API.
set -euo pipefail
umask 022
export PYTHONDONTWRITEBYTECODE=1
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
helper="$repo_root/patches/caveman"
python3 "$helper/test_build.py"
revision=b4531dfc05a3ef0ba93b57e8bfa5f48564a4b4d6
source_sha=29ef4c5bccee70dbc486bae266f966b8bd53132c6e12e27e9b06b36cf35c51f4
patch_sha=5f95e80d41d5744f16e09d534b1d56585fe746f9d5aed1e14c2586bbe953810d
[[ $(uname -s) == Linux ]] || { echo 'Linux native build required' >&2; exit 1; }
case $(uname -m) in
 x86_64) arch=amd64; zig_arch=x86_64; zig_sha=70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00; target=x86_64-linux-musl; cflags='-O2 -g0 -mno-avx -mno-avx2' ;;
 aarch64) arch=arm64; zig_arch=aarch64; zig_sha=ea4b09bfb22ec6f6c6ceac57ab63efb6b46e17ab08d21f69f3a48b38e1534f17; target=aarch64-linux-musl; cflags='-O2 -g0' ;;
 *) echo 'Only native Linux amd64/arm64 is supported' >&2; exit 1 ;;
esac
for command in bwrap python3 curl tar sha256sum patch readelf go; do command -v "$command" >/dev/null; done
[[ $(go version | cut -d' ' -f3) == go1.27.3 ]] || { echo 'Reviewed Go1.27.3 is required' >&2; exit 1; }
goroot=$(go env GOROOT)
output=${1:-"$repo_root/dist/caveman"}
mkdir -p -- "$output"
output=$(cd -- "$output" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-caveman-build.XXXXXXXX")
cleanup() { python3 "$helper/build.py" cleanup "$work"; }
trap cleanup EXIT
mkdir -p "$work/build" "$work/zig" "$work/cache" "$work/modcache" "$work/payload/third-party"
fetch() {
 local supplied=$1 url=$2 digest=$3 destination=$4
 if [[ -n $supplied ]]; then cp -- "$supplied" "$destination"; else env -i PATH=/usr/bin:/bin curl --disable --fail --location --proto '=https' --tlsv1.2 "$url" --output "$destination"; fi
 printf '%s  %s\n' "$digest" "$destination" | sha256sum --check --status || { echo 'Pinned build archive checksum mismatch' >&2; exit 1; }
}
fetch "${CAVEMAN_SOURCE_ARCHIVE:-}" "https://codeload.github.com/JuliusBrussee/caveman/tar.gz/$revision" "$source_sha" "$work/source.tgz"
fetch "${CAVEMAN_ZIG_ARCHIVE:-}" "https://ziglang.org/download/0.16.0/zig-$zig_arch-linux-0.16.0.tar.xz" "$zig_sha" "$work/zig.tar.xz"
printf '%s  %s\n' "$patch_sha" "$helper/dependency-lock.patch" | sha256sum --check --status
python3 "$helper/build.py" extract "$work/source.tgz" "$work/build"
python3 "$helper/build.py" extract "$work/zig.tar.xz" "$work/zig"
patch --directory "$work/build" --strip=1 --fuzz=0 < "$helper/dependency-lock.patch"
if [[ -n ${CAVEMAN_GO_MODULE_CACHE:-} ]]; then
 # Content-addressed public module downloads only; no personal configuration.
 cp -a -- "$CAVEMAN_GO_MODULE_CACHE/cache" "$work/modcache/"
fi
sandbox=(bwrap --die-with-parent --new-session --unshare-all --clearenv
 --ro-bind /usr /usr --ro-bind /bin /bin --ro-bind /lib /lib --ro-bind-try /lib64 /lib64
 --ro-bind "$goroot" /go --ro-bind "$work/zig" /zig --ro-bind "$helper" /review
 --bind "$work/build" /build --bind "$work/cache" /cache --bind "$work/modcache" /gomodcache
 --bind "$work/payload" /out --proc /proc --dev /dev --tmpfs /tmp --dir /home --dir /home/build
 --setenv HOME /home/build --setenv PATH /go/bin:/usr/bin:/bin --setenv LANG C.UTF-8 --setenv TZ UTC
 --setenv GOTOOLCHAIN local --setenv GOMODCACHE /gomodcache --setenv GOCACHE /cache/go
 --setenv ZIG_GLOBAL_CACHE_DIR /cache/zig --setenv GOMAXPROCS 2 --setenv GOAMD64 v1
 --setenv SOURCE_DATE_EPOCH 0 --setenv GOOS linux --setenv GOARCH "$arch" --chdir /build)
network=()
if [[ ${CAVEMAN_OFFLINE:-0} == 1 ]]; then
 network+=(--setenv GOPROXY off)
else
 network+=(--share-net --ro-bind /etc/resolv.conf /etc/resolv.conf --ro-bind /etc/ssl /etc/ssl
  --setenv GOPROXY https://proxy.golang.org --setenv GOSUMDB sum.golang.org)
fi
# Only the reviewed Go toolchain downloads modules; module code has no install hooks.
"${sandbox[@]}" "${network[@]}" /go/bin/go mod download
cgo=(--setenv CGO_ENABLED 1 --setenv CC "/zig/zig cc -target $target -mcpu=baseline" --setenv CGO_CFLAGS "$cflags" --setenv CGO_CXXFLAGS "$cflags" --setenv GOPROXY off)
"${sandbox[@]}" "${cgo[@]}" /go/bin/go build -p=2 -mod=readonly -trimpath -tags netgo,osusergo -ldflags "-X main.version=bin-v4.0.2-ivoai.1 -buildid= -w -linkmode external -extldflags '-static -s'" -o /out/caveman-proxy ./proxy/cmd/caveman-proxy
# MCP is pure-Go upstream too; multilingual compression lives in the proxy.
"${sandbox[@]}" --setenv CGO_ENABLED 0 --setenv GOPROXY off /go/bin/go build -p=2 -mod=readonly -trimpath -ldflags '-X main.version=bin-v4.0.2-ivoai.1 -buildid= -w' -o /out/caveman-mcp ./mcp/cmd/caveman-mcp
"${sandbox[@]}" "${cgo[@]}" /go/bin/go test -p=2 -mod=readonly -count=1 -tags netgo,osusergo -ldflags "-linkmode external -extldflags '-static'" -run '^TestReleaseShape' ./engine ./proxy/internal/gateway
"${sandbox[@]}" "${cgo[@]}" /go/bin/go list -mod=readonly -deps -json -tags netgo,osusergo ./proxy/cmd/caveman-proxy ./mcp/cmd/caveman-mcp > "$work/deps.json"
# Module paths are the sandbox's paths; copy notices inside that same sandbox.
cp "$work/deps.json" "$work/build/ivoai-build-deps.json"
"${sandbox[@]}" /usr/bin/python3 /review/build.py licenses /build/ivoai-build-deps.json /out/third-party
for binary in caveman-proxy caveman-mcp; do
 if readelf -l "$work/payload/$binary" | grep -q 'INTERP'; then echo 'Dynamic runtime is forbidden' >&2; exit 1; fi
 if readelf -d "$work/payload/$binary" | grep -q 'NEEDED'; then echo 'Dynamic dependencies are forbidden' >&2; exit 1; fi
done
cp "$work/build/LICENSE" "$work/build/LICENSE-MIT" "$work/build/NOTICE" "$work/build/LICENSING.md" "$work/payload/"
cp "$work/zig/LICENSE" "$work/payload/third-party/LICENSE.zig"
cp "$work/zig/lib/libc/musl/COPYRIGHT" "$work/payload/third-party/COPYRIGHT.musl"
cp "$goroot/LICENSE" "$work/payload/third-party/LICENSE.go"
cp "$work/build/engine/pixel/NOTICE" "$work/payload/third-party/NOTICE.engine-pixel"
cp "$work/build/engine/pixel/assets/SPLEEN_LICENSE.txt" "$work/build/engine/pixel/assets/UNIFONT_LICENSE.txt" "$work/payload/third-party/"
python3 - "$work/payload" "$arch" "$source_sha" "$patch_sha" "$zig_sha" <<'PY'
import json,pathlib,sys
p,arch,source,patch,zig=sys.argv[1:]
v={'schema_version':1,'version':'4.0.2-ivoai.1','upstream_revision':'b4531dfc05a3ef0ba93b57e8bfa5f48564a4b4d6','source_sha256':source,'patch_sha256':patch,'platform':'linux/'+arch,'go':'1.27.3','zig':'0.16.0','zig_sha256':zig,'proxy_cgo':True,'mcp_cgo':False,'static_musl':True,'cpu':'x86-64-v1/no-avx/no-avx2' if arch=='amd64' else 'armv8-a-baseline','signature_status':'not_exposed','attestation_status':'not_exposed','publisher':'IVOAI local rebuild; not an upstream Caveman binary or signature'}
(pathlib.Path(p)/'provenance.json').write_text(json.dumps(v,sort_keys=True,indent=2)+'\n')
PY
archive="$output/ivoai-caveman_linux_${arch}.tar.gz"
python3 "$helper/build.py" package "$work/payload" "$archive" "$arch" "$repo_root"
(cd -- "$output" && sha256sum "$(basename -- "$archive")" > "$(basename -- "$archive").sha256")
printf 'Built %s\n' "$archive"
