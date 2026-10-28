"""Negative process-contract tests; these fakes are never browser evidence."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[1]


class AdminCIRunnerFailureTests(unittest.TestCase):
    def run_fake_contract(self, case):
        actual_node = shutil.which("node")
        self.assertIsNotNone(actual_node, "the CI runner requires its pinned Node")
        with tempfile.TemporaryDirectory(prefix="ivoai-admin-ci-negative-") as temp:
            private = Path(temp)
            tools = private / "tools"
            tools.mkdir(mode=0o700)
            observed = private / "observed-run-root"
            fixture = textwrap.dedent(
                f"""\
                #!{sys.executable}
                import json, os, pathlib, time
                path = pathlib.Path(os.environ['IVOAI_ADMIN_BROWSER_FIXTURE'])
                path.write_text(json.dumps({{'url':'http://127.0.0.1:1/', 'activation_code':'disposable-test-only'}}))
                path.chmod(0o600)
                for _ in range(80):
                    if pathlib.Path(str(path)+'.stop').exists(): break
                    time.sleep(0.1)
                else: raise SystemExit(24)
                print({'--- SKIP: TestLiveAdminBrowserFixture (0.00s)' if case == 'backend_skip' else '--- PASS: TestLiveAdminBrowserFixture (0.00s)'!r})
                raise SystemExit({23 if case == 'backend_exit' else 0})
                """
            )
            fake_go = textwrap.dedent(
                f"""\
                #!{sys.executable}
                import pathlib, sys
                if {case == 'compile_exit'!r}: raise SystemExit(17)
                target = pathlib.Path(sys.argv[sys.argv.index('-o')+1])
                pathlib.Path({str(observed)!r}).write_text(str(target.parent))
                target.write_text({fixture!r})
                target.chmod(0o700)
                """
            )
            fake_node = textwrap.dedent(
                f"""\
                #!{sys.executable}
                import json, os, pathlib, sys
                if sys.argv[1].startswith('--input-type='):
                    os.execv({actual_node!r}, [{actual_node!r}]+sys.argv[1:])
                if {case == 'browser_exit'!r}: raise SystemExit(19)
                if sys.argv[1].endswith('test-admin-console.mjs'):
                    report = {{'scope':'hermetic_ui_fixtures_only', 'actual_oauth_exchange':False, 'actual_backend_store':False, 'native_client_execution':False, 'results':['fake']*6}}
                else:
                    report = {{'status':'PASS', 'mode':os.environ['IVOAI_ADMIN_BROWSER_PROOF_MODE'], 'scope':'real_local_app_oauth_store_with_seeded_session', 'actual_provider_execution':False, 'production_state':False, 'denied_external_page_requests':0, 'results':['fake']*5}}
                    pathlib.Path(os.environ['IVOAI_ADMIN_BROWSER_FIXTURE']+'.stop').touch(mode=0o600)
                print(json.dumps(report))
                """
            )
            for name, body in [("go", fake_go), ("node", fake_node), ("chromium", "#!/bin/sh\nexit 1\n")]:
                path = tools / name
                path.write_text(body)
                path.chmod(0o700)
            env = {
                "PATH": str(tools) + os.pathsep + os.defpath,
                "HOME": str(private),
                "IVOAI_ADMIN_UI_NODE_MODULES": str(private),
                "IVOAI_ADMIN_UI_CHROMIUM": str(tools / "chromium"),
            }
            result = subprocess.run(
                ["bash", str(ROOT / "scripts/test-admin-console-ci.sh")],
                cwd=ROOT, env=env, capture_output=True, text=True, timeout=15,
            )
            self.assertNotIn("ADMIN_BROWSER_CI=PASS", result.stdout)
            self.assertNotIn("ADMIN_BROWSER_REAL_LOGOUT=PASS", result.stdout)
            self.assertNotIn("disposable-test-only", result.stdout + result.stderr)
            if observed.exists():
                self.assertFalse(Path(observed.read_text()).exists(), "private run workspace leaked")
            return result

    def test_backend_failure_overrides_successful_browser_report(self):
        result = self.run_fake_contract("backend_exit")
        self.assertEqual(result.returncode, 23)
        self.assertIn("stage=real-logout-backend-exit", result.stderr)

    def test_skipped_backend_is_not_success(self):
        result = self.run_fake_contract("backend_skip")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stage=real-logout-backend-exit", result.stderr)

    def test_fixture_compile_failure_is_not_success(self):
        result = self.run_fake_contract("compile_exit")
        self.assertEqual(result.returncode, 17)
        self.assertIn("stage=compile-fixture", result.stderr)

    def test_browser_failure_is_not_success(self):
        result = self.run_fake_contract("browser_exit")
        self.assertEqual(result.returncode, 19)
        self.assertIn("stage=hermetic-ui", result.stderr)


if __name__ == "__main__":
    unittest.main()
