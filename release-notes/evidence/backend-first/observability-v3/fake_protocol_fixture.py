#!/usr/bin/python3
# FAKE local process only: commands/MCP events and usage below are fabricated.
import json,pathlib,sys
args=sys.argv[1:];root=pathlib.Path(args[args.index('-C')+1]);prompt=args[-1]
if 'Clamp(value,min,max)' in prompt:
 p=root/'mathpkg/clamp.go';p.write_text(p.read_text().replace('if value > max { return min }','if value > max { return max }'))
 answer={'file':'mathpkg/clamp.go','lower':0,'inside':10,'upper':20}
else:
 for name in ['packages/shared/src/discount.ts','packages/shop/src/main.ts','packages/admin/src/report.ts']:
  p=root/name;p.write_text(p.read_text().replace('discount','applyDiscount').replace('/applyDiscount.ts','/discount.ts'))
 answer={'export':'applyDiscount','call_sites':2,'shop_result':90,'admin_result':72}
print(json.dumps({'type':'thread.started','thread_id':'PRIVATE_THREAD'}))
print(json.dumps({'type':'turn.started'}))
for n in range(2):
 print(json.dumps({'type':'item.completed','item':{'id':'PRIVATE_COMMAND_'+str(n),'type':'command_execution','status':'completed','command':'PRIVATE_COMMAND /private/path SECRET','aggregated_output':'PRIVATE_OUTPUT'}}))
print(json.dumps({'type':'item.completed','item':{'id':'PRIVATE_MCP','type':'mcp_tool_call','server':'PRIVATE_SERVER','tool':'PRIVATE_TOOL','status':'completed','arguments':{'SECRET':'PRIVATE_ARGUMENT'}}}))
print(json.dumps({'type':'item.completed','item':{'id':'PRIVATE_FINAL','type':'agent_message','text':json.dumps(answer)}}))
print(json.dumps({'type':'turn.completed','usage':{'input_tokens':100,'cached_input_tokens':60,'output_tokens':2}}))
