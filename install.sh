#!/bin/sh
set -eu

repo="ivo-lopes/ivoai"
version="${IVOAI_VERSION:-latest}"
install_dir="${IVOAI_INSTALL_DIR:-}"
create_system_link=0
interactive=0
use_color=0
use_unicode=0
active_step_pid=""

if [ -t 1 ] && [ "${TERM:-dumb}" != "dumb" ] && [ -z "${CI:-}" ] && [ "${IVOAI_NO_ANIMATION:-0}" != "1" ]; then
  interactive=1
  [ -z "${NO_COLOR:-}" ] && use_color=1
fi
case "${LC_ALL:-}${LC_CTYPE:-}${LANG:-}" in
  *UTF-8*|*utf-8*|*UTF8*|*utf8*) [ "${IVOAI_ASCII:-0}" != "1" ] && use_unicode=1 ;;
esac

if [ "$use_color" -eq 1 ]; then
  c_reset='\033[0m'
  c_cyan='\033[38;5;81m'
  c_violet='\033[38;5;141m'
  c_green='\033[38;5;77m'
  c_yellow='\033[38;5;220m'
  c_red='\033[38;5;203m'
  c_dim='\033[38;5;245m'
else
  c_reset='' c_cyan='' c_violet='' c_green='' c_yellow='' c_red='' c_dim=''
fi

banner() {
  [ "$interactive" -eq 1 ] || return 0
  columns=80
  if command -v tput >/dev/null 2>&1; then
    detected_columns="$(tput cols 2>/dev/null || true)"
    case "$detected_columns" in *[!0-9]*|'') ;; *) columns=$detected_columns ;; esac
  fi
  if [ "$columns" -lt 46 ]; then
    printf '%bivoai%b\n\n' "$c_cyan" "$c_reset"
  elif [ "$columns" -lt 90 ]; then
    printf '%b%s%b\n\n' "$c_cyan" ' ___ _   _  ___   _  ___
|_ _| | | |/ _ \ / \|_ _|
 | || |_| | (_) / _ \| |
|___|\___/ \___/_/ \_\___|' "$c_reset"
  else
    printf '%b%s\n%b%s%b\n\n' "$c_cyan" '██╗██╗   ██╗ ██████╗  █████╗ ██╗
██║██║   ██║██╔═══██╗██╔══██╗██║
██║██║   ██║██║   ██║███████║██║
██║╚██╗ ██╔╝██║   ██║██╔══██║██║
██║ ╚████╔╝ ╚██████╔╝██║  ██║██║' "$c_violet" '╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝╚═╝' "$c_reset"
  fi
}

fail() {
  printf '%b%s%b %s\n' "$c_red" "$(status_icon failure)" "$c_reset" "$*" >&2
  exit 1
}

info() {
  printf '%b%s%b %s\n' "$c_cyan" "$(status_icon info)" "$c_reset" "$*"
}

success() {
  printf '%b%s%b %s\n' "$c_green" "$(status_icon success)" "$c_reset" "$*"
}

step() {
  printf '%b%s%b %s\n' "$c_violet" "$(status_icon step)" "$c_reset" "$*"
}

status_icon() {
  if [ "$interactive" -eq 0 ] || [ "$use_unicode" -ne 1 ]; then
    case "$1" in success) printf '[OK]' ;; failure) printf '[ERROR]' ;; info) printf '[INFO]' ;; *) printf '[..]' ;; esac
  else
    case "$1" in success) printf '✓' ;; failure) printf '✗' ;; info) printf '•' ;; *) printf '→' ;; esac
  fi
}

run_step() {
  step_label=$1
  shift
  if [ "$interactive" -eq 0 ]; then
    step "$step_label"
    set +e
    "$@"
    step_status=$?
    set -e
    if [ "$step_status" -ne 0 ]; then
      fail "$step_label failed"
    fi
    success "$step_label"
    return
  fi
  step_log="$tmp_dir/step.log"
  : >"$step_log"
  "$@" >"$step_log" 2>&1 &
  step_pid=$!
  active_step_pid=$step_pid
  step_index=0
  while kill -0 "$step_pid" 2>/dev/null; do
    case $((step_index % 4)) in 0) frame='|' ;; 1) frame='/' ;; 2) frame='-' ;; *) frame="\\" ;; esac
    printf '\r\033[2K%b%s%b %s' "$c_cyan" "$frame" "$c_reset" "$step_label"
    step_index=$((step_index + 1))
    sleep 0.1
  done
  set +e
  wait "$step_pid"
  step_status=$?
  set -e
  active_step_pid=""
  printf '\r\033[2K'
  if [ "$step_status" -ne 0 ]; then
    cat "$step_log" >&2
    fail "$step_label failed"
  fi
  success "$step_label"
}

download() {
  download_url=$1
  download_target=$2
  download_label=$3
  step "$download_label"
  if [ "$interactive" -eq 1 ]; then
    curl -fL --progress-bar --retry 3 --connect-timeout 10 "$download_url" -o "$download_target" || fail "$download_label failed"
  else
    curl -fsSL --retry 3 --connect-timeout 10 "$download_url" -o "$download_target" || fail "$download_label failed"
  fi
  success "$download_label"
}

banner
printf '%bSecure client and server installer%b\n\n' "$c_dim" "$c_reset"

for prerequisite in awk cat chmod curl dirname find grep id install kill mkdir mktemp readlink sha256sum sleep stat tar uname; do
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
      info "Using system Go $candidate_version"
      return
    fi
    info "System Go ${candidate_version:-unknown} is older than required Go $required_go"
  else
    info "Go was not found; bootstrapping verified Go $required_go"
  fi

  case "$required_go/$arch" in
    1.27.0/amd64) go_checksum="675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685" ;;
    1.27.0/arm64) go_checksum="51798d2c42d0e1c6ed7fd9f48728b4193abac9e8aad6dbac2fe96a81f5909bda" ;;
    *) fail "no reviewed Go toolchain is pinned for Go $required_go on linux/$arch" ;;
  esac
  go_asset="go${required_go}.linux-${arch}.tar.gz"
  download "https://go.dev/dl/$go_asset" "$tmp_dir/$go_asset" "Download Go $required_go toolchain"
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
handle_signal() {
  signal_status=$1
  trap - HUP INT TERM
  if [ -n "$active_step_pid" ]; then
    kill -TERM "$active_step_pid" 2>/dev/null || true
    wait "$active_step_pid" 2>/dev/null || true
    active_step_pid=""
  fi
  printf '\n%b%s%b Installation cancelled.\n' "$c_yellow" "$(status_icon failure)" "$c_reset" >&2
  exit "$signal_status"
}
trap cleanup EXIT
trap 'handle_signal 129' HUP
trap 'handle_signal 130' INT
trap 'handle_signal 143' TERM

info "Platform: $os/$arch"
info "Install directory: $install_dir"

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
  build_source() {
    (cd "$script_dir" && GOTOOLCHAIN=local CGO_ENABLED=0 \
    GOCACHE="$tmp_dir/go-build-cache" GOMODCACHE="$tmp_dir/go-module-cache" \
    "$go_command" build -buildvcs=false -trimpath -o "$tmp_dir/ivoai" ./cmd/ivoai)
  }
  run_step "Build ivoai from the source checkout" build_source
else
  asset="ivoai_${os}_${arch}.tar.gz"
  if [ "$version" = "latest" ]; then
    base="https://github.com/${repo}/releases/latest/download"
  else
    printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
      fail "IVOAI_VERSION must be a semantic version such as v0.1.0"
    base="https://github.com/${repo}/releases/download/${version}"
  fi

  download "$base/$asset" "$tmp_dir/$asset" "Download ivoai release"
  download "$base/checksums.txt" "$tmp_dir/checksums.txt" "Download release checksums"
  expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$tmp_dir/checksums.txt")"
  case "$expected" in
    *[!0-9a-fA-F]*|'') fail "release checksum is missing or invalid" ;;
  esac
  [ "${#expected}" -eq 64 ] || fail "release checksum is missing or invalid"
  verify_release() { printf '%s  %s\n' "$expected" "$tmp_dir/$asset" | sha256sum -c - >/dev/null; }
  extract_release() { tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" ivoai; }
  run_step "Verify release checksum" verify_release
  run_step "Extract ivoai binary" extract_release
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
install_binary() { install -m 0755 "$tmp_dir/ivoai" "$install_dir/ivoai"; }
run_step "Install ivoai binary" install_binary

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

register_install() { IVOAI_MANAGED_LAUNCHER="$managed_launcher" "$install_dir/ivoai" _register-install; }
run_step "Register managed installation" register_install

printf '\n%bInstallation complete%b\n' "$c_green" "$c_reset"
printf '  Binary: %s/ivoai\n\n' "$install_dir"
if path_contains "$install_dir" || [ "$create_system_link" -eq 1 ]; then
  if [ "$(id -u)" -eq 0 ]; then
    printf '%bNext commands%b\n  ivoai setup --mode server\n  ivoai server doctor\n' "$c_violet" "$c_reset"
  else
    printf '%bNext commands%b\n  ivoai setup\n  ivoai doctor\n' "$c_violet" "$c_reset"
  fi
else
  printf '%bNext commands%b\n  %s/ivoai setup\n  %s/ivoai doctor\n' "$c_violet" "$c_reset" "$install_dir" "$install_dir"
  printf '%bTip:%b add %s to PATH to use ivoai directly.\n' "$c_yellow" "$c_reset" "$install_dir"
fi
