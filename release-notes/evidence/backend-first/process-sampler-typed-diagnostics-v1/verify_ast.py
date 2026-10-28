import ast,copy,hashlib,json,pathlib,subprocess,sys
ROOT=pathlib.Path('/home/ivo/Downloads/PROJETOS/MINDSITE/ivoai-sampler-calibration');OUT=pathlib.Path('/var/tmp/ivoai-sampler-typed-diagnostics-20261028');BASE='d7bd3a04f0bd8c1192ee7abc1d56fc972ac0e3c3'
old=ast.parse(subprocess.check_output(['git','-C',str(ROOT),'show',BASE+':scripts/benchmark_sampler_calibration.py'],text=True));new=ast.parse((ROOT/'scripts/benchmark_sampler_calibration.py').read_text())
def field(node):
 return isinstance(node,ast.Subscript) and isinstance(node.value,ast.Name) and node.value.id=='row' and isinstance(node.slice,ast.Constant) and node.slice.value in ('failure_reason','failure_errno')
class DiagnosticProjection(ast.NodeTransformer):
 def visit_Dict(self,node):
  node=self.generic_visit(node);keep=[i for i,k in enumerate(node.keys) if not(isinstance(k,ast.Constant) and k.value in ('failure_reason','failure_errno'))];node.keys=[node.keys[i] for i in keep];node.values=[node.values[i] for i in keep];return node
 def visit_Assign(self,node):
  targets=node.targets[0].elts if len(node.targets)==1 and isinstance(node.targets[0],ast.Tuple) else node.targets
  if targets and all(field(t) for t in targets):return None
  return self.generic_visit(node)
 def visit_ExceptHandler(self,node):
  if node.name=='error':
   assert ast.dump(node.type)==ast.dump(ast.parse('try:\n pass\nexcept (OSError, ValueError, TimeoutError, subprocess.TimeoutExpired):\n pass').body[0].handlers[0].type)
   node.name=None
  return self.generic_visit(node)
 def visit_Try(self,node):
  # Exactly one diagnostic JSON catch; no broader try/call/control removal.
  if len(node.body)==1 and len(node.handlers)==1 and isinstance(node.body[0],ast.Assign) and ast.dump(node.body[0].value)==ast.dump(ast.parse('json.loads(body)',mode='eval').body):
   handler=node.handlers[0];assert isinstance(handler.type,ast.Name) and handler.type.id=='ValueError' and len(handler.body)==1
   raise_node=handler.body[0];assert isinstance(raise_node,ast.Raise) and ast.dump(raise_node.exc)==ast.dump(ast.parse("ProtocolFailure('invalid_json')",mode='eval').body) and not node.orelse and not node.finalbody
   return self.visit(node.body[0])
  return self.generic_visit(node)
 def visit_Raise(self,node):
  if isinstance(node.exc,ast.Call) and isinstance(node.exc.func,ast.Name):
   if node.exc.func.id=='ProtocolFailure':
    assert len(node.exc.args)==1 and isinstance(node.exc.args[0],ast.Constant)
    reason=node.exc.args[0].value;assert reason in ('eof','oversize','shape','ready_mismatch','oracle_mismatch')
    return ast.Raise(exc=ast.Call(func=ast.Name(id='ValueError',ctx=ast.Load()),args=[ast.Constant(value='fixture' if reason=='oracle_mismatch' else 'protocol')],keywords=[]),cause=None)
   if node.exc.func.id=='ProtocolDeadline':
    assert not node.exc.args and not node.exc.keywords
    return ast.Raise(exc=ast.Call(func=ast.Name(id='TimeoutError',ctx=ast.Load()),args=[ast.Constant(value='protocol')],keywords=[]),cause=None)
  return self.generic_visit(node)
functions=lambda tree:{n.name:n for n in tree.body if isinstance(n,ast.FunctionDef)}
a,b=functions(old),functions(new)
unchanged=[]
for name,node in a.items():
 normalized=DiagnosticProjection().visit(copy.deepcopy(b[name]));assert ast.dump(node,include_attributes=False)==ast.dump(normalized,include_attributes=False),name
 unchanged.append(name)
constants=lambda tree:{n.targets[0].id:ast.dump(n.value,include_attributes=False) for n in tree.body if isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name)}
a,b=constants(old),constants(new);assert all(b[k]==v for k,v in a.items())
for name in ['scripts/benchmark_process_sampler.py','scripts/benchmark-economic-runtime.py']:
 assert (ROOT/name).read_bytes()==subprocess.check_output(['git','-C',str(ROOT),'show',BASE+':'+name])
sources={name:hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in ['scripts/benchmark_sampler_calibration.py','scripts/test_benchmark_sampler_calibration.py','scripts/benchmark_process_sampler.py','scripts/benchmark-economic-runtime.py']}
value={'base':BASE,'normalized_existing_functions_identical':unchanged,'constants_identical':list(a),'diagnostic_whitelist':['nullable row reason/errno fields','typed ValueError/TimeoutError replacements preserving predicates','one ValueError JSON wrapper mapping invalid_json','exception binding and failure_details assignment'],'sampler_driver_bytes_identical':True,'source_sha256':sources,'verifier_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'no_new_workload_cohort':True,'original_cause':'INCONCLUSIVE/DRAFT'}
(OUT/'source-freeze.json').write_text(json.dumps(value,indent=2)+'\n');print('ASTidentityPASS all original functions/limits; source frozen before tests')
