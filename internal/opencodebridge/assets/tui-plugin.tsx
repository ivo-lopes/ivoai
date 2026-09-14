/** @jsxImportSource @opentui/solid */
import { For, Show, createSignal, onCleanup } from "solid-js"
import { clean, logo, panel, servers, fitRows } from "./presentation.mjs"
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
  const [catalog, setCatalog] = createSignal<any[]>([])
  const sessionControl = async () => {
    try {
      const response = await fetch(options.bridge+"/console/sessions",{headers:{Authorization:"Bearer "+options.token},signal:AbortSignal.timeout(2000)})
      if (!response.ok) throw new Error("unavailable")
      const sessions = await response.json()
      api.ui.dialog.replace(() => <api.ui.DialogSelect title="IVOAI Session Control — current project" options={(Array.isArray(sessions)?sessions:[]).slice(0,128).map((s:any)=>({
        title:`${clean(s.id).slice(0,17)} · ${clean(s.primary)} · ${clean(s.state)}`,value:s,
        description:s.resumable ? "Resume same logical session / native OpenCode conversation" : "Use IVOAI Session Control for explicit mode switch or provider handoff", disabled:!s.resumable,
      }))} onSelect={(option:any)=> {
        const s=option.value
        api.ui.dialog.replace(()=><api.ui.DialogConfirm title="Resume conversation" message="Switch to this existing IVOAI session? No previous turn is replayed; new input remains gated." onCancel={()=>api.ui.dialog.clear()} onConfirm={()=>{
          void fetch(options.bridge+"/console/resume",{method:"POST",headers:{Authorization:"Bearer "+options.token,"Content-Type":"application/json"},body:JSON.stringify({id:s.id,confirm:true}),signal:AbortSignal.timeout(5000)})
          .then(async r=>{if(!r.ok)throw new Error("unavailable");const body=await r.json();api.ui.dialog.clear();api.route.navigate("session",{sessionID:body.native_id})})
          .catch(()=>api.ui.toast({title:"IVOAI",message:"Cannot resume while a turn is active or ownership is unavailable",variant:"error"}))
        }}/>)
      }}/>)
    } catch { api.ui.toast({title:"IVOAI",message:"Session catalog unavailable",variant:"error"}) }
  }
  const loadCatalog = async () => {
    try {
      const response = await fetch(options.bridge + "/console/catalog", {headers:{Authorization:"Bearer " + options.token}, signal:AbortSignal.timeout(2000)})
      if (!response.ok) throw new Error("unavailable")
      const body = await response.json()
      setCatalog(Array.isArray(body.models) ? body.models.slice(0,256) : [])
      api.ui.dialog.setSize("large")
      api.ui.dialog.replace(() => <scrollbox maxHeight={Math.max(8,api.renderer.height-8)}><Rows rows={catalog().flatMap((model:any) => [
        {text:`${clean(model.name)} · provider=${clean(model.executor,"scheduler")} · source=${clean(model.model_source)}`,role:"text"},
        {text:`  reasoning=${(Array.isArray(model.supported_efforts) ? model.supported_efforts : []).map((x:any)=>clean(x)).join(", ") || "not exposed"} · admission revalidates availability/quota`,role:"muted"},
      ])}/></scrollbox>)
    } catch { api.ui.toast({title:"IVOAI",message:"Model catalog unavailable",variant:"error"}) }
  }
  const confirmAction = (action:string, message:string) => {
    api.ui.dialog.replace(() => <api.ui.DialogConfirm title="IVOAI control plane" message={message} onCancel={() => api.ui.dialog.clear()} onConfirm={() => {
      api.ui.dialog.clear()
      void fetch(options.bridge + "/console/action", {method:"POST",headers:{Authorization:"Bearer " + options.token,"Content-Type":"application/json"},body:JSON.stringify({action,confirm:true}),signal:AbortSignal.timeout(15000)})
        .then(response => { if (!response.ok) throw new Error("failed"); api.ui.toast({title:"IVOAI",message:action.startsWith("profile.") ? "Profile saved for the next session" : "Owned hook wiring validated",variant:"success"}) })
        .catch(() => api.ui.toast({title:"IVOAI",message:"Action failed; inspect IVOAI status",variant:"error"}))
    }}/>)
  }
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

  const refresh = async () => {
    if (stopped) return
    try {
      const response = await fetch(options.bridge + "/status", {
        headers: { Authorization: "Bearer " + options.token },
        signal: AbortSignal.timeout(2000),
      })
      if (response.ok) setStatus(await response.json())
      await refreshPermissions()
    } catch {
      setStatus((value: any) => ({ ...value, session_state: "stale" }))
    } finally {
      if (!stopped) timer = setTimeout(refresh, 1000)
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
      <For each={fitRows(props.rows, Math.max(12, api.renderer.width - 8))}>
        {(row: any) => <text wrapMode="word" fg={row.role === "identity" ? theme().primary : row.role === "warning" ? theme().warning : row.role === "muted" ? theme().textMuted : theme().text}>{row.text}</text>}
      </For>
    </box>
  )
  const Servers = () => <Rows rows={servers(status())} />
  const Panel = () => (
    <scrollbox maxHeight={Math.max(8, api.renderer.height - 8)}>
    <box flexDirection="column" gap={0} padding={1}>
      <Rows rows={panel(status())} />
    </box>
    </scrollbox>
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
    // Native dialog ownership restores the composer focus on Escape. A route
    // without a back binding stranded keyboard-only users outside the session.
    onSelect: () => {
      api.ui.dialog.setSize("large")
      api.ui.dialog.replace(() => <Panel />)
    },
  }, {
    title:"IVOAI Session Control",value:"ivoai.sessions",category:"IVOAI",slash:{name:"ivoai-sessions"},onSelect:sessionControl,
  }, {
    title:"IVOAI runtime model catalog",value:"ivoai.catalog",category:"IVOAI",slash:{name:"ivoai-models"},onSelect:loadCatalog,
  }, ...["economic","balanced","quality","custom"].map(name => ({
    title:`IVOAI automation profile: ${name}`,value:`ivoai.profile.${name}`,category:"IVOAI",slash:{name:`ivoai-profile-${name}`},
    onSelect:()=>confirmAction(`profile.${name}`, name === "custom" ? "Keep current policies as custom? Edit individual policies from the IVOAI Orchestration Policies menu." : `Apply ${name} to the next session? Preserves explicit provider/model overrides and security gates; resets plan approval to required. Current workers are unchanged.`),
  })), {
    title:"IVOAI validate memory hooks",value:"ivoai.hooks.validate",category:"IVOAI",onSelect:()=>confirmAction("hooks.validate","Validate IVOAI-owned hook wiring without running lifecycle hooks?"),
  }, {
    title:"IVOAI repair owned memory hooks",value:"ivoai.hooks.repair",category:"IVOAI",onSelect:()=>confirmAction("hooks.repair","Repair only proven IVOAI-owned wiring? Personal hooks are preserved. No lifecycle hook will be executed."),
  }])
}

export default { id: "ivoai.managed.tui", tui }
