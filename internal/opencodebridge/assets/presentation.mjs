// Shared by the managed TUI and its deterministic structural goldens. No
// network, timestamps, secrets, terminal state, or provider inference here.
export const clean = (value, fallback = "N/A", limit = 80) => {
  if (typeof value !== "string") return fallback
  const text = value.replace(/[\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/g, "").slice(0, limit)
  return text || fallback
}
export const logo = ["IVOAI", "OpenCode frontend · IVOAI control plane"]
const line = (text, role = "muted") => ({text, role})
export function servers(status) {
  const sources = Array.isArray(status.servers) ? status.servers : []
  const result = [line("IVOAI knowledge", "heading"), line(`${status.connected_count ?? 0} connected / ${status.configured_count ?? 0} configured · ${clean(status.knowledge_mode)}`)]
  for (const source of sources.slice(0, 8)) {
    const mark = !source.enabled || !source.selected ? "○" : source.health === "healthy" ? "✓" : "!"
    result.push(line(`${mark} ${clean(source.alias)}`, mark === "!" ? "warning" : "text"),
      line(`  purpose=${clean(source.purpose, "unspecified")} · ${source.enabled ? "enabled" : "disabled"}`),
      line(`  ${clean(source.health)} · session=${source.selected ? "selected" : "excluded"}`),
      line(`  auth=${clean(source.auth_state, "not verified")}`))
  }
  if (sources.length > 8) result.push(line(`+${sources.length - 8} more sources`))
  return result
}
export function panel(status) {
  const result = [line("IVOAI", "identity"), line("Session", "heading"),
    line(`Permissions: ${clean(status.permission_mode)}`),
    line(`Resume: ${clean(status.resume_policy, "not exposed")}`),
    line(`frontend=${clean(status.frontend, "OpenCode")} · primary=${clean(status.primary)} · state=${clean(status.session_state)}`),
    line(`mode=${clean(status.selection_mode, "auto")} · requested=${clean(status.requested_model, "automatic")} · model=${clean(status.effective_model, "UNKNOWN")} · reasoning=${clean(status.effective_effort, "UNKNOWN")}`),
    line("Executors", "heading")]
  const orchestration = []
  if (status.prompt_readiness) {
    orchestration.push(line(`Prompt readiness: ${clean(status.prompt_readiness)}`))
    for (const field of (Array.isArray(status.prompt_missing) ? status.prompt_missing : []).slice(0, 5)) orchestration.push(line(`Missing: ${clean(field)}`, "warning"))
  }
  if (status.plan_state) orchestration.push(line(`Plan: ${clean(status.plan_state)} · ${Number.isSafeInteger(status.task_count) ? status.task_count : 0} tasks`), line(`Workers: ${Number.isSafeInteger(status.workers_active) ? status.workers_active : 0} active / ${Number.isSafeInteger(status.workers_queued) ? status.workers_queued : 0} queued / ${Number.isSafeInteger(status.workers_done) ? status.workers_done : 0} done`))
  if (status.concurrency_policy) orchestration.push(line(`Concurrency: ${clean(status.concurrency_policy)} → ${Number.isSafeInteger(status.concurrency_limit) && status.concurrency_limit > 0 ? status.concurrency_limit : "pending DAG"} · cap=${status.worker_cap > 0 ? status.worker_cap : "auto"}`))
  if (status.knowledge_policy) orchestration.push(line(`Knowledge routing: ${clean(status.knowledge_policy)} · worker MCPs: deny-by-default`))
  if (status.quota_mode) orchestration.push(line(`Quota mode: ${clean(status.quota_mode)}`))
  if (status.parallel_write_degraded) orchestration.push(line("Parallel writes: degraded · worktree unavailable · sequential checked patches / read-only workers"))
  for (const worker of (Array.isArray(status.workers) ? status.workers : []).slice(0, 12)) {
    orchestration.push(line(`${clean(worker.id)} · ${clean(worker.role)} · ${clean(worker.state)}`),
      line(`  ${clean(worker.executor)} / ${clean(worker.tier)} / ${clean(worker.model)} · reasoning=${clean(worker.effort, "unsupported")}`),
      line(`  purposes=${(Array.isArray(worker.purposes) ? worker.purposes : []).slice(0, 8).map(x => clean(x)).join(", ") || "none"} · MCPs=${(Array.isArray(worker.mcps) ? worker.mcps : []).slice(0, 16).map(x => clean(x)).join(", ") || "none"}`))
    if (worker.task_id || worker.dependencies?.length || worker.duration_ms > 0) orchestration.push(line(`  task=${clean(worker.task_id)} · dependencies=${(Array.isArray(worker.dependencies) ? worker.dependencies : []).slice(0, 16).map(x => clean(x)).join(", ") || "none"} · duration=${Number.isFinite(worker.duration_ms) && worker.duration_ms >= 0 ? (worker.duration_ms / 1000).toFixed(1) + "s" : "unavailable"}`))
    if (worker.blocked_reason) orchestration.push(line(`  blocked=${clean(worker.blocked_reason)}`, "warning"))
    if (Array.isArray(worker.skills)) orchestration.push(line(`  Skills=${worker.skills.slice(0, 12).map(x => clean(x)).join(", ") || "none"} · Ponytail=${worker.skills.includes("ponytail") ? "active" : "inactive"}`))
  }
  if (status.activity_gap) orchestration.push(line("Activity history gap · showing retained events", "warning"))
  const activity = (Array.isArray(status.activity) ? status.activity : []).slice(-8)
  if (activity.length) orchestration.push(line("Recent activity", "heading"))
  for (const event of activity) orchestration.push(line(`${clean(event.operation)}${event.phase ? " · " + clean(event.phase) : ""} · ${clean(event.task_id || event.worker_id || event.component, "session")} · ${clean(event.state)}${event.routing_reason ? " · " + clean(event.routing_reason) : ""}${event.fallback_reason ? " · fallback=" + clean(event.fallback_reason) : ""}`, ["failed", "blocked", "denied"].includes(event.state) ? "warning" : "muted"))
  const code = activity.filter(event => event.operation === "code.inspect").at(-1)
  if (code) {
    const metric = (value, suffix = "") => Number.isFinite(value) && value >= 0 ? value + suffix : "unknown"
    orchestration.push(line(`Code: ${clean(code.provider)} · cache=${typeof code.cache_hit === "boolean" ? code.cache_hit ? "hit" : "miss" : "unknown"} · index=${typeof code.index_fresh === "boolean" ? code.index_fresh ? "fresh" : "stale" : "unknown"}`),
      line(`  coverage=${metric(code.index_files)}/${metric(code.source_files)} files · latency=${metric(code.duration_ms, "ms")} · memory=${code.peak_memory_bytes == null ? "unknown" : metric(Math.round(code.peak_memory_bytes / 1048576), "MiB")} · cpu=${metric(code.cpu_ms, "ms")}`))
  }
  result.splice(2, 0, ...orchestration)
  for (const [id, name] of [["codex", "Codex"], ["claude", "Claude"], ["opencode", "OpenCode"]]) {
    const auth = clean(status[id + "_auth"])
    result.push(line(`${auth === "authenticated" ? "✓" : "!"} ${name} ${auth} · quota=${clean(status[id + "_quota"])}`))
  }
  result.push(...servers(status), line("Runtime", "heading"),
    line(`compression=${clean(status.compression)} · memory=${clean(status.memory)} · context=${clean(status.context)}`),
    line(`skills=${clean(status.skills)} · IVOAI ${clean(status.version)}`))
  return result
}
// Grapheme-aware conservative cell widths: combining sequences stay intact;
// wide CJK and emoji occupy two cells. OpenTUI may wrap again in a narrower
// dialog/sidebar, but no IVOAI row asks for a no-wrap overflow.
const graphemes = new Intl.Segmenter("en", {granularity:"grapheme"})
export function cellWidth(text) {
  let width = 0
  for (const {segment} of graphemes.segment(text)) {
    width += /[\p{Extended_Pictographic}\p{Script=Han}\p{Script=Hangul}\p{Script=Hiragana}\p{Script=Katakana}\uFF01-\uFF60\uFFE0-\uFFE6]/u.test(segment) ? 2 : 1
  }
  return width
}
export function fitRows(rows, columns) {
  const limit = Math.max(12, Math.min(160, Math.floor(columns)))
  return rows.flatMap(row => {
    const result = []
    let text = "", width = 0
    for (const {segment} of graphemes.segment(row.text)) {
      const size = cellWidth(segment)
      if (width + size > limit) {result.push({...row,text});text="";width=0}
      text += segment; width += size
    }
    result.push({...row,text})
    return result
  })
}
// OpenTUI remains responsible for actual Unicode cell layout and resize. These
// rows deliberately contain no unbounded single-line boxes: text wraps natively.
