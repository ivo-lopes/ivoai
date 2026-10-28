#!/usr/bin/env bash
# Real managed HTTP/TUI and negative plugin contracts with no external network.
set -euo pipefail
umask 077
archive=${1:?usage: test-managed-opencode-offline.sh ARCHIVE METADATA}
metadata=${2:?usage: test-managed-opencode-offline.sh ARCHIVE METADATA}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
command -v bwrap >/dev/null
[[ $(jq -er '.version' "$metadata") == 1.19.0-ivoai.3 ]]
printf '%s  %s\n' "$(jq -er '.sha256' "$metadata")" "$archive" | sha256sum --check --status
work=$(mktemp -d "${TMPDIR:-/tmp}/ivoai-opencode-offline.XXXXXXXX")
trap 'printf "Managed offline validation workspace: %s\n" "$work" >&2' EXIT
tar -xzf "$archive" -C "$work" opencode
mkdir -m 700 "$work/home"
cp "$repo_root/scripts/test-managed-opencode-policy.py" "$work/policy.py"
cd -- "$repo_root"
go test -c -o "$work/bridge.test" ./internal/opencodebridge
go test -c -o "$work/agents.test" ./internal/agents
# Reuse the repository's CI-only AppArmor admission; local callers configure
# their own supported sandbox. Namespace failure is an error, never a SKIP.
outer_network=$(readlink /proc/self/ns/net)
inner_network=$(bwrap --die-with-parent --unshare-all --clearenv --ro-bind /usr /usr --ro-bind /lib /lib --ro-bind-try /lib64 /lib64 --proc /proc --dev /dev --tmpfs /tmp /usr/bin/readlink /proc/self/ns/net)
[[ -n "$inner_network" && "$inner_network" != "$outer_network" ]]
common=(--die-with-parent --unshare-all --clearenv --ro-bind /usr /usr --symlink usr/bin /bin --ro-bind /lib /lib --ro-bind-try /lib64 /lib64 --proc /proc --dev /dev --tmpfs /tmp --dir /etc --bind "$work" /proof --chdir /proof --setenv PATH /usr/bin:/bin --setenv HOME /proof/home --setenv TMPDIR /tmp --setenv LANG C.UTF-8 --setenv TERM xterm-256color --setenv IVOAI_LIVE_OPENCODE_PATH /proof/opencode --setenv IVOAI_LIVE_OPENCODE_VERSION 1.19.0-ivoai.3 --setenv IVOAI_LIVE_OPENCODE_TUI 1 --setenv IVOAI_OPENCODE_PRIVATE_NETWORK 1)
bwrap "${common[@]}" /usr/bin/python3 /proof/policy.py /proof/opencode /proof/policy > "$work/policy.json"
for spec in 'bridge:TestLiveManagedOpenCode(RoutesPromptThroughIVOAI|AttachRendersIVOAIPlugin|Permissions|API|ResizeKeyboard)' 'agents:TestLive(OpenCodeExecutor(HTTPContract|NativeLifecycle)|NativeOpenCodeAUTOExecutor)'; do
  IFS=: read -r package pattern <<< "$spec"
  bwrap "${common[@]}" "/proof/$package.test" -test.run "^($pattern)$" -test.count=1 -test.v -test.timeout=6m > "$work/$package.log" 2>&1
  grep -q '^PASS$' "$work/$package.log"
  if grep -q -- '--- SKIP:' "$work/$package.log"; then
    echo 'Required offline managed test skipped' >&2
    exit 1
  fi
  cat "$work/$package.log"
done
# Root inside its own user namespace exercises the same flag, not DAC denial.
bwrap --uid 0 --gid 0 "${common[@]}" /usr/bin/python3 /proof/policy.py /proof/opencode /proof/root-policy > "$work/root-policy.json"
cat "$work/policy.json" "$work/root-policy.json"
printf '%s\n' 'Managed OpenCode offline HTTP/TUI and plugin-policy contracts: PASS'
