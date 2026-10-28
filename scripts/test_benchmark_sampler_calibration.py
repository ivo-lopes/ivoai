"""Protocol, bounds and real offline fixture checks; no native client/provider."""
import errno
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import time
from types import SimpleNamespace
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
            with mock.patch.object(calibration.subprocess, 'Popen', side_effect=OSError('PRIVATE_SECRET')):
                row = calibration.run_one(item, Path(temporary), calibration.waiter())
            self.assertEqual(row['status'], 'FAILED')
            self.assertFalse(row['correct'])
            self.assertEqual(row['failure_phase'], 'launch')
            self.assertEqual(row['failure_reason'], 'os_error')
            self.assertIsNone(row['failure_errno'])
            self.assertIsNone(row['workload_wait4_user_ms'])
            self.assertIsNone(row['actual_fixture'])
            self.assertIsNone(row['process_observation'])
            self.assertNotIn('PRIVATE_SECRET', json.dumps(row))

    def simulated_pipe_row(self, payload, code=0, read_error=None, sample_error=None):
        # Real bounded pipe, fake already-owned process: no workload/PID is
        # launched. Any accidental attempt to signal/reap a PID fails safely.
        read_fd, write_fd = os.pipe()
        try:
            os.write(write_fd, payload)
        finally:
            os.close(write_fd)
        process = SimpleNamespace(pid=2147483647, stdin=io.BytesIO(),
                                  stdout=os.fdopen(read_fd, 'rb'), returncode=0)
        item = {**calibration.plan()[0], 'arm': 'ON' if sample_error else 'OFF'}
        def wait4(owned, timeout, sampler):
            owned.returncode = code
            return code, SimpleNamespace(ru_utime=0.0, ru_stime=0.0)
        with tempfile.TemporaryDirectory() as temporary:
            with mock.patch.object(calibration.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(calibration.os, 'killpg', side_effect=AssertionError('no real signals permitted')), \
                 mock.patch.object(calibration.os, 'wait4', side_effect=AssertionError('no real reap permitted')):
                if read_error:
                    with mock.patch.object(calibration, 'read_message', side_effect=read_error):
                        return calibration.run_one(item, Path(temporary), wait4)
                if sample_error:
                    with mock.patch.object(calibration, 'ProcessSampler', side_effect=sample_error):
                        return calibration.run_one(item, Path(temporary), wait4)
                return calibration.run_one(item, Path(temporary), wait4)

    def test_typed_pipe_failures_preserve_phase_exit_and_private_payload(self):
        ready = b'{"kind":"ready","ready_children":0}\n'
        known = calibration.expected('wait')
        done = {'kind': 'done', **{k: known[k] for k in
                ('started_children', 'completed_children', 'child_failures', 'checksum', 'touched_pages')},
                'reported_coordinator_cpu_ms': 0.0, 'reported_waited_children_cpu_ms': 0.0}
        good_done = json.dumps(done).encode() + b'\n'
        wrong_done = json.dumps({**done, 'checksum': 99}).encode() + b'\n'
        cases = [
            ('eof', 'ready', None, b'', 0),
            ('oversize', 'ready', None, b'x' * (calibration.MAX_MESSAGE + 1), 0),
            ('invalid_json', 'ready', None, b'PRIVATE_SECRET invalid JSON\n', 0),
            ('shape', 'ready', None, b'[]\n', 0),
            ('ready_mismatch', 'ready', None, b'{"kind":"ready","ready_children":99}\n', 0),
            ('eof', 'done', 0, ready, 0),
            ('invalid_json', 'done', 0, ready + b'PRIVATE_SECRET\n', 0),
            ('nonzero_exit', 'wait4', 7, ready + good_done, 7),
            ('deadline', 'wait4', 124, ready, 124),
            ('oracle_mismatch', 'oracle', 0, ready + wrong_done, 0),
        ]
        for reason, phase, exit_code, payload, code in cases:
            with self.subTest(reason=reason, phase=phase, exit_code=exit_code):
                row = self.simulated_pipe_row(payload, code)
                self.assertFalse(row['correct'])
                self.assertEqual(row['failure_reason'], reason)
                self.assertEqual(row['failure_phase'], phase)
                self.assertEqual(row['exit_code'], exit_code)
                self.assertIsNone(row['failure_errno'])
                self.assertNotIn('PRIVATE_SECRET', json.dumps(row))
                self.assertEqual(row['error'], 'fixture_or_protocol_failed')

    def test_deadline_and_os_error_diagnostics_do_not_invent_errno(self):
        read_fd, write_fd = os.pipe()
        try:
            with self.assertRaises(calibration.ProtocolDeadline) as result:
                calibration.read_message(read_fd, time.monotonic() - 1)
        finally:
            os.close(read_fd); os.close(write_fd)
        self.assertIsInstance(result.exception, TimeoutError)
        row = self.simulated_pipe_row(b'', read_error=result.exception)
        self.assertEqual((row['failure_reason'], row['failure_phase'], row['exit_code']),
                         ('deadline', 'ready', None))
        self.assertIsNone(row['failure_errno'])
        row = self.simulated_pipe_row(b'{"kind":"ready","ready_children":0}\n',
                                      sample_error=OSError(errno.EMFILE, 'PRIVATE_SECRET', '/PRIVATE_PATH'))
        self.assertEqual((row['failure_reason'], row['failure_phase'], row['failure_errno']),
                         ('os_error', 'sample', errno.EMFILE))
        self.assertIsNone(row['exit_code'])
        self.assertNotIn('PRIVATE', json.dumps(row))
        spoof = ValueError('PRIVATE_SECRET'); spoof.errno = errno.EACCES
        self.assertEqual(calibration.failure_details(spoof, None), ('value_error', None))
        for number in (-1, 4096, True):
            self.assertIsNone(calibration.failure_details(OSError(number, 'PRIVATE_SECRET'), None)[1])
        with self.assertRaises(ValueError):
            calibration.ProtocolFailure('PRIVATE_SECRET')

    @unittest.skipUnless(os.environ.get('IVOAI_SAMPLER_CALIBRATION_FIXTURES') == '1', 'NOT_RUN: explicit offline subprocess fixtures')
    def test_real_memory_and_churn_have_known_counts_and_partial_observation(self):
        wait4 = calibration.waiter()
        with tempfile.TemporaryDirectory() as temporary:
            for case in ('touched_memory_children', 'churn'):
                with self.subTest(case=case):
                    item = next(r for r in calibration.plan() if r['case'] == case and r['arm'] == 'ON')
                    row = calibration.run_one(item, Path(temporary), wait4)
                    message = json.dumps(row, sort_keys=True, allow_nan=False)
                    self.assertLessEqual(len(message.encode()), 32768)
                    self.assertTrue(row['correct'], message)
                    self.assertIsNone(row['failure_phase'], message)
                    self.assertIsNone(row['failure_reason'], message)
                    self.assertIsNone(row['failure_errno'], message)
                    self.assertEqual(row['actual_fixture']['completed_children'], calibration.expected(case)['completed_children'], message)
                    observed = row['process_observation']
                    self.assertEqual(observed['status'], 'partial', message)
                    self.assertIsNone(observed['exact_peak_rss'], message)
                    self.assertIsNone(observed['actual_process_total'], message)
                    self.assertGreater(observed['sample_count'], 0, message)
                    self.assertGreaterEqual(row['known_identities_not_observed'], 0, message)
                    for field in ('pid', 'argv', 'environment', 'command'):
                        self.assertNotIn('"' + field + '"', json.dumps(row), message)


if __name__ == '__main__':
    unittest.main()
