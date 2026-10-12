#!/usr/bin/env bash
# This installs no production policy. --check-only never changes AppArmor.
set -euo pipefail
case ${1:-} in
  --check-only) ;;
  '')
    [[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_OS:-} == Linux ]] || {
      echo 'AppArmor setup is restricted to ephemeral GitHub Linux runners' >&2
      exit 1
    }
    repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
    sudo install -m 0644 "$repo_root/patches/graft/ci-bwrap.apparmor" /etc/apparmor.d/bwrap
    sudo /usr/sbin/apparmor_parser --replace /etc/apparmor.d/bwrap
    ;;
  *) echo 'usage: setup-ci-graft-sandbox.sh [--check-only]' >&2; exit 2 ;;
esac
# Fail before downloading/building anything if UID maps or network isolation fail.
outer_network=$(/usr/bin/readlink /proc/self/ns/net)
inner_network=$(/usr/bin/bwrap --die-with-parent --new-session --unshare-all --clearenv \
  --ro-bind /usr /usr --ro-bind /lib /lib --ro-bind-try /lib64 /lib64 \
  --proc /proc --dev /dev --tmpfs /tmp /usr/bin/readlink /proc/self/ns/net)
[[ -n $inner_network && $inner_network != "$outer_network" ]]
printf '%s\n' 'Graft sandbox UID-map and private network preflight: PASS'
