"""Benchmark gate regressions; fake executables never call a provider."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import signal
import time
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("economic_runtime", Path(__file__).with_name("benchmark-economic-runtime.py"))
BENCHMARK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BENCHMARK)


def valid_row(scenario="api", arm="A", **changes):
    row = {key: 0 for key in BENCHMARK.COUNTS}
    row.update({key: None for key in BENCHMARK.OPTIONAL_COUNTS})
    row.update(Scenario=scenario, Arm=arm, Model="gpt-6.1-sol", Effort="low",
               DistributionInventorySHA256="a" * 64, TotalMS=100, ProviderMS=90,
               PrefetchMS=10, InputTokens=100, CachedInputTokens=60, OutputTokens=5,
               Correct=True, JSONDecoded=True)
    row.update(changes)
    return row


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "row.json"

    def read(self, payload, code=0):
        self.path.write_text(json.dumps(payload))
        return BENCHMARK.read_evidence(self.path, "api", "A", 0, code, "a" * 64)

    def test_valid_scalar_allowlist_and_failed_exit(self):
        row = self.read([valid_row(Private="SECRET", Error="SECRET")])
        self.assertTrue(row["Correct"])
        self.assertTrue(row["MeasurementValid"])
        self.assertNotIn("SECRET", json.dumps(row))
        failed = self.read([valid_row()], code=7)
        self.assertFalse(failed["Correct"])
        self.assertTrue(failed["MeasurementValid"])
        self.assertEqual(failed["InputTokens"], 100)
        self.assertEqual(failed["Error"], "test_execution_failed")

    def test_invalid_verdict_identity_and_measurements(self):
        for payload in ([], None, {}, [valid_row(), valid_row()], [True],
                        [valid_row(Arm="B")], [valid_row(Scenario="implementation")],
                        [valid_row(Correct="true")], [valid_row(JSONDecoded=1)],
                        [valid_row(InputTokens=True)], [valid_row(InputTokens=-1)],
                        [valid_row(InputTokens=float("nan"))], [valid_row(InputTokens=1.5)],
                        [valid_row(InputTokens=1 << 54)], [valid_row(CachedInputTokens=101)],
                        [valid_row(OutputTokens=None)], [valid_row(Model="SECRET")],
                        [valid_row(DistributionInventorySHA256="b" * 64)],
                        [valid_row(TotalMS=1)], [valid_row(TotalMS=0)]):
            with self.subTest(payload=payload):
                row = self.read(payload)
                self.assertFalse(row["Correct"])
                self.assertFalse(row["MeasurementValid"])
                self.assertEqual(row["Error"], "invalid_evidence")
                self.assertNotIn("SECRET", json.dumps(row))

    def test_missing_malformed_duplicate_and_oversized_evidence(self):
        self.assertEqual(BENCHMARK.read_evidence(self.path, "api", "A", 0, 0, "a" * 64)["Error"], "no_evidence")
        for body in ("SECRET", '{"Arm":"A","Arm":"B"}', " " * (BENCHMARK.MAX_EVIDENCE_BYTES + 1)):
            self.path.write_text(body)
            row = BENCHMARK.read_evidence(self.path, "api", "A", 0, 0, "a" * 64)
            self.assertFalse(row["Correct"])
            self.assertEqual(row["Error"], "invalid_evidence")

    def test_symlink_evidence_is_not_followed(self):
        secret = self.path.with_name("private")
        secret.write_text("SECRET")
        self.path.symlink_to(secret)
        row = BENCHMARK.read_evidence(self.path, "api", "A", 0, 0, "a" * 64)
        self.assertEqual(row["Error"], "invalid_evidence")
        BENCHMARK.write_json(self.path, [row])
        self.assertEqual(secret.read_text(), "SECRET")
        self.assertFalse(self.path.is_symlink())
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)

    def test_failed_attempt_can_keep_unknown_metrics(self):
        row = self.read([valid_row(Correct=False, JSONDecoded=False, InputTokens=None,
                                   CachedInputTokens=None, OutputTokens=None, Error="SECRET")])
        self.assertTrue(row["MeasurementValid"])
        self.assertFalse(row["Correct"])
        self.assertIsNone(row["InputTokens"])
        self.assertEqual(row["Error"], "correctness_failed")


class SummaryTests(unittest.TestCase):
    def rows(self):
        rows = []
        for repetition in range(3):
            for scenario in BENCHMARK.SCENARIOS:
                for arm in ("A", "B"):
                    row = valid_row(scenario, arm, InputTokens=100 if arm == "A" else 80,
                                    CachedInputTokens=50, TotalMS=100 if arm == "A" else 90,
                                    ProviderMS=80, PrefetchMS=10)
                    row.update(Repetition=repetition, TestExitCode=0, MeasurementValid=True)
                    rows.append(row)
        return rows

    def test_subset_can_pass_without_claiming_global_completion(self):
        summary = BENCHMARK.summarize(self.rows(), 3)
        self.assertEqual(summary["subset_economic_diagnostic"], "PASS")
        self.assertEqual(summary["global_economic_gate"], "INCOMPLETE")
        self.assertEqual(summary["matched_pairs"]["count"], 9)
        self.assertEqual(summary["matched_pairs"]["reduction_percent"], {"InputTokens": 20, "TotalMS": 10})
        self.assertEqual(summary["all_attempts"]["A"]["InputTokens"]["sum"], 900)
        self.assertEqual(summary["all_attempts"]["A"]["CachedInputTokens"]["sum"], 450)
        self.assertIsNone(summary["monetary_cost"])

    def test_failures_remain_in_all_attempts_and_out_of_matched_pairs(self):
        rows = self.rows()
        rows[0].update(Correct=False, InputTokens=1000)
        summary = BENCHMARK.summarize(rows, 3)
        self.assertEqual(summary["all_attempts"]["A"]["attempts"], 9)
        self.assertEqual(summary["all_attempts"]["A"]["InputTokens"]["sum"], 1800)
        self.assertEqual(summary["matched_pairs"]["count"], 8)
        self.assertEqual(summary["subset_economic_diagnostic"], "INCOMPLETE")

    def test_negative_result_and_repeat_consistency_are_not_hidden(self):
        rows = self.rows()
        for row in rows:
            if row["Arm"] == "B":
                row["InputTokens"] = 120
        summary = BENCHMARK.summarize(rows, 3)
        self.assertEqual(summary["matched_pairs"]["reduction_percent"]["InputTokens"], -20)
        self.assertEqual(summary["subset_economic_diagnostic"], "FAIL")
        rows = self.rows()
        for row in rows:
            if row["Arm"] == "B":
                row["InputTokens"] = 0 if row["Repetition"] == 0 else 101
                row["CachedInputTokens"] = 0
                row["TotalMS"] = 1 if row["Repetition"] == 0 else 101
        summary = BENCHMARK.summarize(rows, 3)
        self.assertEqual(summary["improved_repetitions"], 1)
        self.assertEqual(summary["subset_economic_diagnostic"], "FAIL")

    def test_missing_rows_and_single_repetition_are_incomplete(self):
        self.assertEqual(BENCHMARK.summarize([], 3)["subset_economic_diagnostic"], "INCOMPLETE")
        rows = [row for row in self.rows() if row["Repetition"] == 0]
        self.assertEqual(BENCHMARK.summarize(rows, 1)["subset_economic_diagnostic"], "INCOMPLETE")


class DriverTests(unittest.TestCase):
    def execute(self, mode):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        fake = root / "fake-test"
        # This test program prints a secret sentinel to prove output suppression.
        fake.write_text("#!/usr/bin/python3\nimport os,sys,json,hashlib\nfrom pathlib import Path\n"
                        "print('SECRET')\nprint('SECRET',file=sys.stderr)\n"
                        "arm=os.environ['IVOAI_ECONOMIC_RUNTIME_ARM']\n"
                        "scenario=next(x for x in sys.argv if x.startswith('-test.run=')).split('/')[1]\n"
                        "output=Path(os.environ['IVOAI_GRAFT_PREFETCH_OUTPUT'])\n"
                        f"mode={mode!r}\n"
                        f"row={valid_row()!r}\n" +
                        "row.update(Scenario=scenario,Arm=arm,Private='SECRET',Error='SECRET')\n"
                        "row['DistributionInventorySHA256']=hashlib.sha256((Path(os.environ['IVOAI_GRAFT_TEST_DIST'])/'files.json').read_bytes()).hexdigest()\n"
                        "if mode=='no_test': sys.exit(0)\n"
                        "if mode=='empty_failed': output.write_text('[]'); sys.exit(7)\n"
                        "if mode=='malformed_A' and arm=='A': output.write_text('SECRET'); sys.exit(0)\n"
                        "output.write_text(json.dumps([row]))\n")
        fake.chmod(0o700)
        distribution = root / "distribution"
        distribution.mkdir()
        (distribution / "files.json").write_text("{}")
        output = root / "evidence"
        argv = []
        for name in ("baseline", "candidate", "codex", "node", "sandbox-binary"):
            argv.extend(["--" + name, str(fake)])
        argv.extend(["--distribution", str(distribution), "--output", str(output), "--repetitions", "1"])
        captured = io.StringIO()
        with contextlib.redirect_stdout(captured):
            status = BENCHMARK.main(argv)
        self.assertNotIn("SECRET", captured.getvalue())
        for path in output.glob("*.json"):
            self.assertNotIn("SECRET", path.read_text())
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        return status, output

    def test_no_test_is_failure(self):
        status, output = self.execute("no_test")
        self.assertEqual(status, 1)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual(len(rows), 6)
        self.assertTrue(all(row["Error"] == "no_evidence" for row in rows))

    def test_empty_evidence_nonzero_exit_does_not_pass_or_stop_following_attempts(self):
        status, output = self.execute("empty_failed")
        self.assertEqual(status, 1)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual(len(rows), 6)
        self.assertTrue(all(row["TestExitCode"] == 7 and not row["Correct"] for row in rows))

    def test_malformed_arm_continues_and_ab_ba_is_preserved(self):
        status, output = self.execute("malformed_A")
        self.assertEqual(status, 1)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual([row["Arm"] for row in rows], ["A", "B", "B", "A", "A", "B"])
        self.assertEqual(sum(row["Correct"] for row in rows), 3)
        self.assertTrue(all(not row["Correct"] for row in rows if row["Arm"] == "A"))

    def test_valid_fake_execution_records_provenance_and_resources(self):
        status, output = self.execute("valid")
        self.assertEqual(status, 0)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["runtime_flags"], {"experimental_evidence_gateway": os.environ.get("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY") == "1"})
        for name in ("baseline_test_sha256", "candidate_test_sha256", "codex_sha256", "node_sha256",
                     "sandbox_sha256", "distribution_inventory_sha256", "harness_sha256"):
            self.assertEqual(len(manifest[name]), 64)
        summary = json.loads((output / "summary.json").read_text())
        self.assertEqual(summary["global_economic_gate"], "INCOMPLETE")
        rows = json.loads((output / "runtime-comparison.json").read_text())
        for row in rows:
            self.assertIn(row["LocalResourceSource"], ("unavailable", "linux_gnu_time_wait4_reaped_descendants", "linux_wait4_reaped_descendants_max_rss_not_sum"))
            if row["LocalResourceSource"] != "unavailable":
                self.assertGreater(row["LocalPeakRSSBytes"], 0)

    @unittest.skipUnless(hasattr(os, "wait4") and os.sys.platform.startswith("linux"), "Linux wait4 resource accounting")
    def test_native_wait4_fallback_measures_owned_pid_and_reaps_once(self):
        processes = []
        original_popen = BENCHMARK.subprocess.Popen

        def capture(*args, **kwargs):
            process = original_popen(*args, **kwargs)
            processes.append(process)
            return process

        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.object(BENCHMARK.Path, "is_file", return_value=False), \
                mock.patch.object(BENCHMARK.subprocess, "Popen", side_effect=capture):
            code, resources = BENCHMARK.run_attempt(
                ["/usr/bin/python3", "-c", "sum(i*i for i in range(100000)); raise SystemExit(3)"],
                os.environ.copy(), Path(directory) / "resource.json")
        self.assertEqual(code, 3)
        self.assertEqual(processes[0].returncode, 3)
        self.assertEqual(resources["LocalResourceSource"], "linux_wait4_reaped_descendants_max_rss_not_sum")
        self.assertGreater(resources["LocalPeakRSSBytes"], 0)
        self.assertGreater(resources["LocalElapsedMS"], 0)
        self.assertGreaterEqual(resources["LocalUserCPUMS"], 0)
        with self.assertRaises(ChildProcessError):
            os.waitpid(processes[0].pid, os.WNOHANG)

    @unittest.skipUnless(hasattr(os, "wait4") and os.sys.platform.startswith("linux"), "Linux process group accounting")
    def test_native_wait4_timeout_kills_descendant_group(self):
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.object(BENCHMARK.Path, "is_file", return_value=False):
            child_pid_file = Path(directory) / "child.pid"
            program = ("import subprocess,time; from pathlib import Path; "
                       "child=subprocess.Popen(['/usr/bin/python3','-c','import time;time.sleep(30)']); "
                       f"Path({str(child_pid_file)!r}).write_text(str(child.pid)); time.sleep(30)")
            code, resources = BENCHMARK.run_attempt(["/usr/bin/python3", "-c", program],
                os.environ.copy(), Path(directory) / "resource.json", timeout=1)
            self.assertEqual(code, 124)
            self.assertGreater(resources["LocalElapsedMS"], 0)
            child_pid = int(child_pid_file.read_text())
            deadline = time.monotonic() + 1
            try:
                while time.monotonic() < deadline:
                    try:
                        state = (Path("/proc") / str(child_pid) / "stat").read_text().rsplit(")", 1)[1].split()[0]
                    except FileNotFoundError:
                        break
                    if state == "Z":  # killed orphan may await its init reaper
                        break
                    time.sleep(0.01)
                else:
                    self.fail("timed-out command left an executing descendant")
            finally:
                try:
                    os.kill(child_pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass

    def test_runtime_flag_manifest_is_explicit_boolean(self):
        with mock.patch.dict(os.environ, {"IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY": "1"}):
            status, output = self.execute("valid")
            self.assertEqual(status, 0)
            manifest = json.loads((output / "manifest.json").read_text())
            self.assertEqual(manifest["runtime_flags"], {"experimental_evidence_gateway": True})

    def test_missing_executable_is_recorded(self):
        with tempfile.TemporaryDirectory() as directory:
            code, _ = BENCHMARK.run_attempt([str(Path(directory) / "absent")], os.environ.copy(), Path(directory) / "resource.json")
            self.assertEqual(code, 127)

    def test_timeout_is_recorded(self):
        with tempfile.TemporaryDirectory() as directory:
            code, _ = BENCHMARK.run_attempt(["/usr/bin/python3", "-c", "import time; time.sleep(30)"],
                                             os.environ.copy(), Path(directory) / "resource.json", timeout=0.05)
            self.assertEqual(code, 124)


if __name__ == "__main__":
    unittest.main()
