#!/bin/sh
set -eu

repo="ivo-lopes/ivoai"
version="${IVOAI_VERSION:-latest}"
install_dir="${IVOAI_INSTALL_DIR:-}"
create_system_link=0

fail() {
  printf 'ivoai installer: %s\n' "$*" >&2
  exit 1
}

info() {
  printf 'ivoai installer: %s\n' "$*"
}

for prerequisite in awk chmod curl dirname find grep id install mkdir mktemp readlink sha256sum stat tar uname; do
  command -v "$prerequisite" >/dev/null 2>&1 || fail "$prerequisite is required"
done

case "$(uname -s)" in
  Linux) os="linux" ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

path_contains() {
  case ":${PATH:-}:" in
    *:"$1":*) return 0 ;;
    *) return 1 ;;
  esac
}

owned_install() {
  target=$1
  ownership="${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/ivoai/ownership.toml"
  [ -f "$ownership" ] && [ ! -L "$ownership" ] || return 1
  [ "$(stat -c '%u' "$ownership" 2>/dev/null)" = "$(id -u)" ] || return 1
  mode="$(stat -c '%a' "$ownership" 2>/dev/null)" || return 1
  [ "$mode" = "600" ] || return 1
  awk -v target="$target" '
    /^\[components\.ivoai\]$/ { section = 1; next }
    /^\[/ { if (section) exit }
    section && /^[[:space:]]*managed[[:space:]]*=/ {
      value = $0; sub(/^[^=]*=[[:space:]]*/, "", value); managed = (value == "true")
    }
    section && /^[[:space:]]*path[[:space:]]*=/ {
      value = $0; sub(/^[^=]*=[[:space:]]*/, "", value)
      quote = substr(value, 1, 1)
      if ((quote == "\"" || quote == "\047") && substr(value, length(value), 1) == quote) {
        value = substr(value, 2, length(value) - 2)
      }
      path = (value == target)
    }
    END { exit !(managed && path) }
  ' "$ownership"
}

version_at_least() {
  current_version=$1
  required_version=$2
  awk -v current="$current_version" -v required="$required_version" 'BEGIN {
    split(current, c, "."); split(required, r, ".")
    for (i = 1; i <= 3; i++) {
      c[i] += 0; r[i] += 0
      if (c[i] > r[i]) exit 0
      if (c[i] < r[i]) exit 1
    }
    exit 0
  }'
}

select_go_toolchain() {
  required_go=$1
  go_command=""
  if command -v go >/dev/null 2>&1; then
    candidate_go="$(command -v go)"
    candidate_version="$(GOTOOLCHAIN=local "$candidate_go" env GOVERSION 2>/dev/null || true)"
    candidate_version=${candidate_version#go}
    if printf '%s\n' "$candidate_version" | grep -Eq '^[0-9]+\.[0-9]+(\.[0-9]+)?$' &&
       version_at_least "$candidate_version" "$required_go"; then
      go_command=$candidate_go
      info "using system Go $candidate_version"
      return
    fi
    info "system Go ${candidate_version:-unknown} is older than required Go $required_go"
  else
    info "Go was not found; bootstrapping Go $required_go for this source build"
  fi

  case "$required_go/$arch" in
    1.27.0/amd64) go_checksum="675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685" ;;
    1.27.0/arm64) go_checksum="51798d2c42d0e1c6ed7fd9f48728b4193abac9e8aad6dbac2fe96a81f5909bda" ;;
    *) fail "no reviewed Go toolchain is pinned for Go $required_go on linux/$arch" ;;
  esac
  go_asset="go${required_go}.linux-${arch}.tar.gz"
  curl -fsSL --retry 3 --connect-timeout 10 "https://go.dev/dl/$go_asset" -o "$tmp_dir/$go_asset" ||
    fail "Go $required_go toolchain download failed"
  printf '%s  %s\n' "$go_checksum" "$tmp_dir/$go_asset" | sha256sum -c - >/dev/null ||
    fail "Go $required_go toolchain checksum mismatch"
  tar -xzf "$tmp_dir/$go_asset" -C "$tmp_dir"
  go_command="$tmp_dir/go/bin/go"
  if ! { [ -f "$go_command" ] && [ -x "$go_command" ] && [ ! -L "$go_command" ]; }; then
    fail "downloaded Go toolchain is incomplete"
  fi
  bootstrapped_version="$(GOTOOLCHAIN=local "$go_command" env GOVERSION 2>/dev/null || true)"
  [ "$bootstrapped_version" = "go$required_go" ] || fail "downloaded Go toolchain version is invalid"
}

if [ -z "$install_dir" ]; then
  if [ "$(id -u)" -eq 0 ]; then
    install_dir="/usr/local/bin"
  elif path_contains "${HOME:?HOME is required}/.local/bin"; then
    install_dir="$HOME/.local/bin"
  elif path_contains "$HOME/bin"; then
    install_dir="$HOME/bin"
  elif command -v sudo >/dev/null 2>&1; then
    install_dir="$HOME/.local/bin"
    create_system_link=1
  else
    fail "neither $HOME/.local/bin nor $HOME/bin is on PATH and sudo is unavailable; add a user bin directory to PATH, then retry"
  fi
fi
case "$install_dir" in
  /*) ;;
  *) fail "installation directory must be an absolute path" ;;
esac

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-install.XXXXXXXX")"
cleanup() {
  chmod -R u+w "$tmp_dir" 2>/dev/null || true
  find "$tmp_dir" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

# An authenticated clone can install itself before a public release exists. Do not
# mistake the caller's current directory for the script directory in `curl | sh`.
source_checkout=0
case "$0" in
  install.sh | */install.sh)
    script_dir="$(CDPATH='' cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)" ||
      fail "cannot resolve installer directory"
    source_checkout=1
    ;;
esac
if [ "$source_checkout" -eq 1 ] && [ -f "$script_dir/go.mod" ] && [ -d "$script_dir/cmd/ivoai" ] &&
   grep -Eq '^module[[:space:]]+github\.com/ivo-lopes/ivoai$' "$script_dir/go.mod"; then
  required_go="$(awk '$1 == "go" && NF == 2 { print $2; exit }' "$script_dir/go.mod")"
  printf '%s\n' "$required_go" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' ||
    fail "go.mod does not declare a supported stable Go version"
  select_go_toolchain "$required_go"
  (cd "$script_dir" && GOTOOLCHAIN=local CGO_ENABLED=0 \
    GOCACHE="$tmp_dir/go-build-cache" GOMODCACHE="$tmp_dir/go-module-cache" \
    "$go_command" build -buildvcs=false -trimpath -o "$tmp_dir/ivoai" ./cmd/ivoai)
else
  asset="ivoai_${os}_${arch}.tar.gz"
  if [ "$version" = "latest" ]; then
    base="https://github.com/${repo}/releases/latest/download"
  else
    printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
      fail "IVOAI_VERSION must be a semantic version such as v0.1.0"
    base="https://github.com/${repo}/releases/download/${version}"
  fi

  curl -fsSL --retry 3 --connect-timeout 10 "$base/$asset" -o "$tmp_dir/$asset" || \
    fail "release download failed (for a private repository, install from an authenticated clone)"
  curl -fsSL --retry 3 --connect-timeout 10 "$base/checksums.txt" -o "$tmp_dir/checksums.txt" || \
    fail "checksum download failed"
  expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$tmp_dir/checksums.txt")"
  case "$expected" in
    *[!0-9a-fA-F]*|'') fail "release checksum is missing or invalid" ;;
  esac
  [ "${#expected}" -eq 64 ] || fail "release checksum is missing or invalid"
  printf '%s  %s\n' "$expected" "$tmp_dir/$asset" | sha256sum -c - >/dev/null || fail "release checksum mismatch"
  tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" ivoai
fi

if ! { [ -f "$tmp_dir/ivoai" ] && [ ! -L "$tmp_dir/ivoai" ]; }; then
  fail "downloaded ivoai is not a regular file"
fi
chmod 0755 "$tmp_dir/ivoai"
[ ! -L "$install_dir" ] || fail "$install_dir is a symlink; refusing installation"
mkdir -p "$install_dir"
if ! { [ -d "$install_dir" ] && [ ! -L "$install_dir" ]; }; then
  fail "$install_dir is not a regular directory"
fi
install_target="$install_dir/ivoai"
if [ -L "$install_target" ]; then
  fail "$install_target is a symlink; refusing to replace it"
fi
if [ -e "$install_target" ] && ! owned_install "$install_target"; then
  fail "$install_target already exists but is not recorded as ivoai-managed; refusing to replace it"
fi
install -m 0755 "$tmp_dir/ivoai" "$install_dir/ivoai"

managed_launcher=""
if [ "$create_system_link" -eq 1 ]; then
  system_link="/usr/local/bin/ivoai"
  if [ -L "$system_link" ]; then
    [ "$(readlink "$system_link")" = "$install_target" ] ||
      fail "$system_link is a symlink to another target; refusing to replace it"
  elif [ -e "$system_link" ]; then
    fail "$system_link already exists and is not an ivoai-managed symlink; refusing to replace it"
  else
    sudo mkdir -p /usr/local/bin
    sudo ln -s "$install_target" "$system_link"
  fi
  managed_launcher="$system_link"
fi

IVOAI_MANAGED_LAUNCHER="$managed_launcher" "$install_dir/ivoai" _register-install

printf 'ivoai installed at %s/ivoai\n' "$install_dir"
if path_contains "$install_dir" || [ "$create_system_link" -eq 1 ]; then
  printf 'Next: ivoai setup\n'
else
  printf 'Run: %s/ivoai setup\n' "$install_dir"
  printf 'Add %s to PATH to use the ivoai command directly.\n' "$install_dir"
fi
