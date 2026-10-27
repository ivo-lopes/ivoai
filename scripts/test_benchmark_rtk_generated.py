import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('generated_rtk', Path(__file__).with_name('benchmark-rtk-generated.py'))
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


def row(scenario='generated_grep', repetition=0, arm=0):
    value = {field: 0 for field in benchmark.INTEGER_FIELDS}
    value.update({field: False for field in benchmark.BOOL_FIELDS})
    value.update(scenario=scenario, diagnostic='disabled', artifact_id='artifact_abc', snapshot_sha256='a'*64, repetition=repetition, arm=arm, order=(arm-repetition)%2, facts_total=2, facts_present=2)
    return value


class GeneratedRTKReport(unittest.TestCase):
    def test_parser_only_accepts_allowlisted_scalar_rows(self):
        encoded = json.dumps(row())
        parsed = benchmark.observations('arbitrary output containing secrets\n    output_view_generated_test.go:99: GENERATED_RTK_JSON='+encoded+'\n')
        self.assertEqual(parsed, [row()])
        for field, value in [('raw_bytes', True), ('scenario', 'untrusted'), ('duration_us', -1), ('artifact_id', '../private'), ('facts_present', 3), ('derived', 'yes'), ('raw_stdout', 'data')]:
            invalid=row();invalid[field]=value
            with self.subTest(field=field), self.assertRaises(ValueError):
                benchmark.validate_row(invalid)

    def test_completeness_rejects_missing_and_duplicate_attempts(self):
        rows=[row(s,r,a) for s in benchmark.SCENARIOS for r in range(6) for a in range(2)]
        self.assertTrue(benchmark.summarize(rows)['complete_36_observations'])
        self.assertFalse(benchmark.summarize(rows[:-1])['complete_36_observations'])
        self.assertFalse(benchmark.summarize(rows[:-1]+[copy.deepcopy(rows[0])])['complete_36_observations'])

    def test_provider_failure_and_omissions_are_not_reclassified_as_correctness(self):
        direct=row();direct.update(baseline_correct=True, exact_recovery=True, duration_us=2, view_bytes=100)
        candidate=row(arm=1);candidate.update(diagnostic='provider_failed', provider_failure=True, facts_present=0, facts_missing=2, exact_recovery=True, duration_us=50000, view_bytes=100)
        result=benchmark.summarize([direct,candidate])
        self.assertEqual(result['scenarios']['generated_grep']['arms']['1']['provider_failures'],1)
        self.assertEqual(result['scenarios']['generated_grep']['arms']['1']['literal_facts_missing_total'],2)
        self.assertEqual(result['scenarios']['generated_grep']['arms']['0']['baseline_correct_count'],1)
        self.assertFalse(result['semantic_parity_scored'])
        self.assertTrue(result['no_rows_excluded'])
