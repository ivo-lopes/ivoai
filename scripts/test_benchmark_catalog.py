"""Offline matrix/discovery regressions; subprocesses are synthetic, never LLMs."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

import benchmark_catalog as CATALOG
from test_benchmark_economic_runtime import valid_row, admitted_row

SPEC = importlib.util.spec_from_file_location("matrix_driver", Path(__file__).with_name("benchmark-economic-runtime.py"))
DRIVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DRIVER)


class CatalogTests(unittest.TestCase):
    def test_exact_historical_matrix_and_explicit_limits(self):
        cases = CATALOG.load_catalog()
        self.assertEqual(len(cases), 16)
        self.assertEqual([s.label for s in cases], ["Small known-file correction", "Function identification",
            "Cross-module refactor", "Dependencies", "Impact", "APIs", "Large project / IVOAI", "Monorepo",
            "Unsupported parser", "Institutional documentation", "ai-memory history", "Non-code tasks",
            "Multiple workers", "Reuse between turns", "Interrupted continuity", "Concurrent distinct projects"])
        self.assertEqual(sum(s.runner_supported for s in cases), 9)
        self.assertEqual(sum(CATALOG.planned_status(s) == "BLOCKED" for s in cases), 2)
        self.assertEqual(sum(CATALOG.planned_status(s) == "UNSUPPORTED" for s in cases), 5)
        self.assertEqual(DRIVER.SCENARIOS, ("api", "implementation", "multi_module_callers"))
        self.assertTrue(all(s.expected is None for s in cases if not s.runner_supported))

    def test_hashes_bind_every_project_format(self):
        capabilities = CATALOG.expected_capabilities(CATALOG.ROOT / "internal/app/primary_prefetch_live_test.go")
        self.assertEqual(capabilities["direct_mcp_helper_configuration_scope"], "USER_CONFIG_INCLUDED_SHARED_SCOPE")
        self.assertIs(capabilities["direct_mcp_helper_connected"], True)
        self.assertIs(capabilities["admission"]["source_wired"], True)
        self.assertEqual(len(capabilities["supported_scenarios"]), 9)
        self.assertEqual(len(capabilities["fixtures"]), 7)
        self.assertTrue(all(len(value) == 64 for value in capabilities["fixtures"].values()))
        with self.assertRaises(ValueError):
            CATALOG.fixture_digest("../go-core")

    def test_missing_tool_is_blocked_not_verified(self):
        rows = CATALOG.verify_fixtures()
        for name in ("go-core", "monorepo-ts"):
            row = next(x for x in rows if x["fixture"] == name)
            self.assertEqual(row["status"], "BLOCKED")
            self.assertFalse(row["correct"])
        self.assertTrue(all(row["provider_attempts"] == 0 for row in rows))


class PreflightTests(unittest.TestCase):
    def execute(self, mode="valid", selection=None, preflight=True, oracles=False, policy=True):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        expected = CATALOG.expected_capabilities(CATALOG.ROOT / "internal/app/primary_prefetch_live_test.go")
        fake = root / "test-binary"
        marker = root / "client-attempts.txt"
        fake.write_text("#!/usr/bin/python3\nimport os,sys,json,hashlib\nfrom pathlib import Path\n"
            f"mode={mode!r}\ncapabilities={expected!r}\nbase={admitted_row(expected)!r}\nmarker=Path({str(marker)!r})\n"
            "print('PRIVATE_SENTINEL')\nprint('PRIVATE_SENTINEL',file=sys.stderr)\n"
            "if any('TestPrimaryPrefetchBenchmarkCatalog' in a for a in sys.argv):\n"
            " output=Path(os.environ['IVOAI_BENCHMARK_CATALOG_OUTPUT'])\n"
            " if mode=='skipped': sys.exit(0)\n"
            " if mode=='mismatch': capabilities['harness_sha256']='b'*64\n"
            " if mode=='scope_mismatch': capabilities['direct_mcp_helper_configuration_scope']='UNKNOWN'\n"
            " if mode=='scope_missing': del capabilities['direct_mcp_helper_configuration_scope']\n"
            " if mode=='scope_connected': capabilities['direct_mcp_helper_connected']=False\n"
            " if mode=='bool_version': capabilities['schema_version']=True\n"
            " if mode=='old_admission': capabilities.pop('admission')\n"
            " if mode=='forged_admission': capabilities['admission']['source_wired']=1\n"
            " if mode=='admission_source': capabilities['admission']['source_sha256']='f'*64\n"
            " if mode=='oversized': output.write_text('x'*32769);sys.exit(0)\n"
            " if mode=='duplicate': output.write_text('{\"schema_version\":1,\"schema_version\":1}');sys.exit(0)\n"
            " output.write_text(json.dumps(capabilities));sys.exit(7 if mode=='bad_exit' else 0)\n"
            "def oracle_identity(scenario):\n"
            " tool=Path(os.environ.get('IVOAI_BENCHMARK_ORACLE_GO') if scenario=='small_known_file_correction' else os.environ['IVOAI_GRAFT_TEST_NODE'])\n"
            " paths=[tool,Path(os.environ['IVOAI_BENCHMARK_BWRAP']),Path('/usr/bin/prlimit')]\n"
            " h=hashlib.sha256((scenario+'\\0').encode())\n"
            " for p in paths: h.update(hashlib.sha256(p.read_bytes()).hexdigest().encode()+b'\\0')\n"
            " return h.hexdigest()\n"
            "if any('TestPrimaryPrefetchBenchmarkEditPreflight' in a for a in sys.argv):\n"
            " if not mode.startswith('edit_probe_') or mode=='edit_probe_skipped':sys.exit(0)\n"
            " scenario=os.environ['IVOAI_BENCHMARK_EDIT_SCENARIO']\n"
            " identity=oracle_identity(scenario) if mode!='edit_probe_wrong' else 'f'*64\n"
            " output={'schema_version':2,'scenario':scenario,'oracle_tools_sha256':identity,'ready':True,'provider_attempts':0}\n"
            " Path(os.environ['IVOAI_BENCHMARK_EDIT_PREFLIGHT_OUTPUT']).write_text(json.dumps(output)+(' '*4097 if mode=='edit_probe_oversized' else ''))\n"
            " sys.exit(7 if mode=='edit_probe_failed_exit' else 0)\n"
            "scenario=next(a for a in sys.argv if a.startswith('-test.run=')).split('/')[1]\n"
            "with marker.open('a') as stream: stream.write(scenario+'\\n')\n"
            "base.update(Scenario=scenario,Arm=os.environ['IVOAI_ECONOMIC_RUNTIME_ARM'])\n"
            "base['DistributionInventorySHA256']=hashlib.sha256((Path(os.environ['IVOAI_GRAFT_TEST_DIST'])/'files.json').read_bytes()).hexdigest()\n"
            "base['CatalogSHA256']=capabilities['catalog_sha256'];base['HarnessSHA256']=capabilities['harness_sha256']\n"
            f"fixtures={dict((s.id,s.fixture) for s in CATALOG.load_catalog())!r}\n"
            "base['FixtureSHA256']=capabilities['fixtures'][fixtures[scenario]]\n"
            "if mode=='wrong_fixture': base['FixtureSHA256']='f'*64\n"
            "if mode=='admission_refused':\n"
            " base.update(Correct=False,JSONDecoded=False,InputTokens=None,CachedInputTokens=None,OutputTokens=None)\n"
            " base['Admission'].update(state='NOT_RUN',reason='AUTH_DENIED',inventory_attempts=0,model_attempts=0,scope_matched=False,inventory_ms=0)\n"
            "if scenario in ('small_known_file_correction','cross_module_refactor'):\n"
            " base.update(Execution='workspace_edit',EditValidated=True,EditFiles=1 if scenario=='small_known_file_correction' else 3,OracleToolsSHA256=oracle_identity(scenario))\n"
            "Path(os.environ['IVOAI_GRAFT_PREFETCH_OUTPUT']).write_text(json.dumps([base]))\n")
        fake.chmod(0o700)
        distribution = root / "distribution"
        distribution.mkdir()
        (distribution / "files.json").write_text("{}")
        output = root / "evidence"
        args = []
        for key in ("baseline", "candidate", "codex", "node", "sandbox-binary"):
            args += ["--" + key, str(fake)]
        args += ["--distribution", str(distribution), "--output", str(output), "--repetitions", "1"]
        if policy:
            args += ["--filtered-environment-permitted"]
        if oracles:
            args += ["--oracle-go", str(fake), "--oracle-bwrap", str(fake)]
        args += ["--matrix16"] if selection is None else selection
        if preflight:
            args += ["--preflight-only"]
        captured = io.StringIO()
        with contextlib.redirect_stdout(captured):
            code = DRIVER.main(args)
        self.assertNotIn("PRIVATE_SENTINEL", captured.getvalue())
        for path in output.glob("*.json"):
            self.assertNotIn("PRIVATE_SENTINEL", path.read_text())
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        return code, output, marker

    def test_complete_plan_is_not_complete_execution(self):
        code, output, marker = self.execute()
        self.assertEqual(code, 1)
        self.assertFalse(marker.exists())
        report = json.loads((output / "preflight.json").read_text())
        self.assertEqual(report["provider_attempts"], 0)
        self.assertEqual([row["status"] for row in report["scenarios"]].count("READY"), 7)
        self.assertTrue(all(row["client_execution"] == "NOT_RUN" for row in report["scenarios"]))
        summary = json.loads((output / "summary.json").read_text())
        self.assertEqual(summary["global_economic_gate"], "INCOMPLETE")
        self.assertEqual(summary["all_attempts"]["A"]["attempts"], 0)

    def test_sampler_opt_in_requires_discovery_before_client_execution(self):
        code, output, marker = self.execute("mismatch", selection=["--sample-processes"], preflight=False)
        self.assertEqual(code, 1)
        self.assertFalse(marker.exists())
        report = json.loads((output / "preflight.json").read_text())
        self.assertEqual(len(report["scenarios"]), 3)
        self.assertTrue(all(row["status"] == "BLOCKED" for row in report["scenarios"]))
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertTrue(manifest["process_sampling"]["enabled"])
        self.assertEqual(len(manifest["process_sampler_sha256"]), 64)

    def test_default_scenarios_require_discovery_before_any_client_turn(self):
        for mode in ("mismatch", "scope_mismatch", "scope_missing", "scope_connected", "old_admission", "forged_admission", "admission_source", "skipped", "bool_version", "duplicate", "bad_exit"):
            with self.subTest(mode=mode):
                code, output, marker = self.execute(mode, selection=[], preflight=False)
                attempts = len(marker.read_text().splitlines()) if marker.exists() else 0
                self.assertEqual(attempts, 0, "default path invoked client turns before validating binary contracts")
                self.assertEqual(code, 1)
                report = json.loads((output / "preflight.json").read_text())
                self.assertEqual([row["scenario"] for row in report["scenarios"]], list(DRIVER.SCENARIOS))
                self.assertTrue(all(row["status"] == "BLOCKED" for row in report["scenarios"]))

    def test_valid_default_selection_binds_both_contracts_and_preserves_order(self):
        code, output, marker = self.execute(selection=[], preflight=False)
        self.assertEqual(code, 0)  # Fabricated fixture mechanics, never economic acceptance.
        self.assertEqual(len(marker.read_text().splitlines()), 6)
        preflight = json.loads((output / "preflight.json").read_text())
        self.assertTrue(all(value["status"] == "READY" for value in preflight["discovery"].values()))
        self.assertEqual([row["scenario"] for row in preflight["scenarios"]], list(DRIVER.SCENARIOS))
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual([row["Arm"] for row in rows], ["A", "B", "B", "A", "A", "B"])
        expected = CATALOG.expected_capabilities(CATALOG.ROOT / "internal/app/primary_prefetch_live_test.go")
        self.assertTrue(all(row["Correct"] and row["HarnessSHA256"] == expected["harness_sha256"] for row in rows))

    def test_default_preflight_only_never_starts_client(self):
        code, output, marker = self.execute(selection=[], preflight=True)
        self.assertEqual(code, 0)
        self.assertFalse(marker.exists())
        report = json.loads((output / "preflight.json").read_text())
        self.assertEqual([row["scenario"] for row in report["scenarios"]], list(DRIVER.SCENARIOS))
        self.assertTrue(all(row["status"] == "READY" for row in report["scenarios"]))

    def test_missing_skipped_wrong_or_unbounded_discovery_blocks_client(self):
        for mode in ("skipped", "mismatch", "scope_mismatch", "scope_missing", "scope_connected", "old_admission", "forged_admission", "admission_source", "bool_version", "oversized", "duplicate", "bad_exit"):
            with self.subTest(mode=mode):
                code, output, marker = self.execute(mode, ["--scenarios", "dependencies"], preflight=False)
                self.assertEqual(code, 1)
                self.assertFalse(marker.exists())
                report = json.loads((output / "preflight.json").read_text())
                self.assertEqual(report["scenarios"][0]["status"], "BLOCKED")
                self.assertEqual(json.loads((output / "summary.json").read_text())["all_attempts"]["A"]["attempts"], 0)

    def test_new_subset_uses_existing_ab_ba_and_source_identities(self):
        ids = ["dependencies", "monorepo", "unsupported_parser", "non_code"]
        code, output, marker = self.execute(selection=["--scenarios", *ids], preflight=False)
        self.assertEqual(code, 0)  # Synthetic runner mechanics, never economic acceptance.
        self.assertEqual(len(marker.read_text().splitlines()), 8)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual([x["Arm"] for x in rows], ["A", "B", "B", "A", "A", "B", "B", "A"])
        self.assertTrue(all(x["MeasurementValid"] and len(x["FixtureSHA256"]) == 64 for x in rows))
        summary = json.loads((output / "summary.json").read_text())
        self.assertEqual(summary["global_economic_gate"], "INCOMPLETE")

    def test_missing_environment_policy_does_not_start_client_or_probe(self):
        status, output, marker = self.execute(selection=['--scenarios','api'], preflight=False, policy=False)
        self.assertEqual(status,1)
        self.assertFalse(marker.exists())
        preflight=json.loads((output/'preflight.json').read_text())
        self.assertEqual(preflight['scenarios'][0]['reason'],'filtered_environment_policy_not_declared')
        self.assertEqual(preflight['authentication'],'UNOBSERVED_BY_STATIC_DISCOVERY')

    def test_admission_refusal_stops_before_next_arm_and_preserves_zero_model(self):
        status, output, marker = self.execute('admission_refused', selection=['--scenarios', 'api'], preflight=False)
        self.assertEqual(status, 1)
        self.assertEqual(len(marker.read_text().splitlines()), 1)
        rows=json.loads((output/'runtime-comparison.json').read_text())
        self.assertEqual(len(rows),1)
        self.assertEqual(rows[0]['ClientExecution'],'NOT_RUN')
        self.assertEqual(rows[0]['Admission']['model_attempts'],0)
        self.assertEqual(rows[0]['Admission']['automatic_fallbacks'],0)
        self.assertEqual(rows[0]['Admission']['reason'],'AUTH_DENIED')

    def test_unavailable_cases_never_become_fake_attempts_or_success(self):
        code, output, marker = self.execute(preflight=False)
        self.assertEqual(code, 1)
        self.assertEqual(len(marker.read_text().splitlines()), 14)
        summary = json.loads((output / "summary.json").read_text())
        self.assertEqual(summary["subset_economic_diagnostic"], "INCOMPLETE")
        self.assertEqual(summary["all_attempts"]["A"]["attempts"], 7)
        self.assertEqual(len(summary["requested_scenarios"]), 16)
        self.assertEqual(len(summary["measured_scenarios"]), 7)

    def test_edit_probes_require_real_output_not_missing_skip_or_failed_exit(self):
        selection = ["--scenarios", "small_known_file_correction", "cross_module_refactor"]
        for mode in ("edit_probe_skipped", "edit_probe_wrong", "edit_probe_failed_exit", "edit_probe_oversized"):
            with self.subTest(mode=mode):
                code, output, marker = self.execute(mode, selection, preflight=False, oracles=True)
                self.assertEqual(code, 1)
                self.assertFalse(marker.exists())
                rows = json.loads((output / "preflight.json").read_text())["scenarios"]
                self.assertEqual([r["status"] for r in rows], ["BLOCKED", "BLOCKED"])

    def test_synthetic_edit_contract_preserves_abba_and_oracle_receipts(self):
        selection = ["--scenarios", "small_known_file_correction", "cross_module_refactor"]
        code, output, marker = self.execute("edit_probe_valid", selection, preflight=False, oracles=True)
        self.assertEqual(code, 0)
        self.assertEqual(len(marker.read_text().splitlines()), 4)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertEqual([r["Arm"] for r in rows], ["A", "B", "B", "A"])
        self.assertTrue(all(r["MeasurementValid"] and r["Correct"] and r["EditValidated"] for r in rows))
        self.assertTrue(all(len(r["OracleToolsSHA256"]) == 64 for r in rows))
        self.assertEqual(json.loads((output / "summary.json").read_text())["global_economic_gate"], "INCOMPLETE")

    def test_runtime_fixture_mismatch_is_retained_as_invalid(self):
        code, output, marker = self.execute("wrong_fixture", ["--scenarios", "dependencies"], preflight=False)
        self.assertEqual(code, 1)
        self.assertEqual(len(marker.read_text().splitlines()), 2)
        rows = json.loads((output / "runtime-comparison.json").read_text())
        self.assertTrue(all(not x["Correct"] and not x["MeasurementValid"] and x["Error"] == "invalid_evidence" for x in rows))


if __name__ == "__main__":
    unittest.main()
