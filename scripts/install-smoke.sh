#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-smoke.XXXXXXXX")"

cleanup() {
  # Go's module cache is intentionally read-only. Restore owner write access so
  # the isolated HOME can be removed without noisy best-effort failures.
  chmod -R u+w "$smoke_root" 2>/dev/null || true
  rm -rf -- "$smoke_root"
}
trap cleanup EXIT

fail() {
  printf 'install smoke (%s): %s\n' "${IVOAI_SMOKE_TARGET:-local}" "$*" >&2
  exit 1
}

export HOME="$smoke_root/home"
export XDG_CONFIG_HOME="$smoke_root/xdg/config"
export XDG_DATA_HOME="$smoke_root/xdg/data"
export XDG_STATE_HOME="$smoke_root/xdg/state"
export XDG_CACHE_HOME="$smoke_root/xdg/cache"
export CODEX_HOME="$smoke_root/vendor/codex"
export CLAUDE_CONFIG_DIR="$smoke_root/vendor/claude"
export IVOAI_INSTALL_DIR="$smoke_root/bin"
export IVOAI_TEST_MODE=1
export PATH="$IVOAI_INSTALL_DIR:$PATH"

mkdir -p "$HOME" "$IVOAI_INSTALL_DIR" "$CODEX_HOME" "$CLAUDE_CONFIG_DIR"

cd "$repo_root"
./install.sh >/dev/null
[[ -x "$IVOAI_INSTALL_DIR/ivoai" ]] || fail "install.sh did not install an executable"

first_binary_hash="$(sha256sum "$IVOAI_INSTALL_DIR/ivoai" | awk '{print $1}')"
./install.sh >/dev/null
second_binary_hash="$(sha256sum "$IVOAI_INSTALL_DIR/ivoai" | awk '{print $1}')"
[[ "$first_binary_hash" == "$second_binary_hash" ]] || fail "reinstall changed the binary unexpectedly"

unmanaged_dir="$smoke_root/unmanaged-bin"
mkdir -p "$unmanaged_dir"
printf 'pre-existing-user-file\n' >"$unmanaged_dir/ivoai"
if IVOAI_INSTALL_DIR="$unmanaged_dir" ./install.sh >/dev/null 2>&1; then
  fail "install.sh replaced an unowned pre-existing executable"
fi
grep -Fq 'pre-existing-user-file' "$unmanaged_dir/ivoai" || fail "unowned executable contents changed"

ivoai setup >/dev/null

config="$XDG_CONFIG_HOME/ivoai/config.toml"
secrets="$XDG_CONFIG_HOME/ivoai/secrets.json"
ownership="$XDG_STATE_HOME/ivoai/ownership.toml"
state="$XDG_STATE_HOME/ivoai/state.toml"

for required in "$config" "$secrets" "$ownership" "$state"; do
  [[ -f "$required" ]] || fail "setup did not create ${required#"$smoke_root"/}"
done

[[ "$(stat -c '%a' "$secrets")" == "600" ]] || fail "secret file mode is not 0600"
for private_dir in \
  "$XDG_CONFIG_HOME/ivoai" \
  "$XDG_DATA_HOME/ivoai" \
  "$XDG_STATE_HOME/ivoai" \
  "$XDG_CACHE_HOME/ivoai"; do
  [[ "$(stat -c '%a' "$private_dir")" == "700" ]] || fail "private directory mode is not 0700: $private_dir"
done

config_hash="$(sha256sum "$config" | awk '{print $1}')"
secrets_hash="$(sha256sum "$secrets" | awk '{print $1}')"
ownership_hash="$(sha256sum "$ownership" | awk '{print $1}')"

ivoai setup >/dev/null

[[ "$config_hash" == "$(sha256sum "$config" | awk '{print $1}')" ]] || fail "second setup changed configuration"
[[ "$secrets_hash" == "$(sha256sum "$secrets" | awk '{print $1}')" ]] || fail "second setup changed secrets"
[[ "$ownership_hash" == "$(sha256sum "$ownership" | awk '{print $1}')" ]] || fail "second setup changed ownership"

if find "$XDG_DATA_HOME/ivoai/hooks" -mindepth 1 -print -quit | grep -q .; then
  fail "test-mode setup unexpectedly duplicated or installed live hooks"
fi

status_output="$(ivoai status)"
for expected in \
  "ivoai          ready" \
  "Codex          installed / not connected" \
  "Claude Code    installed / not connected" \
  "Headroom       installed / enabled / interactive not validated" \
  "Context        not configured" \
  "ai-memory      installed / offline hooks" \
  "Ruflo          ready / provider execution disabled" \
  "Server         not configured" \
  "Overall: READY"; do
  grep -Fq "$expected" <<<"$status_output" || fail "status is missing: $expected"
done

doctor_output="$(ivoai doctor)"
grep -Fq "Overall: READY" <<<"$doctor_output" || fail "human doctor is not READY"
grep -Fq "Secret permissions: 0600" <<<"$doctor_output" || fail "human doctor did not verify secret permissions"
grep -Fq "hooks=true" <<<"$doctor_output" || fail "human doctor did not verify ai-memory hooks"

doctor_json="$(ivoai doctor --json)"
grep -Eq '"overall"[[:space:]]*:[[:space:]]*"READY"' <<<"$doctor_json" || fail "JSON doctor is not READY"
grep -Eq '"test_mode"[[:space:]]*:[[:space:]]*true' <<<"$doctor_json" || fail "JSON doctor is not in test mode"
grep -Eq '"secret_permissions"[[:space:]]*:[[:space:]]*"0600"' <<<"$doctor_json" || fail "JSON doctor did not verify secret permissions"

export IVOAI_SERVER_ROOT="$smoke_root/server"
ivoai setup --mode server >/dev/null

server_config="$IVOAI_SERVER_ROOT/etc/ivoai/server.toml"
server_compose="$IVOAI_SERVER_ROOT/etc/ivoai/compose.yaml"
server_gateway_unit="$IVOAI_SERVER_ROOT/etc/systemd/system/ivoai-gateway.service"
server_context_unit="$IVOAI_SERVER_ROOT/etc/systemd/system/ivoai-context.service"
server_dependencies_unit="$IVOAI_SERVER_ROOT/etc/systemd/system/ivoai-dependencies.service"

for managed_file in \
  "$server_config" \
  "$server_compose" \
  "$server_gateway_unit" \
  "$server_context_unit" \
  "$server_dependencies_unit"; do
  [[ -f "$managed_file" ]] || fail "server setup did not create $managed_file"
done
grep -Fq 'user: "1000:1000"' "$server_compose" || fail "ai-memory is not configured with a non-root container identity"
if grep -Fq 'user: "0:0"' "$server_compose"; then
  fail "server compose configures a root dependency container"
fi

server_hash_before="$(sha256sum \
  "$server_config" \
  "$server_compose" \
  "$server_gateway_unit" \
  "$server_context_unit" \
  "$server_dependencies_unit" | sha256sum | awk '{print $1}')"
ivoai setup --mode server >/dev/null
server_hash_after="$(sha256sum \
  "$server_config" \
  "$server_compose" \
  "$server_gateway_unit" \
  "$server_context_unit" \
  "$server_dependencies_unit" | sha256sum | awk '{print $1}')"
[[ "$server_hash_before" == "$server_hash_after" ]] || fail "second server setup changed managed assets"

server_doctor="$(ivoai server doctor)"
grep -Fq "ivoai server: configured" <<<"$server_doctor" || fail "server doctor did not report configured"
grep -Fq "services: test-mode" <<<"$server_doctor" || fail "server doctor did not preserve test-mode isolation"

printf 'install smoke (%s): PASS\n' "${IVOAI_SMOKE_TARGET:-local}"
