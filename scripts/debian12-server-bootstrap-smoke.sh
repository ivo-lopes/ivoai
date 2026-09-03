#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
docker_cli="${IVOAI_TEST_DOCKER_CLI:-docker}"
container="ivoai-debian12-lxc-smoke-$$-${RANDOM}"
work="$(mktemp -d)"
trap '"$docker_cli" rm -f "$container" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

command -v "$docker_cli" >/dev/null

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$work/ivoai" "$repo_root/cmd/ivoai"
"$docker_cli" run --privileged --network host --detach --name "$container" debian:12-slim sleep infinity >/dev/null
"$docker_cli" cp "$work/ivoai" "$container:/usr/local/bin/ivoai"

# This disposable shim models the LXC guest's systemd control plane while the
# nested Docker daemon is real. The first official-package enable starts
# dockerd; all IVOAI service lifecycle calls remain bounded no-op fixtures.
# This script is evaluated inside the container.
# shellcheck disable=SC2016
"$docker_cli" exec "$container" sh -eu -c '
  mkdir -p /run/systemd/system /usr/local/bin
  printf "lxc\n" >/run/systemd/container
  printf "%s\n" \
    "#!/bin/sh" \
    "case \"\$*\" in" \
    "  *docker.service*)" \
    "    if command -v dockerd >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then" \
    "      nohup dockerd --host=unix:///var/run/docker.sock --storage-driver=vfs --bridge=none --iptables=false --ip-forward=false --ip-masq=false >/tmp/dockerd.log 2>&1 &" \
    "      tries=0" \
    "      until docker info >/dev/null 2>&1; do tries=\$((tries+1)); [ \$tries -lt 60 ] || { cat /tmp/dockerd.log >&2; exit 1; }; sleep 1; done" \
    "    fi" \
    "    ;;" \
    "esac" \
    "if [ \"\${1:-}\" = is-active ]; then printf \"active\\n\"; fi" \
    "exit 0" >/usr/local/bin/systemctl
  chmod 0755 /usr/local/bin/systemctl
'

"$docker_cli" exec "$container" ivoai setup --mode server >"$work/first.txt"
grep -q '^OS_SUPPORTED=true$' "$work/first.txt"
grep -q '^LXC_DETECTED=true$' "$work/first.txt"
grep -q '^DOCKER_CLI_PRESENT=false$' "$work/first.txt"
grep -q 'Docker Engine is absent; provisioning from Docker' "$work/first.txt"

"$docker_cli" exec "$container" ivoai setup --mode server >"$work/second.txt"
grep -q '^DOCKER_CLI_PRESENT=true$' "$work/second.txt"
grep -q '^DOCKER_DAEMON_REACHABLE=true$' "$work/second.txt"
grep -q '^DOCKER_COMPOSE_V2_PRESENT=true$' "$work/second.txt"
grep -q 'setup complete' "$work/second.txt"

# These assertions run inside the container.
# shellcheck disable=SC2016
"$docker_cli" exec "$container" sh -eu -c '
  docker version --format "{{.Server.Version}}" | grep -Eq "^(2[89]|[3-9][0-9])\."
  docker compose version --short | grep -Eq "^(v)?([3-9]|[1-9][0-9])\."
  test -s /etc/ivoai/docs.json
  test "$(stat -c %a /etc/ivoai/docs.json)" = 640
  test -s /etc/systemd/system/ivoai-docs.service
  grep -q "User=ivoai-docs" /etc/systemd/system/ivoai-docs.service
  grep -q "ExecStart=/usr/local/bin/ivoai server docs serve" /etc/systemd/system/ivoai-docs.service
  test -s /etc/ivoai/secrets/qdrant.env
  test -s /etc/ivoai/secrets/memory.env
'

printf 'SERVER_BOOTSTRAP_DEBIAN12_LXC=PASS\n'
