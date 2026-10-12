/** @jsxImportSource @opentui/solid */
import { For, Show, createSignal, onCleanup } from "solid-js"
import { useTerminalDimensions } from "@opentui/solid"
import { clean, logo, panel, servers, fitRows } from "./presentation.mjs"
import { readOperationalStream, mergeActivity, createStreamWatchdog, initialOperationalStatus } from "./events.mjs"
import { installKeyboardProtocol } from "./keyboard.mjs"

const tui = async (api: any, options: any) => {
  const restoreKeyboard = installKeyboardProtocol(api.renderer, process.stdout, process.env.TERM)
  onCleanup(restoreKeyboard)
  api.lifecycle.onDispose(restoreKeyboard)
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
  let permissionDialog = false
  const refreshPermissions = async () => {
    if (permissionDialog || api.ui.dialog.open || stopped) return
    const response = await fetch(options.bridge + "/native-permissions", {
      headers: { Authorization: "Bearer " + options.token }, signal: AbortSignal.timeout(2000),
    })
    if (!response.ok) return
    const pending = await response.json()
    if (!Array.isArray(pending) || !pending[0] || stopped || api.ui.dialog.open) return
    const permission = pending[0]
    permissionDialog = true
    let answered = false
    const reply = (allow: boolean) => {
      if (answered) return
      answered = true
      void fetch(options.bridge + "/native-permissions/reply", {
        method: "POST", headers: { Authorization: "Bearer " + options.token, "Content-Type": "application/json" },
        body: JSON.stringify({id: permission.id, allow}), signal: AbortSignal.timeout(2000),
      }).catch(() => {})
      permissionDialog = false
      api.ui.dialog.clear()
    }
    const title = String(permission.id).startsWith("plan_") ? "IVOAI plan approval" : String(permission.id).startsWith("routing_") ? "IVOAI quota routing approval" : String(permission.id).startsWith("ext_") ? "IVOAI external MCP permission" : "IVOAI native executor permission"
    api.ui.dialog.replace(() => <api.ui.DialogConfirm title={title} message={clean(permission.description, "Permission details unavailable", 2048)} onConfirm={() => reply(true)} onCancel={() => reply(false)} />, () => reply(false))
  }

  let streamAbort: AbortController | undefined
  let cursor = 0
  let eventSession = ""
  const refresh = async () => {
    if (stopped) return
    streamAbort = new AbortController()
    const controller = streamAbort
    const watchdog = createStreamWatchdog(() => {
      if (!stopped) setStatus((value: any) => ({ ...value, session_state: "stale" }))
      controller.abort()
    })
    try {
      const response = await fetch(options.bridge + "/events?session_id=" + encodeURIComponent(eventSession), {
        headers: { Authorization: "Bearer " + options.token, "Last-Event-ID": String(cursor) },
        signal: streamAbort.signal,
      })
      if (!response.ok || !response.body) throw new Error("operational stream unavailable")
      await readOperationalStream(response.body, async (frame: any) => {
        if (stopped) return
        watchdog.touch()
        cursor = frame.replay.latest_sequence
        eventSession = frame.replay.session_id
        setStatus((value: any) => mergeActivity(value, frame))
        await refreshPermissions()
      })
      if (!stopped) throw new Error("operational stream closed")
    } catch {
      if (!stopped) setStatus((value: any) => ({ ...value, session_state: "stale" }))
    } finally {
      watchdog.dispose()
      // Only reconnect after transport closure; healthy sessions have one stream.
      if (!stopped) timer = setTimeout(refresh, 3000)
    }
  }
  const initialize = async () => {
    streamAbort = new AbortController()
    const controller = streamAbort
    const timeout = setTimeout(() => controller.abort(), 5000)
    try {
      const snapshot = await initialOperationalStatus(options.bridge, options.token, controller.signal)
      if (!stopped) setStatus(snapshot)
    } catch {
      if (!stopped) setStatus((value: any) => ({ ...value, session_state: "stale" }))
    } finally {
      clearTimeout(timeout)
      if (!stopped) void refresh()
    }
  }
  void initialize()
  const dispose = () => {
    stopped = true
    streamAbort?.abort()
    if (timer) clearTimeout(timer)
  }
  onCleanup(dispose)
  api.lifecycle.onDispose(dispose)
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
      <text fg={theme().primary}><b>{logo[0]}</b></text>
      <text fg={theme().textMuted}>{logo[1]}</text>
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
        {clean(status().selection_mode, "auto")} · {clean(status().primary)} · {clean(status().effective_model, "UNKNOWN")} · {clean(status().effective_effort, "UNKNOWN")}
      </text>
      <text fg={theme().textMuted}>/ivoai</text>
    </box>
  )
  const Rows = (props: any) => (
    <box flexDirection="column" gap={0}>
      <For each={fitRows(props.rows, props.columns ?? Math.max(12, api.renderer.width - 8))}>
        {(row: any) => <text wrapMode="word" fg={row.role === "identity" ? theme().primary : row.role === "warning" ? theme().warning : row.role === "muted" ? theme().textMuted : theme().text}>{row.text}</text>}
      </For>
    </box>
  )
  const Servers = () => <Rows rows={servers(status())} />
  const Panel = () => {
    const dimensions = useTerminalDimensions()
    return (
      <scrollbox focused scrollX={false} maxHeight={Math.max(4, Math.floor(dimensions().height / 2))} padding={1}>
        <Rows rows={panel(status())} columns={Math.max(12, Math.min(80, dimensions().width - 12))} />
      </scrollbox>
    )
  }

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
    // Native dialog ownership restores the composer focus on Escape. A route
    // without a back binding stranded keyboard-only users outside the session.
    onSelect: () => {
      api.ui.dialog.setSize("large")
      api.ui.dialog.replace(() => <Panel />)
    },
  }])
}

export default { id: "ivoai.managed.tui", tui }
