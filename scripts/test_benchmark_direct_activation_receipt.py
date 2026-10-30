import copy
import dataclasses
import json
import os
from pathlib import Path
import tempfile
import unittest

import benchmark_direct_activation_receipt as contract


def fixture():
    identities = {key: format(index + 1, '064x') for index, key in enumerate(contract.IDENTITY_KEYS)}
    expected = contract.ExpectedColdActivation('api', 'go-core', **identities)
    arms = []
    for index, name in enumerate(('A', 'B')):
        arm = dict.fromkeys(contract.COUNTS, 0)
        arm.update(dict.fromkeys(contract.BOOL_FIELDS, True))
        arm.update(arm=name, gateway_enabled=bool(index), execution_mode='direct_only',
                   code_inspections=1, direct_inspections=1, gateway_resolves=index,
                   gateway_cache_misses=index, text_bytes=120, text_sha256='a' * 64)
        arms.append(arm)
    value = dict(schema_version=1, contract='primary_direct_gateway_cold_v1', scenario='api',
                 fixture='go-core', status='QUALIFIED_COLD_DIRECT_ONLY',
                 supplies_actual_factory_qualification=True, cold_only=True,
                 warmup_performed=False, model_client_started=False, authenticated=False, protected_recovery_enabled=False,
                 identity=identities, arms=arms)
    return expected, value


class ColdActivationContractTests(unittest.TestCase):
    # JSON fixtures test parser semantics only, never actual factory execution.
    def test_positive_consistency_and_frozen_return(self):
        expected, value = fixture()
        result = contract.validate_receipt(value, expected)
        self.assertEqual((result.text_sha256, result.text_bytes), ('a' * 64, 120))
        self.assertEqual(result.basis, 'reviewed_local_receipt_consistency_only')
        with self.assertRaises(dataclasses.FrozenInstanceError):
            result.text_bytes = 0

    def test_every_identity_dimension_and_fresh_challenge_are_bound(self):
        expected, original = fixture()
        for key in contract.IDENTITY_KEYS:
            with self.subTest(identity=key):
                bad = copy.deepcopy(original)
                bad['identity'][key] = 'f' * 64
                with self.assertRaisesRegex(contract.ActivationReceiptError, '^HOST_IDENTITY_MISMATCH$'):
                    contract.validate_receipt(bad, expected)
        stale = dataclasses.replace(expected, challenge_sha256='e' * 64)
        with self.assertRaisesRegex(contract.ActivationReceiptError, '^HOST_IDENTITY_MISMATCH$'):
            contract.validate_receipt(original, stale)

    def test_invalid_host_expectation_cannot_admit_a_receipt(self):
        expected, value = fixture()
        for changes in ({'scenario': 'implementation'}, {'fixture': 'other'},
                        {'gateway_flags': (True, True)}, {'gateway_flags': (0, 1)},
                        {'executable_sha256': 'not a digest'}):
            with self.subTest(changes=tuple(changes)):
                with self.assertRaises(contract.ActivationReceiptError):
                    dataclasses.replace(expected, **changes)
        with self.assertRaisesRegex(contract.ActivationReceiptError, '^MISSING_HOST_EXPECTATION$'):
            contract.validate_receipt(value, None)

    def test_missing_unknown_and_wrong_typed_fields_fail(self):
        expected, original = fixture()
        for key in contract.ROOT_KEYS:
            bad = copy.deepcopy(original)
            del bad[key]
            with self.subTest(missing=key), self.assertRaises(contract.ActivationReceiptError):
                contract.validate_receipt(bad, expected)
        for section in ('root', 'identity', 'arm'):
            bad = copy.deepcopy(original)
            target = bad if section == 'root' else bad['identity'] if section == 'identity' else bad['arms'][0]
            target['raw_prompt'] = 'fixture-secret-must-not-escape'
            with self.subTest(section=section), self.assertRaises(contract.ActivationReceiptError) as caught:
                contract.validate_receipt(bad, expected)
            self.assertNotIn('fixture-secret', str(caught.exception))
        for key, replacement in (('schema_version', True), ('cold_only', 1),
                                 ('supplies_actual_factory_qualification', False),
                                 ('warmup_performed', True), ('model_client_started', True),
                                 ('authenticated', True), ('protected_recovery_enabled', True), ('status', 'SYNTHETIC_PASS')):
            bad = copy.deepcopy(original); bad[key] = replacement
            with self.subTest(field=key), self.assertRaises(contract.ActivationReceiptError):
                contract.validate_receipt(bad, expected)

    def test_flags_classification_owners_fidelity_and_counters_fail_closed(self):
        expected, original = fixture()
        for index in (0, 1):
            for key in contract.BOOL_FIELDS:
                bad = copy.deepcopy(original); bad['arms'][index][key] = False
                with self.subTest(arm=index, field=key), self.assertRaises(contract.ActivationReceiptError):
                    contract.validate_receipt(bad, expected)
            for key in contract.COUNTS:
                for tampered in (True, -1, 99):
                    bad = copy.deepcopy(original); bad['arms'][index][key] = tampered
                    with self.subTest(arm=index, field=key, tampered=tampered), self.assertRaises(contract.ActivationReceiptError):
                        contract.validate_receipt(bad, expected)
            for key, tampered in (('gateway_enabled', bool(1-index)), ('execution_mode', 'managed'),
                                  ('arm', ('B', 'A')[index]), ('text_bytes', 0),
                                  ('text_bytes', 3073), ('text_sha256', 'z' * 64)):
                bad = copy.deepcopy(original); bad['arms'][index][key] = tampered
                with self.subTest(arm=index, field=key), self.assertRaises(contract.ActivationReceiptError):
                    contract.validate_receipt(bad, expected)
        for key, tampered in (('text_bytes', 121), ('text_sha256', 'b' * 64)):
            bad = copy.deepcopy(original); bad['arms'][1][key] = tampered
            with self.subTest(mismatch=key), self.assertRaisesRegex(contract.ActivationReceiptError, '^EXACT_EVIDENCE_MISMATCH$'):
                contract.validate_receipt(bad, expected)

    def test_private_reader_positive_and_invalid_inputs_never_reach_simulated_model(self):
        expected, value = fixture()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'receipt.json'
            def write(body):
                path.write_bytes(body)
                path.chmod(0o600)
            def before_model():
                result = contract.read_receipt(path, expected)
                observed.append('SIMULATED_ONLY')
                return result
            observed = []
            write(json.dumps(value).encode())
            self.assertEqual(before_model().text_bytes, 120)
            self.assertEqual(observed, ['SIMULATED_ONLY'])
            observed.clear()
            bad = copy.deepcopy(value); bad['arms'][1]['gateway_resolves'] = 0
            for label, body in (
                ('missing', None), ('tampered', json.dumps(bad).encode()),
                ('oversize', b' ' * (contract.MAX_RECEIPT_BYTES + 1)),
                ('duplicate', b'{"schema_version":1,"schema_version":1}'),
                ('invalid', b'fixture-secret-invalid-json'),
                ('nonfinite', b'{"schema_version":NaN}'), ('deep', b'[' * 2000)):
                with self.subTest(case=label):
                    path.unlink(missing_ok=True)
                    if body is not None: write(body)
                    with self.assertRaises(contract.ActivationReceiptError) as caught:
                        before_model()
                    self.assertNotIn('fixture-secret', str(caught.exception))
                    self.assertEqual(observed, [])
            write(json.dumps(value).encode())
            path.chmod(0o644)
            with self.assertRaises(contract.ActivationReceiptError): before_model()
            self.assertEqual(observed, [])
            path.unlink()
            target = Path(directory) / 'target.json'; target.write_text(json.dumps(value)); target.chmod(0o600)
            path.symlink_to(target)
            with self.assertRaises(contract.ActivationReceiptError): before_model()
            path.unlink()
            if hasattr(os, 'mkfifo'):
                os.mkfifo(path, 0o600)
                with self.assertRaises(contract.ActivationReceiptError): before_model()
                path.unlink()
            alias = Path(directory) / 'alias'; alias.symlink_to(directory, target_is_directory=True)
            with self.assertRaises(contract.ActivationReceiptError): contract.read_receipt(alias/'target.json', expected)
            with self.assertRaises(contract.ActivationReceiptError): contract.read_receipt(Path('relative.json'), expected)
            self.assertEqual(observed, [])

    def test_stale_receipt_blocks_before_simulated_model(self):
        expected, value = fixture()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'receipt.json'; path.write_text(json.dumps(value)); path.chmod(0o600)
            model_calls = []
            def run_after_qualification():
                contract.read_receipt(path, dataclasses.replace(expected, challenge_sha256='f'*64))
                model_calls.append('SIMULATED_ONLY')
            with self.assertRaises(contract.ActivationReceiptError): run_after_qualification()
            self.assertEqual(model_calls, [])


if __name__ == '__main__':
    unittest.main()
