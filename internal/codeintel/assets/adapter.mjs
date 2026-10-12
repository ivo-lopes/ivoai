// IVOAI-owned entrypoint for the reviewed Graft distribution. Never import CLI,
// MCP server, dotenv, upkeep, hooks or cloud wiring. The host additionally applies
// a filesystem/network/process sandbox before Node starts.
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { readFileSync } from 'node:fs';

export async function execute(request, load = name => import(pathToFileURL(join(request.distribution,'dist',name)).href)) {
 let built;
 if(request.build) {
  const {buildGraph}=await load('graph/build.js');
  built=await buildGraph(request.root,{contextDir:request.cache,graphOnly:true,reuse:true,lsp:false});
 }
 let text;
 if(request.operation==='build') text=JSON.stringify({files:built?.files,nodes:built?.nodes,parsed:built?.parsed,reused:built?.reused});
 else if(request.operation==='blast' && request.file) {
  const {loadGraphCached}=await load('graph/load.js');
  const graph=loadGraphCached(request.cache);if(!graph)throw new Error('missing graph');
  const {blastRadius}=await load('blast/blast.js');
  const ranges=request.start_line?[{start:request.start_line,end:request.end_line}]:[];
  text=JSON.stringify(blastRadius(graph,[{path:request.file,status:'modified',ranges,hunks:[]}],'Explicit authorized file/line selection; structural evidence only',{depth:request.depth}));
 }
 else if(request.operation==='viz') {
  const {exportViz}=await load('viz/export.js');
  const result=exportViz({contextDir:request.cache,viewerDir:join(request.distribution,'dist','viewer'),outDir:join(request.cache,'visualization'),repoName:'Authorized repository',tabs:['code','outline']});
  text=JSON.stringify({artifact:'visualization/index.html',bytes:result.bytes});
 } else {
  const names={ask:'graft_find_code',grep:'graft_find_all',skeleton:'graft_file_api',callers:'graft_trace_calls',blast:'graft_trace_calls',map:'graft_repo_map',check:'graft_check_freshness'};
  if(!names[request.operation])throw new Error('unsupported structural operation');
  const {callTool}=await load('mcp/tools.js');
  const result=await callTool(request.root,names[request.operation],{
   query:request.query,pattern:request.query,file:request.file,symbol:request.symbol||request.file,
   direction:request.direction,depth:request.depth,limit:request.limit,max_dirs:request.limit,
   fixed:true,full:false,in:request.in,ignore_case:request.ignore_case,
  },request.cache);
  if(result.isError)throw new Error('structural query failed');
  // Upstream's theoretical whole-file comparison is not IVOAI telemetry.
  text=result.text.split('\n\n').filter(p=>!p.startsWith('[graft] tokens saved')).join('\n\n');
 }
 const raw=Buffer.from(text,'utf8');
 let bounded=raw.subarray(0,request.budget_bytes).toString('utf8');
 // Avoid a replacement character and budget overrun from a cut UTF-8 sequence.
 while(Buffer.byteLength(bounded)>request.budget_bytes||bounded.endsWith('\ufffd'))bounded=bounded.slice(0,-1);
 return {text:bounded,truncated:raw.length>request.budget_bytes,files:built?.files,nodes:built?.nodes,parsed:built?.parsed,reused:built?.reused};
}

if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href){
 try {
  const body=readFileSync(0,'utf8');if(Buffer.byteLength(body)>16384)throw new Error('request budget exceeded');
  process.env.GRAFT_NO_REFRESH='1';process.env.GRAFT_NO_SEED='1';process.env.DO_NOT_TRACK='1';
  const request=JSON.parse(body);process.env.GRAFT_DIR=request.cache;
  process.stdout.write(JSON.stringify(await execute(request)));
 }catch{process.stderr.write('code intelligence operation failed\n');process.exitCode=1;}
}
