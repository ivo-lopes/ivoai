"""Protocol, bounds and real offline fixture checks; no native client/provider."""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

import benchmark_sampler_calibration as calibration


class CalibrationTests(unittest.TestCase):
    def test_fixed_balanced_plan_and_independent_cpu_oracle(self):
        rows = calibration.plan()
        self.assertEqual(len(rows), 32)
        self.assertEqual([r['ordinal'] for r in rows], list(range(32)))
        for case in calibration.CASES:
            subset = [r for r in rows if r['case'] == case]
            self.assertEqual([r['arm'] for r in subset], ['OFF', 'ON', 'ON', 'OFF'] * 2)
            self.assertEqual(len([r for r in subset if r['arm'] == 'ON']), 4)
        n = calibration.CPU_ITERATIONS
        self.assertEqual(calibration.expected('cpu')['checksum'], sum(i*i for i in range(n)) % 104729)
        self.assertLess(calibration.ATTEMPT_SECONDS * len(rows), 120)

    def test_private_bounded_no_overwrite_and_child_environment(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'receipt.json'
            calibration.write_new(path, {'status': 'PASS'})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                calibration.write_new(path, {'status': 'REPLACED'})
            self.assertEqual(json.loads(path.read_text()), {'status': 'PASS'})
            with self.assertRaises(ValueError):
                calibration.write_new(path.with_name('large'), {'body': 'x' * 32768})
            with mock.patch.dict(os.environ, {'CODEX_HOME': 'PRIVATE_SECRET', 'ANTHROPIC_API_KEY': 'PRIVATE_SECRET', 'GIT_CONFIG_PARAMETERS': 'PRIVATE_SECRET'}):
                env = calibration.private_env(Path(temporary))
            self.assertEqual(set(env), {'PATH', 'HOME', 'TMPDIR', 'LC_ALL', 'TZ'})
            self.assertNotIn('PRIVATE_SECRET', json.dumps(env))

    def test_actual_fixture_failure_is_retained_not_quality_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            item = calibration.plan()[0]
            # A failed owned launch must remain a row with unknown workload
            # CPU/actual children, rather than zero or a successful fixture.
            with mock.patch.object(calibration.subprocess, 'Popen', side_effect=OSError):
                row = calibration.run_one(item, Path(temporary), calibration.waiter())
            self.assertEqual(row['status'], 'FAILED')
            self.assertFalse(row['correct'])
            self.assertIsNone(row['workload_wait4_user_ms'])
            self.assertIsNone(row['actual_fixture'])
            self.assertIsNone(row['process_observation'])
            self.assertNotIn('PRIVATE_SECRET', json.dumps(row))

    @unittest.skipUnless(os.environ.get('IVOAI_SAMPLER_CALIBRATION_FIXTURES') == '1', 'NOT_RUN: explicit offline subprocess fixtures')
    def test_real_memory_and_churn_have_known_counts_and_partial_observation(self):
        wait4 = calibration.waiter()
        with tempfile.TemporaryDirectory() as temporary:
            for case in ('touched_memory_children', 'churn'):
                item = next(r for r in calibration.plan() if r['case'] == case and r['arm'] == 'ON')
                row = calibration.run_one(item, Path(temporary), wait4)
                self.assertTrue(row['correct'], row['error'])
                self.assertEqual(row['actual_fixture']['completed_children'], calibration.expected(case)['completed_children'])
                observed = row['process_observation']
                self.assertEqual(observed['status'], 'partial')
                self.assertIsNone(observed['exact_peak_rss'])
                self.assertIsNone(observed['actual_process_total'])
                self.assertGreater(observed['sample_count'], 0)
                self.assertGreaterEqual(row['known_identities_not_observed'], 0)
                for field in ('pid', 'argv', 'environment', 'command'):
                    self.assertNotIn('"' + field + '"', json.dumps(row))


if __name__ == '__main__':
    unittest.main()
