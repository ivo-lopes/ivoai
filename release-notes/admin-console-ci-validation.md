# Administrative browser integration in CI

This follows the first local read-only administrative slice for IVOAI-189. The
quality job now runs `scripts/test-admin-console-ci.sh` after its existing locked
Playwright Chromium installation. The job's45-minute budget is unchanged; the new
step has an eight-minute bound. No new npm package, global installation or native
client is added by this step.

The runner first compiles the existing Go browser fixture once, then executes the
hermetic UI assertions and two separate real local App/OAuth/Store browser flows:
logout and external grant revocation. These real flows still use seeded synthetic
session metadata and no model inference or production state. They remain distinct
from the hermetic UI assertions. Browser exit zero alone is insufficient: the
runner validates each report's scope/mode/result fields, waits for the corresponding
Go fixture's exit zero and requires its exact PASS marker without SKIP.

The runner reuses `website/node_modules` from the frozen documentation lockfile.
Without an explicit local override, it resolves
`require('@playwright/test').chromium.executablePath()` and requires the preceding
CI step to have installed that executable. It does not silently use system
Chromium, install a browser or skip on missing dependencies.

Temporary state is in a short private `/tmp/ia.XXXXXX` root to keep Chromium Unix
socket paths below their length limit. Each actual fixture gets a fresh disposable
grant and private native/browser homes. Node/Go fixture environments contain only
explicit test variables, PATH, locale and those private paths. The shell never
reads or prints the activation code. Browser and backend output is redirected to
private diagnostics; stdout contains fixed PASS/FAIL labels only. Exit/signal traps
stop the active fixture and remove that root, including rendezvous, OAuth state,
screenshots and diagnostics. No raw fixture artifacts are uploaded. Compilation,
browser execution, fixture startup polling and backend exit are bounded.

`scripts/test_admin_console_ci.py` adds four independent process-contract tests to
the existing Python discovery step. Explicit fake Go/Node processes demonstrate
that a successful browser report followed by backend exit23, a skipped backend,
compile exit17 and browser exit19 cannot produce an aggregate PASS. These tests
are checks of failure propagation, not browser or backend-integration evidence.

Local validation on source base4098848d used Go1.27.3, Node26.11.1 and the already
installed Playwright1.64.0 tooling, with an explicit system Chromium156.0.8078.47
override. The complete runner passed: one hermetic run, real logout and real grant
revocation, both backend exits zero, no skipped fixture. All four negative
process-contract tests passed, as did `bash -n` and `git diff --check`. The default
resolver found no Playwright browser distribution installed locally and refused
that separate preflight with exit1; it was not counted as a successful browser
run. The CI-installed browser distribution itself remains to be exercised by CI.
ShellCheck is not installed locally; the existing mandatory CI ShellCheck step
includes the new shell script. No Go/backend/UI source or prior evidence file was
changed. Sanitized stdout and source/log hashes are in
`evidence/admin-console/ci-runner-validation.json`.
