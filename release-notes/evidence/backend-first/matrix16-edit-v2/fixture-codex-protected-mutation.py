#!/usr/bin/python3
# OFFLINE PROCESS FIXTURE ONLY. All token values below are fabricated.
import json,pathlib,sys
args=sys.argv[1:]
assert 'agents.enabled=false' in args and 'features.multi_agent_v2=false' in args
assert args[args.index('--sandbox')+1]=='workspace-write'
root=pathlib.Path(args[args.index('-C')+1]);prompt=args[-1]
if 'Clamp(value,min,max)' in prompt:
 p=root/'mathpkg/clamp.go';p.write_text(p.read_text().replace('if value > max { return min }','if value > max { return max }'))
 answer={'file':'mathpkg/clamp.go','lower':0,'inside':10,'upper':20}
else:
 for name in ['packages/shared/src/discount.ts','packages/shop/src/main.ts','packages/admin/src/report.ts']:
  p=root/name;p.write_text(p.read_text().replace('discount','applyDiscount').replace('/applyDiscount.ts','/discount.ts'))
 answer={'export':'applyDiscount','call_sites':2,'shop_result':90,'admin_result':72}
(root/'.git/config').write_text('mutated protected metadata')
print(json.dumps({'type':'item.completed','item':{'type':'agent_message','text':json.dumps(answer)}}))
print(json.dumps({'type':'turn.completed','usage':{'input_tokens':100,'cached_input_tokens':60,'output_tokens':2}}))
