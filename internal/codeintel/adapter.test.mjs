import test from 'node:test';
import assert from 'node:assert/strict';
import { execute } from './assets/adapter.mjs';

test('structural execution is offline and bounded, without maintenance entrypoints', async()=>{
 const imported=[];let options;
 const result=await execute({distribution:'/runtime',root:'/source',cache:'/cache',build:true,operation:'ask',query:'Run',limit:3,depth:1,direction:'in',budget_bytes:256}, async name=>{
 imported.push(name);
 if(name==='graph/build.js')return {buildGraph:async(root,opts)=>{assert.equal(root,'/source');options=opts;return {files:1,nodes:2,parsed:1,reused:0}}};
 if(name==='mcp/tools.js')return {callTool:async(root,name,args,cache)=>{assert.equal(name,'graft_find_code');assert.equal(cache,'/cache');assert.equal(args.full,false);return {text:'x'.repeat(1000),isError:false}}};
 throw new Error(`unexpected import ${name}`);
 });
 assert.deepEqual(imported,['graph/build.js','mcp/tools.js']);
 assert.equal(options.graphOnly,true);assert.equal(options.lsp,false);assert.equal(options.summarizer,undefined);
 assert.ok(Buffer.byteLength(result.text)<=256);assert.equal(result.truncated,true);assert.equal(result.files,1);
});
test('all six native operations map to controlled queries',async()=>{
 const expected={ask:'graft_find_code',grep:'graft_find_all',skeleton:'graft_file_api',callers:'graft_trace_calls',map:'graft_repo_map',check:'graft_check_freshness'};
 for(const [operation,tool] of Object.entries(expected)){
  await execute({operation,root:'/source',cache:'/cache',budget_bytes:256,query:'a',file:'a.go',symbol:'a',depth:2,limit:2},async name=>{
   assert.equal(name,'mcp/tools.js');return{callTool:async(root,name,args)=>{assert.equal(name,tool);if(operation==='grep')assert.equal(args.fixed,true);return{text:'ok',isError:false}}};
  });
 }
});
test('upstream failure cannot masquerade as a successful query',async()=>{
 await assert.rejects(execute({operation:'ask',root:'/source',cache:'/cache',budget_bytes:256},async()=>({callTool:async()=>({text:'private error',isError:true})})),/structural query failed/);
});
test('file impact uses reviewed blast report with bounded supplied line ranges',async()=>{
 const imports=[];
 const report=await execute({operation:'blast',file:'a.go',start_line:2,end_line:4,cache:'/cache',depth:2,budget_bytes:4096},async name=>{
  imports.push(name);
  if(name==='graph/load.js')return{loadGraphCached:()=>({nodes:[],edges:[]})};
  if(name==='blast/blast.js')return{blastRadius:(graph,changes,basis,opts)=>{assert.equal(changes[0].path,'a.go');assert.deepEqual(changes[0].ranges,[{start:2,end:4}]);assert.equal(opts.depth,2);return{basis,impacted:[],unindexed:['a.go']}}};
  throw new Error('unexpected import '+name);
 });
 assert.deepEqual(imports,['graph/load.js','blast/blast.js']);assert.match(report.text,/unindexed/);
});
