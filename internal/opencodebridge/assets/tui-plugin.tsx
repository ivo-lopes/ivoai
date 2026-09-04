/** @jsxImportSource @opentui/solid */
import { For, Show, createSignal, onCleanup } from "solid-js"

const clean = (value: unknown, fallback = "N/A") => {
  if (typeof value !== "string") return fallback
  const text = value.replace(/[\u0000-\u001f\u007f-\u009f\u001b\u202a-\u202e\u2066-\u2069]/gi, "").slice(0, 40)
  return text || fallback
}

const tui = async (api: any, options: any) => {
  const [status, setStatus] = createSignal<any>({
    configured_count: 0,
    enabled_count: 0,
    connected_count: 0,
    selected_count: 0,
    servers: [],
    knowledge_mode: "automatic",
  })
  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined

  const refresh = async () => {
    if (stopped) return
    try {
      const response = await fetch(options.bridge + "/status", {
        headers: { Authorization: "Bearer " + options.token },
        signal: AbortSignal.timeout(2000),
      })
      if (response.ok) setStatus(await response.json())
    } catch {
      setStatus((value: any) => ({ ...value, session_state: "stale" }))
    } finally {
      if (!stopped) timer = setTimeout(refresh, 5000)
    }
  }
  void refresh()
  onCleanup(() => {
    stopped = true
    if (timer) clearTimeout(timer)
  })
  api.lifecycle.onDispose(() => {
    stopped = true
    if (timer) clearTimeout(timer)
  })
  await api.theme.install(options.theme)
  api.theme.set("ivoai")

  const theme = () => api.theme.current
  const stateMark = (value: any) => {
    if (!value.enabled || !value.selected) return "○"
    if (value.health === "healthy") return "✓"
    return "!"
  }
  const stateColor = (value: any) => {
    if (!value.enabled || !value.selected) return theme().textMuted
    if (value.health === "healthy") return theme().success
    return theme().warning
  }
  const authMark = (value: unknown) => clean(value) === "authenticated" ? "✓" : "!"
  const visibleServers = () => (status().servers || []).slice(0, 8)

  const knowledgeMark = () => {
    if (status().enabled_count === 0) return "○"
    return status().connected_count >= status().enabled_count ? "✓" : "!"
  }
  const knowledgeColor = () => {
    if (status().enabled_count === 0) return theme().textMuted
    return status().connected_count >= status().enabled_count ? theme().success : theme().warning
  }

  const Logo = () => (
    <box flexDirection="column" alignItems="center" paddingBottom={1}>
      <text fg={theme().primary}><b>IVOAI</b></text>
      <text fg={theme().textMuted}>OpenCode frontend · IVOAI control plane</text>
    </box>
  )
  const Summary = () => (
    <box flexDirection="row" gap={2} paddingLeft={2} paddingRight={2}>
      <text fg={theme().text}>
        <span style={{ fg: knowledgeColor() }}>
          {knowledgeMark()}
        </span>{" "}
        Knowledge {status().connected_count}/{status().configured_count}
      </text>
      <text fg={theme().textMuted}>{clean(status().knowledge_mode)}</text>
      <text fg={theme().accent}>
        {clean(status().selection_mode, "auto")} · {clean(status().primary)} · {clean(status().effective_model || status().requested_model, "client default")} · {clean(status().effective_effort, "default")}
      </text>
      <text fg={theme().textMuted}>/ivoai</text>
    </box>
  )
  const Servers = () => (
    <box flexDirection="column" gap={1}>
      <text fg={theme().text}><b>IVOAI knowledge</b></text>
      <text fg={theme().textMuted}>
        {status().connected_count} connected / {status().configured_count} configured · {clean(status().knowledge_mode)}
      </text>
      <For each={visibleServers()}>
        {(server: any) => (
          <box flexDirection="column">
            <text fg={stateColor(server)}>{stateMark(server)} {clean(server.alias)}</text>
            <text fg={theme().textMuted}>  purpose={clean(server.purpose, "unspecified")} · {server.selected ? clean(server.health) : "session=excluded"}</text>
          </box>
        )}
      </For>
      <Show when={(status().servers || []).length > visibleServers().length}>
        <text fg={theme().textMuted}>+{(status().servers || []).length - visibleServers().length} more sources</text>
      </Show>
    </box>
  )
  const Panel = () => (
    <box flexDirection="column" gap={1} padding={2}>
      <text fg={theme().primary}><b>IVOAI</b></text>
      <text fg={theme().text}>Session</text>
      <text fg={theme().textMuted}>frontend=OpenCode · primary={clean(status().primary)} · state={clean(status().session_state)}</text>
      <text fg={theme().textMuted}>mode={clean(status().selection_mode, "auto")} · requested={clean(status().requested_model, "automatic")} · model={clean(status().effective_model, "client default")} · reasoning={clean(status().effective_effort, "default")}</text>
      <text fg={theme().text}>Executors</text>
      <text fg={theme().textMuted}>{authMark(status().codex_auth)} Codex {clean(status().codex_auth)} · quota={clean(status().codex_quota)}</text>
      <text fg={theme().textMuted}>{authMark(status().claude_auth)} Claude {clean(status().claude_auth)} · quota={clean(status().claude_quota)}</text>
      <Servers />
      <text fg={theme().text}>Runtime</text>
      <text fg={theme().textMuted}>compression={clean(status().compression)} · memory={clean(status().memory)} · context={clean(status().context)}</text>
      <text fg={theme().textMuted}>skills={clean(status().skills)} · IVOAI {clean(status().version)}</text>
    </box>
  )

  api.slots.register({
    order: 20,
    slots: {
      home_logo: () => <Logo />,
      home_bottom: () => <Summary />,
      sidebar_content: () => <Servers />,
    },
  })
  api.route.register([{ name: "ivoai", render: () => <Panel /> }])
  api.command?.register(() => [{
    title: "IVOAI status",
    value: "ivoai.status",
    description: "Executors, quotas, knowledge scope, and runtime health",
    category: "IVOAI",
    slash: { name: "ivoai" },
    onSelect: () => api.route.navigate("ivoai"),
  }])
}

export default { id: "ivoai.managed.tui", tui }
