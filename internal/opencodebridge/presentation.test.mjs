import {test} from "node:test"
import assert from "node:assert/strict"
import fs from "node:fs"
import {fileURLToPath} from "node:url"
import {clean, logo, panel, servers, fitRows, cellWidth} from "./assets/presentation.mjs"

const source = (alias, selected = true, health = "healthy") => ({alias, purpose: alias, enabled: true, selected, health, auth_state: health === "healthy" ? "authenticated" : "not verified"})
const normal = {frontend:"opencode", primary:"codex", session_state:"running", selection_mode:"auto", effective_model:"fixture-model", effective_effort:"high", permission_mode:"interactive", resume_policy:"fresh native turn; identity unverified", codex_auth:"authenticated", codex_quota:"available", claude_auth:"not configured", claude_quota:"N/A", opencode_auth:"authenticated", opencode_quota:"unknown", memory:"ready", context:"ready", compression:"direct", skills:"policy-gated", version:"fixture", configured_count:2, connected_count:2, enabled_count:2, selected_count:2, knowledge_mode:"federated", servers:[source("source-A"),source("source-B")]}
const states = {
 capabilities:{...normal,workers:[{id:"implementation-a",role:"implementation",state:"running",executor:"codex",tier:"BALANCED",model:"fixture-balanced",effort:"medium",purposes:[],mcps:[],skills:["ponytail","caveman-surgical-patch"]},{id:"research-b",role:"research",state:"running",executor:"claude",tier:"LIGHT",model:"fixture-economical",effort:"low",purposes:[],mcps:[],skills:[]}]},
 sequential_patch:{...normal,prompt_readiness:"ready",plan_state:"running",task_count:2,workers_active:1,workers_queued:1,parallel_write_degraded:true,concurrency_policy:"sequential",concurrency_limit:1,worker_cap:2,quota_mode:"normal"},
 plan_approval:{...normal,prompt_readiness:"ready",plan_state:"waiting_for_plan_approval",task_count:3,workers_queued:2,concurrency_policy:"auto",worker_cap:2,knowledge_policy:"purpose-auto",quota_mode:"normal"},
 parallel_workers:{...normal,prompt_readiness:"ready",plan_state:"running",task_count:3,workers_active:2,workers_queued:1,concurrency_policy:"auto",concurrency_limit:2,worker_cap:2,knowledge_policy:"purpose-auto",quota_mode:"normal",workers:[{id:"implementation-a",role:"implementation",state:"running",executor:"codex",tier:"BALANCED",model:"fixture-balanced",effort:"medium",purposes:[],mcps:[]},{id:"research-b",role:"research",state:"running",executor:"claude",tier:"LIGHT",model:"fixture-economical",effort:"low",purposes:["source-B"],mcps:["plane"]}]},
 quota_approval:{...normal,prompt_readiness:"ready",plan_state:"waiting_for_routing_approval",task_count:3,workers_queued:2,quota_mode:"conservation_pending_confirmation",concurrency_policy:"auto",concurrency_limit:2,knowledge_policy:"purpose-auto"},
 normal,
 full:{...normal, permission_mode:"full"},
 interactive:{...normal,permission_mode:"interactive"},
 zero:{...normal,configured_count:0,connected_count:0,enabled_count:0,selected_count:0,servers:[],knowledge_mode:"none"},
 one:{...normal,configured_count:1,connected_count:1,enabled_count:1,selected_count:1,servers:[source("source-A")],knowledge_mode:"single"},
 federated:{...normal,configured_count:3,connected_count:3,enabled_count:3,selected_count:3,servers:[source("source-A"),source("source-B"),source("source-C")]},
 restricted:{...normal,selected_count:1,knowledge_mode:"restricted",servers:[source("source-A"),source("source-B",false)]},
 source_down:{...normal,connected_count:1,session_state:"degraded",servers:[source("source-A"),source("source-B",true,"down")]},
 stale:{...normal,codex_auth:"stale / not verified",codex_quota:"N/A",opencode_auth:"stale / not verified",opencode_quota:"N/A",servers:[source("source-A",true,"stale"),source("source-B",false)]},
 degraded:{...normal,session_state:"degraded",primary:"opencode",effective_model:"native-fixture/model",effective_effort:"UNKNOWN"},
 direct_fallback:{...normal,frontend:"direct",session_state:"degraded",resume_policy:"managed frontend unavailable; direct fallback"},
 explicit:{...normal,selection_mode:"explicit",primary:"opencode",requested_model:"native-fixture/model",effective_model:"native-fixture/model",effective_effort:"high"},
}
for (const [name, state] of Object.entries(states)) {
 test(`managed presentation golden: ${name}`, () => {
  const actual = JSON.stringify({logo,panel:panel(state),sidebar:servers(state)},null,2)+"\n"
  const path = fileURLToPath(new URL(`./testdata/presentation/${name}.golden.json`,import.meta.url))
  if (process.env.UPDATE_GOLDENS === "1") {assert.ok(!process.env.CI,"CI must not rewrite goldens");fs.mkdirSync(new URL("./testdata/presentation/",import.meta.url),{recursive:true});fs.writeFileSync(path,actual)}
  assert.equal(actual,fs.readFileSync(path,"utf8"),"review intentional changes with UPDATE_GOLDENS=1")
  for (const row of panel(state)) {
   assert.doesNotMatch(row.text,/[\x00-\x1f\x7f-\x9f]/)
   assert.doesNotMatch(row.text,/Bearer|access_token|refresh_token|auth\.json|password=/i)
  }
  assert.ok(actual.includes(`Permissions: ${state.permission_mode}`))
  assert.ok(actual.includes("Runtime"))
  for (const entry of state.servers) assert.ok(actual.includes(`session=${entry.selected?"selected":"excluded"}`))
 })
}
test("volatile metadata is not rendered; semantic metadata is not normalized",()=>{
 assert.deepEqual(panel({...normal,session_id:"random-id",updated_at:"random-time",port:43210}),panel(normal))
 assert.notDeepEqual(panel({...normal,permission_mode:"full"}),panel(normal))
 assert.notDeepEqual(panel({...normal,effective_model:"different"}),panel(normal))
 assert.equal(clean("test\x1b\u202e\x07"),"test")
})
for (const columns of [60,100,160]) test(`managed row layout golden: ${columns} columns`,()=>{
 const rows = fitRows(panel(states.source_down), columns - 8)
 for (const row of rows) assert.ok(cellWidth(row.text) <= columns - 8, "controlled row overflow")
 const actual = rows.map(row=>row.text).join("\n")+"\n"
 const path = fileURLToPath(new URL(`./testdata/presentation/layout-${columns}.golden.txt`,import.meta.url))
 if(process.env.UPDATE_GOLDENS==="1"){assert.ok(!process.env.CI,"CI must not rewrite goldens");fs.writeFileSync(path,actual)}
 assert.equal(actual,fs.readFileSync(path,"utf8"))
 assert.ok(actual.includes("IVOAI"));assert.ok(actual.includes("Permissions:"));assert.ok(actual.includes("selected"))
})
test("Unicode width and non-color state remain accessible",()=>{
 for(const width of [12,52,92,152])for(const row of fitRows([{text:"界 é 🧪 ".repeat(40),role:"text"}],width))assert.ok(cellWidth(row.text)<=width)
 for(const state of Object.values(states))for(const row of panel(state))if(row.role==="warning")assert.ok(row.text.startsWith("!"))
 assert.match(panel(states.stale).map(row=>row.text).join("\n"),/stale.*N\/A/)
})
test("orchestration metadata stays bounded at every supported width",()=>{
 for(const state of [states.plan_approval,states.parallel_workers,states.quota_approval,states.sequential_patch])for(const width of [60,100,160]){
  const rows=fitRows(panel(state),width-8)
  for(const row of rows)assert.ok(cellWidth(row.text)<=width-8)
  const text=rows.map(x=>x.text).join("\n")
  assert.match(text,/Prompt readiness: ready/)
  assert.match(text,/Quota mode:/)
  assert.doesNotMatch(text,/Bearer|access_token|prompt body|worker transcript/i)
 }
})
