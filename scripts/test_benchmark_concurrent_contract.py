"""Offline-only composition tests. No client, inference, or overlap attestation."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import benchmark_catalog as catalog
import benchmark_concurrent_contract as contract


class ConcurrentContractTests(unittest.TestCase):
    def test_exact_children_and_matrix_still_unsupported(self):
        value = contract.load_contract()
        self.assertEqual([(c.scenario, c.fixture) for c in value.children], list(contract.CHILDREN))
        scenario = next(s for s in catalog.load_catalog() if s.id == value.scenario)
        self.assertEqual(catalog.planned_status(scenario), "UNSUPPORTED")
        self.assertEqual(len(contract.contract_digest()), 64)

    def test_rejects_mutations_duplicates_unknown_and_unbounded_contract(self):
        value = json.loads(contract.CONTRACT.read_bytes())
        variants = []
        for key, item in (("max_clients", 3), ("schema_version", True), ("execution", "workspace_edit"),
                          ("client_overlap_required", False), ("command", "anything")):
            changed = copy.deepcopy(value); changed[key] = item; variants.append(json.dumps(changed).encode())
        changed = copy.deepcopy(value); changed["children"].reverse(); variants.append(json.dumps(changed).encode())
        changed = copy.deepcopy(value); changed["children"][1] = changed["children"][0]; variants.append(json.dumps(changed).encode())
        variants += [contract.CONTRACT.read_bytes().replace(b'"max_clients": 2', b'"max_clients": 3, "max_clients": 2'),
                     contract.CONTRACT.read_bytes().replace(b'"max_clients"', b'"MAX_CLIENTS"'), b' ' * 4097]
        for body in variants:
            with self.subTest(body=body), self.assertRaises(ValueError):
                contract.load_contract(body)

    def test_digest_binds_both_fixtures(self):
        before = contract.contract_digest()
        original = catalog.fixture_digest
        for fixture in ("go-core", "python-deps"):
            with mock.patch.object(catalog, "fixture_digest", side_effect=lambda name: "f" * 64 if name == fixture else original(name)):
                self.assertNotEqual(contract.contract_digest(), before)

    def test_real_python_oracle_and_missing_go_preserve_both_results(self):
        report = contract.preflight()
        self.assertEqual(report["preflight_status"], "BLOCKED")
        self.assertEqual([c["status"] for c in report["children"]], ["BLOCKED", "VERIFIED"])
        self.assertEqual(report["scenario_status"], "UNSUPPORTED")
        self.assertEqual(report["client_overlap"], "NOT_RUN")
        self.assertEqual(report["provider_attempts"], 0)
        self.assertEqual(report["client_attempts"], 0)

    def test_selected_oracles_never_run_unselected_programs(self):
        with mock.patch.object(catalog.subprocess, "run") as run:
            for selection in ((), ("go-core", "go-core"), ("unknown",)):
                with self.assertRaises(ValueError):
                    catalog.verify_fixtures(fixtures=selection)
            run.assert_not_called()
        rows = catalog.verify_fixtures(fixtures=("python-deps",))
        self.assertEqual([r["fixture"] for r in rows], ["python-deps"])
        self.assertTrue(rows[0]["correct"])

    def test_missing_swapped_skipped_failed_or_boolean_exit_reports_block(self):
        rows = [{"fixture": fixture, "fixture_sha256": catalog.fixture_digest(fixture),
                 "scope": "offline_synthetic_program_oracle", "provider_attempts": 0,
                 "status": "VERIFIED", "correct": True, "exit_code": 0} for _, fixture in contract.CHILDREN]
        variants = [[], rows[:1], list(reversed(rows)), rows + [rows[0]]]
        for index in range(2):
            for key, item in (("status", "SKIP"), ("exit_code", 1), ("exit_code", False), ("correct", False), ("fixture_sha256", "f" * 64)):
                changed = copy.deepcopy(rows); changed[index][key] = item; variants.append(changed)
        for value in variants:
            with self.subTest(value=value), mock.patch.object(catalog, "verify_fixtures", return_value=value):
                report = contract.preflight()
                self.assertEqual(report["preflight_status"], "BLOCKED")
                self.assertEqual(len(report["children"]), 2)
        with mock.patch.object(catalog, "verify_fixtures", return_value=rows):
            report = contract.preflight()
            self.assertEqual(report["preflight_status"], "OFFLINE_ORACLES_VERIFIED")
            self.assertEqual(report["scenario_status"], "UNSUPPORTED")
            self.assertEqual(report["client_execution"], "NOT_RUN")

    def test_private_output_does_not_overwrite_or_follow_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); output = root / "report.json"
            contract.write_private(output, {"client_execution": "NOT_RUN"})
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                contract.write_private(output, {})
            link = root / "link.json"; link.symlink_to(output)
            with self.assertRaises(FileExistsError):
                contract.write_private(link, {})
            root.chmod(0o755)
            with self.assertRaises(ValueError):
                contract.write_private(root / "public.json", {})

    def test_source_change_during_oracle_run_cannot_verify(self):
        rows = [{"fixture": fixture, "fixture_sha256": catalog.fixture_digest(fixture),
                 "scope": "offline_synthetic_program_oracle", "provider_attempts": 0,
                 "status": "VERIFIED", "correct": True, "exit_code": 0} for _, fixture in contract.CHILDREN]
        with mock.patch.object(contract, "source_pins", side_effect=[{"pin": "before"}, {"pin": "after"}]), mock.patch.object(catalog, "verify_fixtures", return_value=rows):
            report = contract.preflight()
            self.assertFalse(report["source_pins_stable"])
            self.assertEqual(report["preflight_status"], "BLOCKED")
            self.assertEqual(report["client_execution"], "NOT_RUN")


if __name__ == "__main__":
    unittest.main()
