#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
matrix_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-upgrade-matrix.XXXXXXXX")"

cleanup() {
  chmod -R u+w "$matrix_root" 2>/dev/null || true
  rm -rf -- "$matrix_root"
}
trap cleanup EXIT

fail() {
  printf 'upgrade matrix: %s\n' "$*" >&2
  exit 1
}

command -v go >/dev/null 2>&1 || fail "Go is required"
command -v git >/dev/null 2>&1 || fail "Git is required"
git -C "$repo_root" rev-parse --verify 'refs/tags/v0.5.0^{commit}' >/dev/null 2>&1 || fail "tag v0.5.0 is unavailable"

GOMODCACHE="$(go env GOMODCACHE)"
GOCACHE="$(go env GOCACHE)"
export GOMODCACHE GOCACHE

mkdir -p "$matrix_root/source-v050/cmd/upgrade-matrix" "$matrix_root/bin" "$matrix_root/home" \
  "$matrix_root/provider/codex" "$matrix_root/provider/claude"
git -C "$repo_root" archive v0.5.0 | tar -x -C "$matrix_root/source-v050"
cp "$repo_root/scripts/upgrade-matrix-v050/main.go" "$matrix_root/source-v050/cmd/upgrade-matrix/main.go"

(cd "$matrix_root/source-v050" && go build -trimpath -ldflags '-X main.version=0.5.0' -o "$matrix_root/bin/ivoai-v050" ./cmd/ivoai)
(cd "$repo_root" && go build -trimpath -ldflags '-X main.version=0.5.1' -o "$matrix_root/bin/ivoai-candidate" ./cmd/ivoai)
(cd "$matrix_root/source-v050" && go build -trimpath -o "$matrix_root/bin/v050-updater" ./cmd/upgrade-matrix)

export HOME="$matrix_root/home"
export XDG_CONFIG_HOME="$matrix_root/xdg/config"
export XDG_DATA_HOME="$matrix_root/xdg/data"
export XDG_STATE_HOME="$matrix_root/xdg/state"
export XDG_CACHE_HOME="$matrix_root/xdg/cache"
export CODEX_HOME="$matrix_root/provider/codex"
export CLAUDE_CONFIG_DIR="$matrix_root/provider/claude"
export IVOAI_TEST_MODE=1
export IVOAI_INSTALL_DIR="$matrix_root/install"
mkdir -p "$IVOAI_INSTALL_DIR"

printf 'provider-owned-codex\n' >"$CODEX_HOME/auth-marker"
printf 'provider-owned-claude\n' >"$CLAUDE_CONFIG_DIR/auth-marker"
provider_before="$(sha256sum "$CODEX_HOME/auth-marker" "$CLAUDE_CONFIG_DIR/auth-marker")"

install -m 0755 "$matrix_root/bin/ivoai-v050" "$IVOAI_INSTALL_DIR/ivoai"
"$IVOAI_INSTALL_DIR/ivoai" setup >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" doctor --json >/dev/null
printf '\n[compatibility_fixture]\nfuture_field = "preserve-me"\n' >>"$XDG_CONFIG_HOME/ivoai/config.toml"
printf '%s\n' '{"server":{"token":"typed-placeholder","client_id":"legacy-client","scopes":["context:read"]}}' >"$XDG_CONFIG_HOME/ivoai/secrets.json"
chmod 0600 "$XDG_CONFIG_HOME/ivoai/secrets.json"

rollback="$XDG_STATE_HOME/ivoai/updates/ivoai.previous"
mkdir -p "$(dirname -- "$rollback")"
"$matrix_root/bin/v050-updater" "$matrix_root/bin/ivoai-candidate" "$IVOAI_INSTALL_DIR/ivoai" "$rollback"
[[ "$("$IVOAI_INSTALL_DIR/ivoai" version)" == "0.5.1" ]] || fail "published updater did not promote the candidate"
"$IVOAI_INSTALL_DIR/ivoai" setup >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" status >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" doctor --json >/dev/null
grep -Eq "future_field[[:space:]]*=[[:space:]]*['\"]preserve-me['\"]" "$XDG_CONFIG_HOME/ivoai/config.toml" || fail "candidate erased an unknown config field"
grep -Eq '"schema"[[:space:]]*:[[:space:]]*2' "$XDG_CONFIG_HOME/ivoai/secrets.json" || fail "candidate did not migrate the v0.5 secret store"
grep -q '"srv_legacy_default"' "$XDG_CONFIG_HOME/ivoai/secrets.json" || fail "candidate did not create the default server credential profile"
grep -q '"token": "typed-placeholder"' "$XDG_CONFIG_HOME/ivoai/secrets.json" || fail "candidate lost the legacy server credential"

# With no new-format journal, the candidate must consume the legacy rollback
# binary, restore v0.5.0, and reconcile setup/doctor.
"$IVOAI_INSTALL_DIR/ivoai" update --rollback >/dev/null
[[ "$("$IVOAI_INSTALL_DIR/ivoai" version)" == "0.5.0" ]] || fail "candidate rollback did not restore v0.5.0"
"$IVOAI_INSTALL_DIR/ivoai" doctor --json >/dev/null
grep -q '"server"' "$XDG_CONFIG_HOME/ivoai/secrets.json" || fail "rollback bridge lost the v0.5 server credential field"
grep -q '"token": "typed-placeholder"' "$XDG_CONFIG_HOME/ivoai/secrets.json" || fail "v0.5 rollback cannot read the original server credential"

provider_after="$(sha256sum "$CODEX_HOME/auth-marker" "$CLAUDE_CONFIG_DIR/auth-marker")"
[[ "$provider_before" == "$provider_after" ]] || fail "provider-owned authentication data changed"

# Prove update-after-rollback using the same historical updater core.
"$matrix_root/bin/v050-updater" "$matrix_root/bin/ivoai-candidate" "$IVOAI_INSTALL_DIR/ivoai" "$rollback"
"$IVOAI_INSTALL_DIR/ivoai" setup >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" doctor --json >/dev/null

(cd "$repo_root" && go test -count=1 ./internal/migration ./internal/update ./internal/app -run 'Transaction|UpdateContext|ServerUpdate|ServerRollback|CommittedRollback')

# Repeat the historical bridge against a server-only installation. The v0.5.0
# updater invoked plain `ivoai setup` after promotion, so the candidate must
# auto-detect the existing server marker and must consume the rollback binary
# from the legacy client-XDG slot without creating a client configuration.
export HOME="$matrix_root/server-home"
export XDG_CONFIG_HOME="$matrix_root/server-xdg/config"
export XDG_DATA_HOME="$matrix_root/server-xdg/data"
export XDG_STATE_HOME="$matrix_root/server-xdg/state"
export XDG_CACHE_HOME="$matrix_root/server-xdg/cache"
export CODEX_HOME="$matrix_root/server-provider/codex"
export CLAUDE_CONFIG_DIR="$matrix_root/server-provider/claude"
export IVOAI_INSTALL_DIR="$matrix_root/server-install"
export IVOAI_SERVER_ROOT="$matrix_root/server-root"
mkdir -p "$HOME" "$IVOAI_INSTALL_DIR" "$CODEX_HOME" "$CLAUDE_CONFIG_DIR"

install -m 0755 "$matrix_root/bin/ivoai-v050" "$IVOAI_INSTALL_DIR/ivoai"
"$IVOAI_INSTALL_DIR/ivoai" setup --mode server >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" server doctor >/dev/null

server_rollback="$XDG_STATE_HOME/ivoai/updates/ivoai.previous"
mkdir -p "$(dirname -- "$server_rollback")"
"$matrix_root/bin/v050-updater" "$matrix_root/bin/ivoai-candidate" "$IVOAI_INSTALL_DIR/ivoai" "$server_rollback"
[[ "$("$IVOAI_INSTALL_DIR/ivoai" version)" == "0.5.1" ]] || fail "published updater did not promote the server candidate"
"$IVOAI_INSTALL_DIR/ivoai" setup >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" server doctor >/dev/null
[[ ! -e "$XDG_CONFIG_HOME/ivoai/config.toml" ]] || fail "candidate server bridge created client configuration"

"$IVOAI_INSTALL_DIR/ivoai" update --rollback >/dev/null
[[ "$("$IVOAI_INSTALL_DIR/ivoai" version)" == "0.5.0" ]] || fail "candidate server rollback did not restore v0.5.0"
"$IVOAI_INSTALL_DIR/ivoai" server doctor >/dev/null

"$matrix_root/bin/v050-updater" "$matrix_root/bin/ivoai-candidate" "$IVOAI_INSTALL_DIR/ivoai" "$server_rollback"
"$IVOAI_INSTALL_DIR/ivoai" setup >/dev/null
"$IVOAI_INSTALL_DIR/ivoai" server doctor >/dev/null

printf 'upgrade matrix client+server v0.5.0 updater -> candidate -> rollback -> candidate: PASS\n'
