#!/usr/bin/env python3
"""Reproduce supplemental accounting of immutable existing rows; no inference or gate changes."""
from pathlib import Path
import hashlib,json,statistics
relative=Path('release-notes/evidence/backend-first/candidate-gateway-v3/runtime-comparison.json')
source=Path(__file__).with_name('runtime-comparison.json')
original=source.read_bytes()
assert hashlib.sha256(original).hexdigest()=='de3e1156a48fc42fa6eba3a2e0efb0310e9818f86a8de620b2144fe5e3a87bc9'
rows=json.loads(original)
assert len(rows)==18
pairs={}
for row in rows:
 key=(row['Scenario'],row['Repetition']);assert row['Arm'] not in pairs.setdefault(key,{})
 pairs[key][row['Arm']]=row
assert len(pairs)==9 and all(set(pair)=={'A','B'}for pair in pairs.values())
table=[]
for(scenario,repetition),pair in sorted(pairs.items()):
 a,b=pair['A'],pair['B'];delta=a['InputTokens']-b['InputTokens']
 table.append({'scenario':scenario,'repetition':repetition,'A_input_tokens':a['InputTokens'],'B_input_tokens':b['InputTokens'],
 'input_delta_A_minus_B':delta,'input_reduction_percent':100*delta/a['InputTokens'],
 'A_prompt_bytes':a['PromptBytes'],'B_prompt_bytes':b['PromptBytes'],'prompt_byte_delta_A_minus_B':a['PromptBytes']-b['PromptBytes'],
 'A_source_bytes':a['SourceBytes'],'B_source_bytes':b['SourceBytes'],'source_bytes_equal':a['SourceBytes']==b['SourceBytes'],
 'A_correct':a['Correct'],'B_correct':b['Correct']})
large=sorted(table,key=lambda row:(-row['input_delta_A_minus_B'],row['scenario'],row['repetition']))[:3]
a_total=sum(p['A']['InputTokens']for p in pairs.values());b_total=sum(p['B']['InputTokens']for p in pairs.values())
median=statistics.median([row['input_reduction_percent']for row in table])
report={'schema_version':1,'kind':'supplemental_context_accounting_diagnostic','not_scored':True,'no_rows_excluded':True,
'new_inference_attempts':0,'input_file':str(relative),'input_sha256':hashlib.sha256(original).hexdigest(),
'generator_file':Path(__file__).name,'generator_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
'input_row_count':len(rows),'pair_count':len(table),'method':{'group_by':['Scenario','Repetition'],'pair_input_delta':'A.InputTokens - B.InputTokens',
'pair_reduction_percent':'100 * (A.InputTokens - B.InputTokens) / A.InputTokens',
'median':'statistics.median of all nine pair_reduction_percent values',
'decomposition':'Three largest positive A-minus-B input deltas; descriptive post-hoc accounting only, no rows excluded'},
'pairs':table,'aggregate':{'A_input_tokens':a_total,'B_input_tokens':b_total,'input_delta_A_minus_B':a_total-b_total,
'input_reduction_percent':100*(a_total-b_total)/a_total,'median_pair_reduction_percent':median},
'largest_three_positive_deltas':{'pairs':[{'scenario':row['scenario'],'repetition':row['repetition']}for row in large],
'delta_sum':sum(row['input_delta_A_minus_B']for row in large),'remaining_six_delta_sum':a_total-b_total-sum(row['input_delta_A_minus_B']for row in large),
'prompt_byte_deltas_A_minus_B':[row['prompt_byte_delta_A_minus_B']for row in large],
'absolute_prompt_byte_differences':sorted([abs(row['prompt_byte_delta_A_minus_B'])for row in large],reverse=True),
'source_bytes_equal_within_each_pair':all(row['source_bytes_equal']for row in large)},
'interpretation':{'provider_context_accounting_origin':'unexplained_not_investigated_or_attested',
'cache_or_reuse_causality_established':False,'backend_model_identity_established':False,
'host_bytes_are_provider_input_token_attribution':False,'economic_gate_changed':False,'subset_economic_gate':'FAIL','global_economic_gate':'INCOMPLETE'}}
assert report['aggregate']['input_delta_A_minus_B']==7686
assert report['largest_three_positive_deltas']['delta_sum']==7687
assert report['largest_three_positive_deltas']['remaining_six_delta_sum']==-1
assert median==0.020191142818683536
output=source.with_name('context-accounting-diagnostic.json');output.write_text(json.dumps(report,indent=2,allow_nan=False)+'\n')
assert source.read_bytes()==original
print(json.dumps({'delta':a_total-b_total,'median':median,'large_pairs':report['largest_three_positive_deltas'],'source_sha256':report['input_sha256']},indent=2))
