#!/usr/bin/env bash
# Exercise the real local App/OAuth/Store as well as the separate UI fixtures.
# No production state, provider authentication, native clients or inference.
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run_root="$(mktemp -d /tmp/ia.XXXXXX)"
readonly repo_root run_root
fixture_pid=""
stage="prepare"

finish() {
  local run_status=$?
  trap - EXIT INT TERM
  if [[ -n "$fixture_pid" ]]; then
    kill -TERM "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  # This also removes the disposable grant, OAuth state, browser homes, reports
  # and private diagnostics. Do not upload raw logs or rendezvous files.
  rm -rf -- "$run_root"
  if (( run_status != 0 )); then
    printf 'ADMIN_BROWSER_CI=FAIL stage=%s exit=%d\n' "$stage" "$run_status" >&2
  fi
  exit "$run_status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$repo_root"
modules="${IVOAI_ADMIN_UI_NODE_MODULES:-$repo_root/website/node_modules}"
export IVOAI_ADMIN_UI_NODE_MODULES="$modules"
if [[ ! -d "$modules" ]]; then
  echo 'Admin browser checks require the existing frozen website dependencies.' >&2
  exit 1
fi
chromium_binary="${IVOAI_ADMIN_UI_CHROMIUM:-}"
if [[ -z "$chromium_binary" ]]; then
  # The preceding CI step installs Playwright's locked Chromium. Resolve that
  # existing distribution without downloading a browser or using a system pin.
  chromium_binary="$(node --input-type=module - <<'NODE'
import { createRequire } from 'node:module';
import { join, resolve } from 'node:path';
const require = createRequire(join(resolve(process.env.IVOAI_ADMIN_UI_NODE_MODULES), '.admin-ci.cjs'));
console.log(require('@playwright/test').chromium.executablePath());
NODE
  )"
fi
if [[ "$chromium_binary" != /* || ! -x "$chromium_binary" ]]; then
  echo 'Admin browser checks require an installed, executable Chromium.' >&2
  exit 1
fi
readonly modules chromium_binary

stage="compile-fixture"
if timeout --kill-after=5s 120s go test -c -o "$run_root/admin.test" ./internal/admin >"$run_root/compile.log" 2>&1; then
  :
else
  exit "$?"
fi
mkdir -m 700 "$run_root/home"

validate_report() {
  local report_file="$1" proof_mode="$2"
  node --input-type=module - "$report_file" "$proof_mode" <<'NODE'
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const report = JSON.parse(readFileSync(process.argv[2], 'utf8'));
const mode = process.argv[3];
if (mode === 'fixture') {
  assert.equal(report.scope, 'hermetic_ui_fixtures_only');
  assert.equal(report.actual_oauth_exchange, false);
  assert.equal(report.actual_backend_store, false);
  assert.equal(report.native_client_execution, false);
  assert.ok(Array.isArray(report.results) && report.results.length >= 6);
} else {
  assert.equal(report.status, 'PASS');
  assert.equal(report.mode, mode);
  assert.equal(report.scope, 'real_local_app_oauth_store_with_seeded_session');
  assert.equal(report.actual_provider_execution, false);
  assert.equal(report.production_state, false);
  assert.equal(report.denied_external_page_requests, 0);
  assert.ok(Array.isArray(report.results) && report.results.length >= 5);
}
NODE
}

stage="hermetic-ui"
if timeout --kill-after=5s 90s env -i PATH="$PATH" HOME="$run_root/home" TMPDIR="$run_root" LANG=C.UTF-8 \
  IVOAI_ADMIN_UI_NODE_MODULES="$modules" IVOAI_ADMIN_UI_CHROMIUM="$chromium_binary" \
  node scripts/test-admin-console.mjs >"$run_root/hermetic.json" 2>"$run_root/hermetic.err"; then
  :
else
  exit "$?"
fi
validate_report "$run_root/hermetic.json" fixture >"$run_root/hermetic-validate.log" 2>&1
echo 'ADMIN_BROWSER_HERMETIC=PASS'

for proof_mode in logout revoke; do
  stage="real-$proof_mode-start"
  rendezvous="$run_root/$proof_mode.json"
  # The native Go fixture receives only disposable state. Its own PASS/exit is
  # required separately from browser success; an absent/skipped fixture fails.
  timeout --kill-after=5s 120s env -i PATH="$PATH" HOME="$run_root/home" TMPDIR="$run_root" LANG=C.UTF-8 \
    IVOAI_ADMIN_BROWSER_FIXTURE="$rendezvous" \
    "$run_root/admin.test" -test.run '^TestLiveAdminBrowserFixture$' -test.count=1 -test.v -test.timeout=115s \
    >"$run_root/$proof_mode-go.log" 2>&1 &
  fixture_pid=$!
  ready=false
  for (( attempt = 0; attempt < 150; attempt++ )); do
    if [[ -s "$rendezvous" ]]; then
      ready=true
      break
    fi
    if ! kill -0 "$fixture_pid" 2>/dev/null; then
      break
    fi
    sleep 0.1
  done
  if [[ "$ready" != true ]]; then
    exit 1
  fi
  stage="real-$proof_mode-browser"
  if timeout --kill-after=5s 90s env -i PATH="$PATH" HOME="$run_root/home" TMPDIR="$run_root" LANG=C.UTF-8 \
    IVOAI_ADMIN_UI_NODE_MODULES="$modules" IVOAI_ADMIN_UI_CHROMIUM="$chromium_binary" \
    IVOAI_ADMIN_BROWSER_FIXTURE="$rendezvous" IVOAI_ADMIN_BROWSER_PROOF_MODE="$proof_mode" \
    node scripts/test-admin-console-live.mjs >"$run_root/$proof_mode.json.log" 2>"$run_root/$proof_mode.err"; then
    :
  else
    exit "$?"
  fi
  validate_report "$run_root/$proof_mode.json.log" "$proof_mode" >"$run_root/$proof_mode-validate.log" 2>&1
  stage="real-$proof_mode-backend-exit"
  if wait "$fixture_pid"; then
    fixture_pid=""
  else
    fixture_status=$?
    fixture_pid=""
    exit "$fixture_status"
  fi
  grep -qF -- '--- PASS: TestLiveAdminBrowserFixture (' "$run_root/$proof_mode-go.log"
  if grep -qF -- '--- SKIP:' "$run_root/$proof_mode-go.log"; then
    exit 1
  fi
  printf 'ADMIN_BROWSER_REAL_%s=PASS backend_exit=0\n' "${proof_mode^^}"
done

stage="complete"
echo 'ADMIN_BROWSER_CI=PASS hermetic=1 real=2 skipped=0'
