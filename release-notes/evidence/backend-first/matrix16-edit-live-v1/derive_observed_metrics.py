#!/usr/bin/python3
"""Derive scalar observations from the reviewed two-edit cohort; no inference or rescoring."""
import hashlib
import json
import sys
from pathlib import Path

source = Path(sys.argv[1])
rows_path, summary_path = source / 'runtime-comparison.json', source / 'summary.json'
rows, summary = json.loads(rows_path.read_bytes()), json.loads(summary_path.read_bytes())
scenarios = ('small_known_file_correction', 'cross_module_refactor')
expected = []
for repetition in range(3):
    for index, scenario in enumerate(scenarios):
        for arm in (('A', 'B') if (repetition + index) % 2 == 0 else ('B', 'A')):
            expected.append((repetition, scenario, arm))
assert [(r['Repetition'], r['Scenario'], r['Arm']) for r in rows] == expected
assert len(rows) == 12 and summary['global_economic_gate'] == 'INCOMPLETE'
metrics = ('InputTokens', 'CachedInputTokens', 'OutputTokens', 'ProviderMS', 'PrefetchMS',
           'ValidationMS', 'TotalMS', 'LocalElapsedMS', 'LocalUserCPUMS', 'LocalSystemCPUMS',
           'ToolCalls', 'PromptBytes', 'EvidenceBytes', 'SourceBytes')
resource_metrics = ('LocalElapsedMS', 'LocalUserCPUMS', 'LocalSystemCPUMS')

def totals(sample):
    result = {'attempts': len(sample), 'correct': sum(r['Correct'] for r in sample),
              'measurement_valid': sum(r['MeasurementValid'] for r in sample)}
    for metric in metrics:
        values = [r[metric] for r in sample if (r['MeasurementValid'] or metric in resource_metrics)
                  and type(r.get(metric)) is int]
        result[metric] = {'sum': sum(values) if values else None, 'observed_rows': len(values)}
    peak = [r['LocalPeakRSSBytes'] for r in sample if type(r.get('LocalPeakRSSBytes')) is int]
    result['LocalPeakRSSBytes'] = {'maximum': max(peak) if peak else None, 'observed_rows': len(peak),
                                 'sum': None, 'scope': 'maximum_individual_process_not_simultaneous_sum'}
    return result

def reductions(arms):
    result = {}
    for metric in ('InputTokens', 'TotalMS', 'LocalElapsedMS'):
        a, b = arms['A'][metric], arms['B'][metric]
        complete = a['observed_rows'] == arms['A']['attempts'] and b['observed_rows'] == arms['B']['attempts']
        result[metric] = 100 * (a['sum'] - b['sum']) / a['sum'] if complete and a['sum'] else None
    return result

arms = {arm: totals([r for r in rows if r['Arm'] == arm]) for arm in ('A', 'B')}
for arm in ('A', 'B'):
    assert arms[arm]['attempts'] == 6
    for metric in ('InputTokens', 'CachedInputTokens', 'OutputTokens', 'TotalMS', 'ToolCalls'):
        assert arms[arm][metric] == summary['all_attempts'][arm][metric]
by_repetition = []
pairs = []
for repetition in range(3):
    sub = {arm: totals([r for r in rows if r['Arm'] == arm and r['Repetition'] == repetition]) for arm in ('A', 'B')}
    by_repetition.append({'repetition': repetition, 'arms': sub, 'observed_reduction_percent': reductions(sub)})
    for scenario in scenarios:
        pair = [r for r in rows if r['Repetition'] == repetition and r['Scenario'] == scenario]
        sub = {arm: totals([r for r in pair if r['Arm'] == arm]) for arm in ('A', 'B')}
        pairs.append({'repetition': repetition, 'scenario': scenario, 'both_correct': all(r['Correct'] for r in pair),
                      'observed_reduction_percent': reductions(sub)})
result = {'scope': 'observed_scalar_metrics_shared_host_two_synthetic_edit_contracts',
          'input_rows_sha256': hashlib.sha256(rows_path.read_bytes()).hexdigest(),
          'driver_summary_sha256': hashlib.sha256(summary_path.read_bytes()).hexdigest(),
          'arms': arms, 'observed_reduction_percent': reductions(arms), 'per_repetition': by_repetition,
          'per_pair': pairs, 'original_phase_subset_diagnostic': summary['subset_economic_diagnostic'],
          'global_economic_gate': 'INCOMPLETE', 'effective_model_backend_cache_quota_equivalence': 'UNKNOWN',
          'workers_retries_delegations': 'UNKNOWN_NOT_INSTRUMENTED', 'cost': None,
          'time_scope': {'TotalMS': 'prefetch/provider/validation after edit admission',
                         'LocalElapsedMS': 'App subprocess including admission/probes measured by wait4',
                         'whole_session': 'NOT_MEASURED'},
          'phase_gate_modified': False, 'new_gate_defined_after_results': False,
          'historical_114_attempts_included_in_this_aggregate': False,
          'causal_savings_claim': False}
print(json.dumps(result, indent=2, sort_keys=True))
