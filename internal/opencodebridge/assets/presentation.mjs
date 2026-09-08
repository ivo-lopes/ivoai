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
