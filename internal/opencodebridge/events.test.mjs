import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readOperationalStream,mergeActivity,createStreamWatchdog,initialOperationalStatus} from './assets/events.mjs'
import {panel} from './assets/presentation.mjs'

test('fragmented replay stream resumes without duplicates and resets across sessions',async()=>{
 const frame={replay:{schema_version:1,session_id:'s',latest_sequence:2,gap:false,events:[{sequence:1,operation:'worker.lifecycle',state:'running'},{sequence:2,operation:'worker.lifecycle',state:'completed'}]},status:{session_id:'s'}}
 const bytes=new TextEncoder().encode('id: 2\nevent: operational\ndata: '+JSON.stringify(frame)+'\n\n')
 const stream=new ReadableStream({start(c){for(let i=0;i<bytes.length;i+=7)c.enqueue(bytes.slice(i,i+7));c.close()}})
 const frames=[];await readOperationalStream(stream,f=>frames.push(f));assert.equal(frames.length,1)
 let state=mergeActivity({},frames[0]);state=mergeActivity(state,frames[0]);assert.equal(state.activity.length,2)
 state=mergeActivity(state,{replay:{...frame.replay,session_id:'new',events:[],gap:true},status:{session_id:'new'}})
 assert.equal(state.activity.length,0);assert.equal(state.activity_gap,true)
})
test('oversized and unknown-version streams fail boundedly',async()=>{
 for(const raw of ['x'.repeat(600000), 'event: operational\ndata: '+JSON.stringify({replay:{schema_version:9},status:{}})+'\n\n']){
  const stream=new ReadableStream({start(c){c.enqueue(new TextEncoder().encode(raw));c.close()}})
  await assert.rejects(readOperationalStream(stream,()=>{}))
 }
})
test('console presents actual dependency, duration and canonical recent activity',()=>{
 const rows=panel({workers:[{id:'w',task_id:'t',dependencies:['prepare'],duration_ms:1500}],activity_gap:true,activity:[{sequence:2,operation:'worker.lifecycle',state:'failed',task_id:'t',routing_reason:'validation_failed'}]})
 const text=rows.map(r=>r.text).join('\n');assert.match(text,/prepare/);assert.match(text,/1.5s/);assert.match(text,/validation_failed/);assert.match(text,/history.*gap/i)
})

test('phase progress distinguishes integration and synthesis without payload text',()=>{
 const text=panel({activity:[{sequence:1,operation:'orchestration.phase',phase:'integration',state:'running'}]}).map(x=>x.text).join('\n')
 assert.match(text,/integration/)
})


test('heartbeat watchdog bounds initial connection and stalled streams with injected clock',()=>{
 let now=0, next=0, failures=0;const timers=new Map()
 const clock={setTimeout(fn,delay){const id=++next;timers.set(id,{fn,at:now+delay});return id},clearTimeout(id){timers.delete(id)}}
 const advance=ms=>{now+=ms;for(const [id,timer] of [...timers])if(timer.at<=now){timers.delete(id);timer.fn()}}
 const initial=createStreamWatchdog(()=>failures++,clock)
 advance(4999);assert.equal(failures,0);advance(1);assert.equal(failures,1);initial.dispose()
 const active=createStreamWatchdog(()=>failures++,clock);active.touch()
 advance(44000);assert.equal(failures,1);active.touch();advance(44000);assert.equal(failures,1);advance(1000);assert.equal(failures,2)
 const disposed=createStreamWatchdog(()=>failures++,clock);disposed.dispose();advance(60000);assert.equal(failures,2)
})


test('status-only bridge supplies useful initial panel before unavailable events',async()=>{
 const calls=[]
 const fetcher=async(url,options)=>{calls.push(url);assert.equal(options.headers.Authorization,'Bearer fixture');return new Response(JSON.stringify({session_id:'s',permission_mode:'full',knowledge_mode:'restricted',servers:[{alias:'source-A'},{alias:'source-B'}]}),{status:200,headers:{'Content-Type':'application/json'}})}
 const status=await initialOperationalStatus('http://127.0.0.1:1234','fixture',new AbortController().signal,fetcher)
 const stale={...status,session_state:'stale'}
 const text=panel(stale).map(row=>row.text).join('\n')
 assert.match(text,/Permissions: full/);assert.match(text,/source-A/);assert.match(text,/source-B/);assert.match(text,/restricted/)
 assert.deepEqual(calls,['http://127.0.0.1:1234/status'])
})
test('initial snapshot rejects unauthorized and oversized responses',async()=>{
 for(const response of [new Response('denied',{status:401}),new Response('x'.repeat(524289)),new Response('[]')]){
  await assert.rejects(initialOperationalStatus('http://127.0.0.1:1','fixture',new AbortController().signal,async()=>response))
 }
})

test('structural observations show measured values and preserve unknown resources',()=>{
 const text=panel({activity:[{operation:'code.inspect',component:'code_intelligence',provider:'graft',state:'completed',cache_hit:true,index_fresh:true,index_files:19,source_files:20,duration_ms:12}]}).map(x=>x.text).join('\n')
 assert.match(text,/cache=hit/);assert.match(text,/19\/20/);assert.match(text,/12ms/);assert.match(text,/memory=unknown/);assert.match(text,/fresh/)
})

test('last observed code summary survives eight newer lifecycle rows without inventing freshness',()=>{
 const observed='2026-10-12T22:00:00Z'
 const events=[{sequence:1,operation:'code.inspect',provider:'graft',state:'completed',observed_at:observed,index_fresh:true,cache_hit:false,index_files:2,source_files:2},
  ...Array.from({length:12},(_,i)=>({sequence:i+2,operation:'task.lifecycle',state:'completed',task_id:'fixture'}))]
 const state=mergeActivity({}, {replay:{session_id:'fixture',events,gap:false},status:{session_id:'fixture'}})
 const rows=panel(state).map(row=>row.text)
 assert.equal(rows.filter(text=>text.startsWith('task.lifecycle')).length,8)
 assert.ok(!rows.some(text=>text.startsWith('code.inspect')))
 assert.match(rows.join('\n'),/Code \(last observed\): graft/)
 assert.match(rows.join('\n'),/observed=2026-10-12T22:00:00Z/)
 assert.match(rows.join('\n'),/index=fresh/)
 const other=mergeActivity(state,{replay:{session_id:'other',events:[],gap:false},status:{session_id:'other'}})
 assert.doesNotMatch(panel(other).map(row=>row.text).join('\n'),/Code \(last observed\)/)
})
