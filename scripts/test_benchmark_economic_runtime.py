"""Benchmark gate regressions; fake executables never call a provider."""
import contextlib
import importlib.util
import io
import json
import os
import sys
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
               Correct=True, JSONDecoded=True, Execution="read_only", EditValidated=False, OracleToolsSHA256="", NativeWorkerObservation="PARTIAL_CLIENT_PROJECTION_TOTALS_UNKNOWN")
    row['NativeEventMetrics'] = {"schema_version":1, "stream_status":"complete", "coverage":"partial_client_projection",
        "tool_completions":dict.fromkeys(("command_execution","mcp_tool_call","file_change","web_search","collab_tool_call"),0),
        "repeated_exact_payloads":dict.fromkeys(("command_execution","mcp_tool_call"),0),
        "collaboration_completions":dict.fromkeys(("spawn_agent","send_input","wait","close_agent"),0),
        "reported_spawn_receivers":None,"unclassified_errors":0,"duplicate_terminal_items":0,"unknown_events":0,"unknown_items":0,
        "native_workers":None,"native_delegations":None,"native_retries":None}
    row.update(changes)
    return row


def admitted_row(capabilities, **changes):
    row = valid_row(Admission={"version": 1, "source_wired": True, "state": "ADMITTED",
        "reason": "ACCEPTED_UNQUALIFIED", "probe_attempts": 1, "inventory_attempts": 1,
        "model_attempts": 1, "automatic_fallbacks": 0, "owned_reaped": True, "native_wait_zero": True,
        "scope_matched": True, "probe_ms": 1, "inventory_ms": 1},
        AdmissionMS=2, AdmissionSourceSHA256=capabilities["admission"]["source_sha256"], TotalMS=102)
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

    def test_gateway_event_fields_survive_json_and_legacy_absence_is_not_zero(self):
        fields = dict(GatewayCodeResolves=3, GatewayCodeCacheHits=1,
                      GatewayCodeCacheMisses=1, GatewayCodeCacheUnknown=1,
                      GatewayObservationBasis="COMPLETED_SESSION_EVENTS_NOT_ECONOMY",
                      GatewayCacheCondition="FRESH_APP_SESSION_NO_PREWARM")
        row = self.read([valid_row(PrefetchCalls=1, CacheHits=0, **fields)])
        self.assertTrue(row["MeasurementValid"])
        self.assertEqual({key: row[key] for key in fields}, fields)
        self.assertEqual(row["PrefetchCalls"], 1)
        self.assertEqual(row["CacheHits"], 0)
        legacy = self.read([valid_row()])
        self.assertTrue(legacy["MeasurementValid"])
        self.assertTrue(all(key not in legacy for key in fields))
        for change in ({"GatewayCodeResolves": True}, {"GatewayCodeCacheHits": -1},
                       {"GatewayCodeCacheUnknown": None}, {"GatewayCodeResolves": 4},
                       {"GatewayObservationBasis": "FLAG_ENABLED"},
                       {"GatewayCacheCondition": "WARM_ASSUMED"}):
            with self.subTest(change=change):
                self.assertFalse(self.read([valid_row(**(fields | change))])["MeasurementValid"])
        for missing in fields:
            with self.subTest(missing=missing):
                partial = {key: value for key, value in fields.items() if key != missing}
                self.assertFalse(self.read([valid_row(**partial)])["MeasurementValid"])

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

    def test_native_observations_are_enum_only_partial_and_fail_closed(self):
        for mutate in (lambda v:v.update(native_workers=0), lambda v:v.update(native_retries=1),
                       lambda v:v.update(private="SECRET"), lambda v:v.update(schema_version=True),
                       lambda v:v['tool_completions'].update(SECRET=1),
                       lambda v:v['repeated_exact_payloads'].update(command_execution=1),
                       lambda v:v.update(stream_status="partial"),
                       lambda v:v.update(reported_spawn_receivers=0)):
            value=valid_row();mutate(value['NativeEventMetrics'])
            row=self.read([value]);self.assertFalse(row['MeasurementValid']);self.assertNotIn('SECRET',json.dumps(row))
        value=valid_row(Correct=False);value['NativeEventMetrics']['stream_status']='partial'
        row=self.read([value]);self.assertTrue(row['MeasurementValid']);self.assertFalse(row['Correct'])


    def test_edit_answer_requires_bound_executable_patch_verdict(self):
        scenario = "small_known_file_correction"
        capabilities = {"catalog_sha256": "1"*64, "harness_sha256": "2"*64,
                        "fixtures": {"go-clamp-edit": "3"*64}, "oracle_tools": {scenario: "4"*64}}
        good = valid_row(scenario, Execution="workspace_edit", EditValidated=True, EditFiles=1,
                         OracleToolsSHA256="4"*64, CatalogSHA256="1"*64,
                         HarnessSHA256="2"*64, FixtureSHA256="3"*64,
                         ValidationMS=20, TotalMS=120)
        for changes in ({}, {"EditValidated": False}, {"EditFiles": 0}, {"EditFiles": 3},
                        {"OracleToolsSHA256": "SECRET"}, {"Execution": "read_only"},
                        {"ValidationMS": 1000}):
            with self.subTest(changes=changes):
                self.path.write_text(json.dumps([{**good, **changes}]))
                row = BENCHMARK.read_evidence(self.path, scenario, "A", 0, 0, "a"*64, capabilities)
                self.assertEqual(row["Correct"], not changes)
                self.assertEqual(row["MeasurementValid"], not changes)
                self.assertNotIn("SECRET", json.dumps(row))


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
        self.assertIsNone(summary["workers"])
        self.assertIsNone(summary["delegations"])
        self.assertEqual(summary["native_worker_observation"], "PARTIAL_CLIENT_PROJECTION_TOTALS_UNKNOWN")

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


class ProcessSamplingTests(unittest.TestCase):
    @unittest.skipUnless(sys.platform.startswith("linux"), "Linux procfs sampler")
    def test_existing_runner_observes_only_its_live_subtree(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, resources=BENCHMARK.run_attempt(["/usr/bin/python3","-c", "import subprocess,time; p=subprocess.Popen(['/bin/sleep','0.5']); p.wait()"],
                {"PATH":"/usr/bin:/bin","HOME":tmp}, Path(tmp)/"resource.json", timeout=3, sample_processes=True)
            self.assertEqual(code,0)
            self.assertGreaterEqual(resources['ProcessSampling']['sampled_process_count_peak'],2)
            self.assertIsNone(resources['ProcessSampling']['actual_process_total'])
            self.assertNotIn('argv',json.dumps(resources))


class DriverTests(unittest.TestCase):
    def test_driver_allowlist_preserves_auth_scope_without_inheriting_secrets(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            temporary = home / "tmp"
            temporary.mkdir()
            safe = {"HOME": str(home), "CODEX_HOME": str(home / "official-auth-scope"),
                    "PATH": "/fixture/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
                    "TZ": "UTC", "TERM": "dumb", "TMPDIR": str(temporary),
                    "XDG_CONFIG_HOME": str(home / "config"), "XDG_RUNTIME_DIR": str(home / "runtime"),
                    "DBUS_SESSION_BUS_ADDRESS": "unix:path=" + str(home / "bus")}
            blocked = ("OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY", "AZURE_OPENAI_API_KEY",
                       "GITHUB_TOKEN", "GH_TOKEN", "GOOGLE_API_KEY", "GEMINI_API_KEY", "AWS_SECRET_ACCESS_KEY",
                       "OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_ENDPOINT", "MCP_SERVER_TOKEN",
                       "IVOAI_SERVER_TOKEN", "LD_PRELOAD", "LD_LIBRARY_PATH", "PYTHONPATH", "PYTHONSTARTUP",
                       "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy",
                       "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "LC_PRIVATE_TOKEN")
            fixture = {**safe, **{key: "PRIVATE_ENV_SENTINEL" for key in blocked},
                       "GOMAXPROCS": "999", "IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY": "1",
                       "IVOAI_GRAFT_TEST_DIST": "PRIVATE_ENV_SENTINEL",
                       "IVOAI_GRAFT_PREFETCH_DIAGNOSTICS": "PRIVATE_ENV_SENTINEL"}
            captured = []
            original = BENCHMARK.run_attempt

            def capture(command, environment, *args, **kwargs):
                captured.append(environment.copy())
                return original(command, environment, *args, **kwargs)

            with mock.patch.dict(os.environ, fixture, clear=True), \
                    mock.patch.object(BENCHMARK, "run_attempt", side_effect=capture):
                status, _ = self.execute("valid")
            self.assertEqual(status, 0)  # Synthetic test processes, no client.
            self.assertEqual(len(captured), 8)
            discovery = [env for env in captured if "IVOAI_BENCHMARK_CATALOG_OUTPUT" in env]
            execution = [env for env in captured if "IVOAI_ECONOMIC_RUNTIME_ARM" in env]
            self.assertEqual(len(discovery), 2)
            self.assertEqual(len(execution), 6)
            for environment in discovery:
                self.assertNotIn("CODEX_HOME", environment)
                self.assertNotEqual(environment["HOME"], str(home))
                self.assertFalse(set(blocked) & environment.keys())
                self.assertNotIn("PRIVATE_ENV_SENTINEL", environment.values())
            for environment in execution:
                for key, value in safe.items():
                    if key != "TMPDIR":  # Driver owns each comparison's private temporary root.
                        self.assertEqual(environment[key], value)
                self.assertTrue(Path(environment["TMPDIR"]).is_absolute())
                self.assertEqual(environment["GOMAXPROCS"], "2")
                self.assertEqual(environment["IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY"], "1")
                self.assertFalse(set(blocked) & environment.keys())
                self.assertNotIn("PRIVATE_ENV_SENTINEL", environment.values())
                self.assertNotIn("IVOAI_GRAFT_PREFETCH_DIAGNOSTICS", environment)

    def execute(self, mode):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        fake = root / "fake-test"
        capabilities = BENCHMARK.benchmark_catalog.expected_capabilities(
            BENCHMARK.benchmark_catalog.ROOT / "internal/app/primary_prefetch_live_test.go")
        fixture_names = {s.id: s.fixture for s in BENCHMARK.benchmark_catalog.load_catalog()}
        # This test program prints a secret sentinel to prove output suppression.
        fake.write_text("#!/usr/bin/python3\nimport os,sys,json,hashlib\nfrom pathlib import Path\n"
                        "print('SECRET')\nprint('SECRET',file=sys.stderr)\n"
                        f"capabilities={capabilities!r}\nfixtures={fixture_names!r}\n"
                        "if any('TestPrimaryPrefetchBenchmarkCatalog' in arg for arg in sys.argv):\n"
                        " Path(os.environ['IVOAI_BENCHMARK_CATALOG_OUTPUT']).write_text(json.dumps(capabilities));sys.exit(0)\n"
                        "arm=os.environ['IVOAI_ECONOMIC_RUNTIME_ARM']\n"
                        "scenario=next(x for x in sys.argv if x.startswith('-test.run=')).split('/')[1]\n"
                        "output=Path(os.environ['IVOAI_GRAFT_PREFETCH_OUTPUT'])\n"
                        f"mode={mode!r}\n"
                        f"row={admitted_row(capabilities)!r}\n" +
                        "row.update(Scenario=scenario,Arm=arm,Private='SECRET',Error='SECRET')\n"
                        "row.update(CatalogSHA256=capabilities['catalog_sha256'],HarnessSHA256=capabilities['harness_sha256'],FixtureSHA256=capabilities['fixtures'][fixtures[scenario]])\n"
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
        argv.extend(["--filtered-environment-permitted", "--distribution", str(distribution), "--output", str(output), "--repetitions", "1"])
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
        self.assertEqual(manifest["host_mcp_tools_exposed"], 0)
        self.assertIsNone(manifest["native_tools_exposed"])
        self.assertEqual(manifest["native_tools_inventory"], "UNVERIFIED")
        self.assertFalse(manifest["process_sampling"]["enabled"])
        self.assertEqual(manifest["native_event_observation_schema"], 1)
        self.assertNotIn("tools_exposed", manifest)
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
                    except (FileNotFoundError, ProcessLookupError):
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

class CommonAdmissionEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'row.json'
        self.capabilities = BENCHMARK.benchmark_catalog.expected_capabilities(
            BENCHMARK.benchmark_catalog.ROOT / 'internal/app/primary_prefetch_live_test.go')

    def row(self):
        row = admitted_row(self.capabilities)
        row.update(CatalogSHA256=self.capabilities['catalog_sha256'],
                   HarnessSHA256=self.capabilities['harness_sha256'],
                   FixtureSHA256=self.capabilities['fixtures']['go-core'])
        return row

    def read(self, row, code=0):
        self.path.write_text(json.dumps([row]))
        return BENCHMARK.read_evidence(self.path, 'api', 'A', 0, code, 'a'*64, self.capabilities)

    def test_fresh_admission_and_noauth_refusal_keep_closed_outcomes(self):
        passed = self.read(self.row())
        self.assertTrue(passed['Correct'])
        self.assertEqual(passed['Admission']['model_attempts'], 1)
        self.assertEqual(passed['ClientExecution'], 'ATTEMPTED')
        failed = self.row()
        failed.update(Correct=False, JSONDecoded=False, Private='SECRET')
        for key in BENCHMARK.OPTIONAL_COUNTS: failed[key] = None
        failed['Admission'].update(state='NOT_RUN', reason='AUTH_DENIED', inventory_attempts=0,
                                   model_attempts=0, scope_matched=False, inventory_ms=0)
        clean = self.read(failed, 1)
        self.assertFalse(clean['Correct'])
        self.assertFalse(clean['MeasurementValid'])
        self.assertEqual(clean['ClientExecution'], 'NOT_RUN')
        self.assertEqual(clean['Admission']['reason'], 'AUTH_DENIED')
        self.assertNotIn('SECRET', json.dumps(clean))
        summary = BENCHMARK.totals([clean])
        self.assertEqual(summary['model_attempts'], 0)
        self.assertEqual(summary['model_not_run_rows'], 1)

    def test_unbound_old_forged_partial_or_replayed_admission_rejected(self):
        for changes in ({'version': True}, {'source_wired': 1}, {'source_wired': False},
                        {'native_wait_zero': False}, {'owned_reaped': False}, {'scope_matched': False},
                        {'probe_attempts': 0}, {'inventory_attempts': 0}, {'model_attempts': 0},
                        {'model_attempts': True}, {'automatic_fallbacks': 1}, {'reason': []},
                        {'reason': 'SECRET'}, {'private': 'SECRET'}):
            with self.subTest(changes=changes):
                row = self.row(); row['Admission'].update(changes)
                clean = self.read(row)
                self.assertFalse(clean['MeasurementValid'])
                self.assertNotIn('SECRET', json.dumps(clean))
        for key in self.row()['Admission']:
            row=self.row();del row['Admission'][key]
            self.assertFalse(self.read(row)['MeasurementValid'])
        for field in ('Admission', 'AdmissionMS', 'AdmissionSourceSHA256'):
            row=self.row();del row[field]
            self.assertFalse(self.read(row)['MeasurementValid'])
        row=self.row();row['AdmissionSourceSHA256']='f'*64
        self.assertFalse(self.read(row)['MeasurementValid'])
        row=self.row();row['TotalMS']=100
        self.assertFalse(self.read(row)['MeasurementValid'])


if __name__ == "__main__":
    unittest.main()
